package api_test

import (
	"context"
	"encoding/json"
	"github.com/bobbydeveaux/weir/internal/secrets"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bobbydeveaux/weir/api"
	"github.com/bobbydeveaux/weir/engine"
	"github.com/bobbydeveaux/weir/internal/testdb"
	"github.com/bobbydeveaux/weir/runner"
	"github.com/bobbydeveaux/weir/worker"
)

const hooked = `
weir: "0.1"
namespace: shop
name: signoff
hooks:
  - on: [run.*, step.waiting]
    connection: app
    path: /hooks
steps:
  - id: approve
    kind: human
    assign: { role: lead }
  - id: done
    kind: deterministic
    handler: core.noop
`

func TestHooksTellTheApplicationWhatHappened(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool := testdb.New(ctx, t)
	eng, err := engine.New(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	reg := worker.NewRegistry()
	eng.HandlerExists = reg.Exists
	srv := httptest.NewServer(api.New(eng, reg))
	defer srv.Close()

	// The application records each hook, and refuses the first one once.
	var mu sync.Mutex
	var got []map[string]any
	var ids []string
	refusals := 1
	app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if refusals > 0 {
			refusals--
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		var body map[string]any
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		got, ids = append(got, body), append(ids, r.Header.Get("Weir-Hook-Id"))
	}))
	defer app.Close()

	if code := call(t, srv.URL, "POST", "/v1/definitions", "", hooked, nil); code == http.StatusCreated {
		t.Fatal("a workflow with a hook to an unknown connection was published")
	}
	call(t, srv.URL, "PUT", "/v1/connections/app", "", `{"base_url": "`+app.URL+`"}`, nil)
	if code := call(t, srv.URL, "POST", "/v1/definitions", "", hooked, nil); code != http.StatusCreated {
		t.Fatalf("publish = %d", code)
	}
	if code := call(t, srv.URL, "POST", "/v1/definitions", "", strings.Replace(hooked, "run.*", "Everything!", 1), nil); code == http.StatusCreated {
		t.Error("a hook with a malformed name was published")
	}

	w := &worker.Worker{Engine: eng, Runner: &runner.Fake{}, Handlers: reg}
	settle := func() {
		t.Helper()
		for idle := 0; idle < 6; {
			if found, err := w.RunOnce(ctx); err != nil {
				t.Fatalf("worker: %v", err)
			} else if found {
				idle = 0
			} else {
				idle++
			}
		}
	}
	types := func() string {
		mu.Lock()
		defer mu.Unlock()
		var out []string
		for _, h := range got {
			out = append(out, h["type"].(string))
		}
		return strings.Join(out, ",")
	}

	var started struct{ ID string }
	call(t, srv.URL, "POST", "/v1/runs", "", `{"namespace": "shop", "name": "signoff", "input": {"order": "A-1"}}`, &started)
	settle()

	// The first delivery is refused, so nothing has arrived and it is waiting to be retried.
	if n, err := w.DeliverHooks(ctx); err != nil || n != 1 || types() != "step.waiting" {
		t.Fatalf("first delivery: sent %d (%v), the application has %q; want the refused hook held back and the next one through", n, err, types())
	}
	// Bring the retry forward, as time would.
	if _, err := pool.Exec(ctx, `UPDATE outbox SET next_at = now() WHERE kind = 'hook' AND delivered_at IS NULL`); err != nil {
		t.Fatal(err)
	}
	if n, _ := w.DeliverHooks(ctx); n != 1 || types() != "step.waiting,run.started" {
		t.Fatalf("after the retry the application has %q", types())
	}

	var tasks []map[string]any
	call(t, srv.URL, "GET", "/v1/tasks", "", "", &tasks)
	call(t, srv.URL, "POST", "/v1/tasks/"+tasks[0]["id"].(string)+"/decision", "", `{"outcome": "approved"}`, nil)
	settle()
	if _, err := w.DeliverHooks(ctx); err != nil {
		t.Fatal(err)
	}
	if types() != "step.waiting,run.started,run.succeeded" {
		t.Fatalf("the application heard %q", types())
	}

	mu.Lock()
	first, last := got[0], got[len(got)-1]
	distinct := map[string]bool{}
	for _, id := range ids {
		distinct[id] = true
	}
	mu.Unlock()
	if first["step"] != "approve" || first["run_id"] != started.ID || first["run_input"].(map[string]any)["order"] != "A-1" {
		t.Errorf("the waiting hook = %v", first)
	}
	if wf := last["workflow"].(map[string]any); wf["name"] != "signoff" || wf["version"] != 1.0 {
		t.Errorf("the finished hook names workflow %v", wf)
	}
	if len(distinct) != 3 {
		t.Errorf("hook ids = %v, want three different ones", ids)
	}

	var record struct {
		Hooks []struct {
			Type        string
			Attempts    int
			DeliveredAt *string `json:"delivered_at"`
		}
	}
	call(t, srv.URL, "GET", "/v1/runs/"+started.ID+"/record", "", "", &record)
	if len(record.Hooks) != 3 || record.Hooks[0].Type != "run.started" || record.Hooks[0].Attempts != 2 || record.Hooks[2].DeliveredAt == nil {
		t.Errorf("recorded hooks = %+v", record.Hooks)
	}
}

func TestABlockedRunCanBeCancelledAndIsThenCountedAsFailed(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool := testdb.New(ctx, t)
	eng, err := engine.New(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	reg := worker.NewRegistry()
	eng.HandlerExists = reg.Exists
	srv := httptest.NewServer(api.New(eng, reg))
	defer srv.Close()

	// The application refuses the one call the workflow makes, so the run blocks.
	app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = io.WriteString(w, `{"error": "that approver is not one of ours"}`)
	}))
	defer app.Close()
	call(t, srv.URL, "PUT", "/v1/connections/app", "", `{"base_url": "`+app.URL+`"}`, nil)
	const wf = `
weir: "0.1"
namespace: shop
name: create
steps:
  - id: make
    kind: external
    connection: app
    call: { path: /make }
    resume: poll
    poll: { until: "true" }
`
	if code := call(t, srv.URL, "POST", "/v1/definitions", "", wf, nil); code != http.StatusCreated {
		t.Fatalf("publish = %d", code)
	}
	var started struct{ ID string }
	call(t, srv.URL, "POST", "/v1/runs", "", `{"namespace": "shop", "name": "create"}`, &started)
	w := &worker.Worker{Engine: eng, Runner: &runner.Fake{}, Handlers: reg}
	for i := 0; i < 5; i++ {
		if _, err := w.RunOnce(ctx); err != nil {
			t.Fatal(err)
		}
	}

	var run struct {
		Status string
		Steps  []struct {
			Error struct{ Message string }
		}
	}
	call(t, srv.URL, "GET", "/v1/runs/"+started.ID, "", "", &run)
	if run.Status != "blocked" || !strings.Contains(run.Steps[0].Error.Message, "not one of ours") {
		t.Fatalf("run = %+v, want it blocked with the application's own reason", run)
	}
	totals := func() map[string]any {
		var usage map[string]any
		call(t, srv.URL, "GET", "/v1/usage", "", "", &usage)
		return usage["totals"].(map[string]any)
	}
	if got := totals(); got["blocked"] != 1.0 || got["active"] != 0.0 || got["failed"] != 0.0 {
		t.Errorf("while blocked the totals are %v, want one blocked and none active", got)
	}

	if code := call(t, srv.URL, "POST", "/v1/runs/"+started.ID+"/cancel", "", `{"reason": "cannot be put right"}`, nil); code != http.StatusOK {
		t.Fatalf("cancel = %d", code)
	}
	if code := call(t, srv.URL, "POST", "/v1/runs/"+started.ID+"/cancel", "", `{}`, nil); code != http.StatusForbidden {
		t.Errorf("cancelling twice = %d, want 403", code)
	}
	if code := call(t, srv.URL, "POST", "/v1/runs/"+started.ID+"/resume", "", `{}`, nil); code == http.StatusOK {
		t.Error("a cancelled run was resumed")
	}
	call(t, srv.URL, "GET", "/v1/runs/"+started.ID, "", "", &run)
	if got := totals(); run.Status != "cancelled" || got["blocked"] != 0.0 || got["failed"] != 1.0 {
		t.Errorf("after cancelling: status %q, totals %v", run.Status, got)
	}
}

func TestAHookCanBeSignedSoTheReceiverKnowsItIsFromWeir(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool := testdb.New(ctx, t)
	eng, err := engine.New(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	const shared = "hook-shared-secret"
	reg := worker.NewRegistry()
	reg.Secrets = &secrets.Resolver{Getenv: func(k string) string { return map[string]string{"WEIR_SECRET__DEFAULT__HOOKS": shared}[k] }}
	eng.HandlerExists = reg.Exists
	srv := httptest.NewServer(api.New(eng, reg))
	defer srv.Close()

	// The application checks each hook as a receiver would.
	type seen struct {
		path, signature, timestamp string
		valid                      bool
	}
	var mu sync.Mutex
	var got []seen
	app := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		s := seen{path: r.URL.Path, signature: r.Header.Get("Weir-Signature"), timestamp: r.Header.Get("Weir-Timestamp")}
		s.valid = s.signature == "sha256="+worker.SignHook(shared, s.timestamp, body)
		mu.Lock()
		got = append(got, s)
		mu.Unlock()
	}))
	defer app.Close()
	call(t, srv.URL, "PUT", "/v1/connections/app", "", `{"base_url": "`+app.URL+`"}`, nil)

	workflow := func(name, secret string) string {
		return `
weir: "0.1"
namespace: shop
name: ` + name + `
hooks:
  - on: [run.succeeded]
    connection: app
    path: /` + name + secret + `
steps:
  - id: done
    kind: deterministic
    handler: core.noop
`
	}
	for name, secret := range map[string]string{"signed": "\n    secret: env:HOOKS", "plain": "", "unset": "\n    secret: env:NOT_SET"} {
		if code := call(t, srv.URL, "POST", "/v1/definitions", "", workflow(name, secret), nil); code != http.StatusCreated {
			t.Fatalf("publish %s = %d", name, code)
		}
		call(t, srv.URL, "POST", "/v1/runs", "", `{"namespace": "shop", "name": "`+name+`"}`, nil)
	}
	if code := call(t, srv.URL, "POST", "/v1/definitions", "", workflow("bad", "\n    secret: /etc/passwd"), nil); code == http.StatusCreated {
		t.Error("a hook whose secret is a path was published")
	}

	w := &worker.Worker{Engine: eng, Runner: &runner.Fake{}, Handlers: reg}
	for i := 0; i < 8; i++ {
		if _, err := w.RunOnce(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := w.DeliverHooks(ctx); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()
	byPath := map[string]seen{}
	for _, s := range got {
		byPath[s.path] = s
	}
	signed := byPath["/signed"]
	age, _ := strconv.ParseInt(signed.timestamp, 10, 64)
	if !signed.valid || time.Since(time.Unix(age, 0)) > time.Minute {
		t.Errorf("the signed hook arrived as %+v, want a signature that checks and a current timestamp", signed)
	}
	if plain, ok := byPath["/plain"]; !ok || plain.signature != "" || plain.timestamp != "" {
		t.Errorf("a hook with no secret arrived as %+v, want it sent as before", plain)
	}
	if _, sent := byPath["/unset"]; sent {
		t.Error("a hook whose secret is not set was sent, unsigned")
	}
	var note string
	if err := pool.QueryRow(ctx, `SELECT COALESCE(last_error, '') FROM outbox WHERE kind = 'hook' AND delivered_at IS NULL`).Scan(&note); err != nil ||
		!strings.Contains(note, "WEIR_SECRET__DEFAULT__NOT_SET") {
		t.Errorf("the unsent hook's note is %q (%v), want the variable to set", note, err)
	}
	var stored int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox t WHERE to_jsonb(t)::text LIKE '%' || $1 || '%'`, shared).Scan(&stored); err != nil || stored != 0 {
		t.Errorf("the shared secret is stored with %d queued hook(s) (%v)", stored, err)
	}
}
