package api_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bobbydeveaux/weir/api"
	"github.com/bobbydeveaux/weir/engine"
	"github.com/bobbydeveaux/weir/internal/secrets"
	"github.com/bobbydeveaux/weir/internal/testdb"
	"github.com/bobbydeveaux/weir/runner"
	"github.com/bobbydeveaux/weir/worker"
)

const guarded = `
weir: "0.1"
namespace: docs
name: review
hooks:
  - on: [run.succeeded]
    connection: app
    path: /hooks
steps:
  - id: read
    kind: ai
    prompt: look@1
    files:
      - { connection: app, path: /files/doc.txt, name: doc.txt }
    tools: { read: [http.get, kb.search] }
    budget: { usd: 1 }
    output: { schema: { type: object } }
  - id: store
    kind: external
    connection: app
    call: { path: /step }
    resume: poll
    poll: { until: "true" }
`

// thirdParty records the credential each path was called with.
type thirdParty struct {
	mu   sync.Mutex
	seen map[string][]string
	srv  *httptest.Server
}

func newThirdParty(t *testing.T) *thirdParty {
	tp := &thirdParty{seen: map[string][]string{}}
	tp.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := r.Header.Get("Authorization")
		if key := r.Header.Get("X-Api-Key"); key != "" {
			got = "X-Api-Key " + key
		}
		tp.mu.Lock()
		tp.seen[r.URL.Path] = append(tp.seen[r.URL.Path], got)
		tp.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/mcp" {
			var req struct{ Method string }
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &req)
			result := `{}`
			switch req.Method {
			case "tools/list":
				result = `{"tools":[{"name":"search","inputSchema":{"type":"object"},"annotations":{"readOnlyHint":true}}]}`
			case "tools/call":
				result = `{"content":[{"type":"text","text":"{\"hits\": 1}"}]}`
			}
			_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":`+result+`}`)
			return
		}
		_, _ = io.WriteString(w, `{"ok": true}`)
	}))
	t.Cleanup(tp.srv.Close)
	return tp
}

// calls returns the credentials a path was called with.
func (tp *thirdParty) calls(path string) []string {
	tp.mu.Lock()
	defer tp.mu.Unlock()
	return append([]string{}, tp.seen[path]...)
}

func TestAConnectionSignsEveryCallAndNeverKeepsTheSecret(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool := testdb.New(ctx, t)
	eng, err := engine.New(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	const secret = "acme-s3cret-value"
	reg := worker.NewRegistry()
	// Only acme has the secret. The server's own settings sit beside it.
	reg.Secrets = &secrets.Resolver{Getenv: func(k string) string {
		return map[string]string{"WEIR_SECRET__ACME__APP_TOKEN": secret, "WEIR_ADMIN_KEY": "the-admin-key"}[k]
	}}
	worker.RegisterHTTPTools(reg, eng, nil)
	eng.HandlerExists, eng.ToolClass = reg.Exists, reg.ToolClass
	srv := httptest.NewServer(api.New(eng, reg))
	defer srv.Close()
	tp := newThirdParty(t)

	for _, ws := range []string{"acme", "rival"} {
		call(t, srv.URL, "POST", "/v1/workspaces", "", `{"slug": "`+ws+`"}`, nil)
		call(t, srv.URL, "POST", "/v1/templates/look", ws, `{"user": "look"}`, nil)
	}

	// Malformed sign-in settings are refused when the connection is made.
	for _, bad := range []string{
		`{"base_url": "http://x", "auth": {"kind": "magic", "secret": "env:APP_TOKEN"}}`,
		`{"base_url": "http://x", "auth": {"kind": "bearer", "secret": "file:../rival/token"}}`,
		`{"base_url": "http://x", "auth": {"kind": "bearer", "secret": "APP_TOKEN"}}`,
		`{"base_url": "http://x", "auth": {"kind": "header", "secret": "env:APP_TOKEN"}}`,
		`{"base_url": "http://x", "auth": {"kind": "bearer"}}`,
	} {
		if code := call(t, srv.URL, "PUT", "/v1/connections/bad", "acme", bad, nil); code != http.StatusBadRequest {
			t.Errorf("%s = %d, want 400", bad, code)
		}
	}

	// acme's connections name the secret. The reply says where it is read from, not what it is.
	var made map[string]any
	if code := call(t, srv.URL, "PUT", "/v1/connections/app", "acme",
		`{"base_url": "`+tp.srv.URL+`", "auth": {"kind": "bearer", "secret": "env:APP_TOKEN"}}`, &made); code != http.StatusOK {
		t.Fatalf("connect = %d", code)
	}
	if made["secret_found"] != true || made["secret_read_from"] != "WEIR_SECRET__ACME__APP_TOKEN" {
		t.Errorf("reply = %v", made)
	}
	if code := call(t, srv.URL, "PUT", "/v1/connections/kb", "acme",
		`{"kind": "mcp", "base_url": "`+tp.srv.URL+`/mcp", "auth": {"kind": "header", "header": "X-Api-Key", "secret": "env:APP_TOKEN"}}`, nil); code != http.StatusOK {
		t.Fatalf("connect the MCP server = %d", code)
	}
	if got := tp.calls("/mcp"); len(got) == 0 || got[0] != "X-Api-Key "+secret {
		t.Errorf("the MCP server was asked for its tools with %q", got)
	}
	if code := call(t, srv.URL, "POST", "/v1/definitions", "acme", guarded, nil); code != http.StatusCreated {
		t.Fatalf("publish = %d", code)
	}

	// An agent's tool calls are signed, and the agent never sees the credential.
	var started struct{ ID string }
	call(t, srv.URL, "POST", "/v1/runs", "acme", `{"namespace": "docs", "name": "review"}`, &started)
	work, err := eng.Claim(ctx, []string{"ai"}, "test", time.Minute)
	if err != nil || work == nil {
		t.Fatalf("claim: %v %v", work, err)
	}
	out, err := worker.CallTool(ctx, eng, reg, work.Token, "http.get", json.RawMessage(`{"connection": "app", "path": "/tool"}`))
	if err != nil || strings.Contains(string(out), secret) {
		t.Fatalf("http.get = %s, %v", out, err)
	}
	if _, err := worker.CallTool(ctx, eng, reg, work.Token, "kb.search", json.RawMessage(`{}`)); err != nil {
		t.Fatalf("kb.search: %v", err)
	}
	if err := eng.Complete(ctx, work.Token, engine.Result{Output: json.RawMessage(`{}`), Actor: "test"}); err != nil {
		t.Fatal(err)
	}

	// A second run goes through the worker: it fetches the file, makes the call and sends the hook.
	fake := &runner.Fake{Outputs: map[string][]json.RawMessage{"read": {json.RawMessage(`{}`)}}}
	w := &worker.Worker{Engine: eng, Runner: fake, Handlers: reg}
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
		if _, err := w.DeliverHooks(ctx); err != nil {
			t.Fatalf("hooks: %v", err)
		}
	}
	var second struct{ ID string }
	call(t, srv.URL, "POST", "/v1/runs", "acme", `{"namespace": "docs", "name": "review"}`, &second)
	settle()

	for path, want := range map[string]string{
		"/tool": "Bearer " + secret, "/files/doc.txt": "Bearer " + secret, "/step": "Bearer " + secret,
		"/hooks": "Bearer " + secret, "/mcp": "X-Api-Key " + secret,
	} {
		got := tp.calls(path)
		if len(got) == 0 {
			t.Errorf("%s was never called", path)
			continue
		}
		for _, cred := range got {
			if cred != want {
				t.Errorf("%s was called with %q, want %q", path, cred, want)
			}
		}
	}
	var run struct{ Status string }
	call(t, srv.URL, "GET", "/v1/runs/"+second.ID, "acme", "", &run)
	if run.Status != "succeeded" {
		t.Errorf("the second run is %s", run.Status)
	}

	// The value is nowhere Weir keeps or shows things.
	for _, path := range []string{"/v1/connections", "/v1/registry", "/v1/runs/" + second.ID, "/v1/runs/" + second.ID + "/record",
		"/v1/runs/" + started.ID + "/record", "/v1/events"} {
		req, _ := http.NewRequest("GET", srv.URL+path, nil)
		req.Header.Set(api.WorkspaceHeader, "acme")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if strings.Contains(string(raw), secret) {
			t.Errorf("GET %s contains the secret", path)
		}
	}
	var conns []map[string]any
	call(t, srv.URL, "GET", "/v1/connections", "acme", "", &conns)
	if auth, _ := conns[0]["auth"].(map[string]any); conns[0]["name"] != "app" || auth["kind"] != "bearer" || auth["secret"] != "env:APP_TOKEN" {
		t.Errorf("the connection is shown as %v, want its kind and the reference", conns[0])
	}
	for _, table := range []string{"connections", "events", "tool_calls", "steps", "runs", "outbox", "attempts", "ai_calls"} {
		var n int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM `+table+` t WHERE to_jsonb(t)::text LIKE '%' || $1 || '%'`, secret).Scan(&n); err != nil || n != 0 {
			t.Errorf("table %s holds the secret in %d row(s) (%v)", table, n, err)
		}
	}

	// rival names the same secret. It is not rival's, so nothing is sent, and the step says what to set.
	var theirs map[string]any
	call(t, srv.URL, "PUT", "/v1/connections/app", "rival",
		`{"base_url": "`+tp.srv.URL+`/rival", "auth": {"kind": "bearer", "secret": "env:APP_TOKEN"}}`, &theirs)
	if theirs["secret_found"] != false {
		t.Errorf("rival's reply = %v, want the secret reported as not found", theirs)
	}
	const callOnly = `
weir: "0.1"
namespace: docs
name: store
steps:
  - id: store
    kind: external
    connection: app
    call: { path: /step }
    resume: poll
    poll: { until: "true" }
`
	call(t, srv.URL, "POST", "/v1/definitions", "rival", callOnly, nil)
	var blocked struct{ ID string }
	call(t, srv.URL, "POST", "/v1/runs", "rival", `{"namespace": "docs", "name": "store"}`, &blocked)
	settle()
	var theirRun struct {
		Status string
		Steps  []struct {
			Error struct{ Type, Message string }
		}
	}
	call(t, srv.URL, "GET", "/v1/runs/"+blocked.ID, "rival", "", &theirRun)
	if theirRun.Status != "blocked" || theirRun.Steps[0].Error.Type != "secret_unavailable" ||
		!strings.Contains(theirRun.Steps[0].Error.Message, "WEIR_SECRET__RIVAL__APP_TOKEN") {
		t.Errorf("rival's run = %+v, want it blocked with the variable to set", theirRun)
	}
	if got := tp.calls("/rival/step"); len(got) != 0 {
		t.Errorf("a call was made without its credential: %q", got)
	}
}
