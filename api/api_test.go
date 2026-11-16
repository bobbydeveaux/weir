package api_test

import (
	"bytes"
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
	"github.com/bobbydeveaux/weir/internal/testdb"
	"github.com/bobbydeveaux/weir/runner"
	"github.com/bobbydeveaux/weir/worker"
)

const workflow = `
weir: "0.1"
namespace: shop
name: refund
steps:
  - id: classify
    kind: decide
    template: classify@1
    labels: [refund, other]
    confidence: 0.8
    default: other
    next:
      - when: label == "other"
        go: done
  - id: approve
    kind: human
    assign: { role: team_lead, not: run.requester }
  - id: pay
    kind: external
    connection: payments
    call: { path: /refunds, body: { order: input.order } }
    resume: callback
    timeout: 1h
  - id: done
    kind: deterministic
    handler: core.noop
`

// client calls the API as a named actor.
type client struct {
	t    *testing.T
	base string
}

func (c client) do(method, path, who, body string) (int, map[string]any) {
	c.t.Helper()
	req, err := http.NewRequest(method, c.base+path, strings.NewReader(body))
	if err != nil {
		c.t.Fatal(err)
	}
	if who != "" {
		req.Header.Set("X-Weir-Actor", who)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(raw), &out); err != nil {
		var list []any
		if json.Unmarshal(raw, &list) == nil {
			out = map[string]any{"list": list}
		}
	}
	return resp.StatusCode, out
}

func TestAWorkflowDrivenEntirelyOverHTTP(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool := testdb.New(ctx, t)
	eng, err := engine.New(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	reg := worker.NewRegistry()
	eng.HandlerExists = reg.Exists

	apiServer := httptest.NewServer(api.New(eng))
	defer apiServer.Close()
	c := client{t: t, base: apiServer.URL}

	// The third party remembers where it was told to call back.
	var mu sync.Mutex
	var callbackURL string
	payments := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		callbackURL = r.Header.Get("Weir-Callback-URL")
		mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
	}))
	defer payments.Close()

	fake := &runner.Fake{Decisions: map[string]runner.DecideResult{"classify": {Label: "refund", Confidence: 0.97}}}
	w := &worker.Worker{Engine: eng, Runner: fake, Handlers: reg, CallbackURL: apiServer.URL}
	settle := func() {
		t.Helper()
		for idle := 0; idle < 8; {
			found, err := w.RunOnce(ctx)
			if err != nil {
				t.Fatalf("worker: %v", err)
			}
			if found {
				idle = 0
			} else {
				idle++
				time.Sleep(5 * time.Millisecond)
			}
		}
	}

	if code, _ := c.do("POST", "/v1/templates/classify", "ops", `{"user": "{{input}}"}`); code != http.StatusCreated {
		t.Fatalf("publish template = %d", code)
	}
	if code, _ := c.do("PUT", "/v1/connections/payments", "ops", `{"base_url": "`+payments.URL+`"}`); code != http.StatusOK {
		t.Fatalf("put connection = %d", code)
	}
	if code, out := c.do("POST", "/v1/definitions", "ops", workflow); code != http.StatusCreated || out["version"] != 1.0 {
		t.Fatalf("publish = %d %v", code, out)
	}
	if code, _ := c.do("POST", "/v1/definitions", "", workflow); code != http.StatusBadRequest {
		t.Fatalf("publish with no actor = %d, want 400", code)
	}

	code, out := c.do("POST", "/v1/runs", "alice", `{"namespace": "shop", "name": "refund", "input": {"order": "A-17"}}`)
	if code != http.StatusCreated {
		t.Fatalf("start = %d %v", code, out)
	}
	runID := out["id"].(string)
	settle()

	_, tasks := c.do("GET", "/v1/tasks", "", "")
	list, _ := tasks["list"].([]any)
	if len(list) != 1 {
		t.Fatalf("tasks = %v, want one", tasks)
	}
	taskID := list[0].(map[string]any)["id"].(string)

	if code, _ := c.do("POST", "/v1/tasks/"+taskID+"/decision", "alice", `{"outcome": "approved"}`); code != http.StatusForbidden {
		t.Fatalf("the requester approving their own run = %d, want 403", code)
	}
	if code, _ := c.do("POST", "/v1/tasks/"+taskID+"/decision", "bob", `{"outcome": "approved"}`); code != http.StatusOK {
		t.Fatalf("approve = %d", code)
	}
	settle()

	mu.Lock()
	url := callbackURL
	mu.Unlock()
	if !strings.HasPrefix(url, apiServer.URL+"/v1/callbacks/") {
		t.Fatalf("the third party was given callback URL %q", url)
	}
	path := strings.TrimPrefix(url, apiServer.URL)
	if code, _ := c.do("POST", path, "", `{"refunded": true}`); code != http.StatusOK {
		t.Fatalf("callback = %d", code)
	}
	if code, _ := c.do("POST", path, "", `{"refunded": true}`); code != http.StatusConflict {
		t.Fatalf("a repeated callback = %d, want 409", code)
	}
	if code, _ := c.do("POST", "/v1/callbacks/not-a-real-token", "", `{}`); code != http.StatusConflict {
		t.Fatalf("a made-up token = %d, want 409", code)
	}
	settle()

	code, run := c.do("GET", "/v1/runs/"+runID, "", "")
	if code != http.StatusOK || run["status"] != "succeeded" {
		t.Fatalf("run = %d %v", code, run)
	}
	if code, _ := c.do("GET", "/v1/runs/00000000-0000-0000-0000-000000000000", "", ""); code != http.StatusNotFound {
		t.Fatalf("an unknown run = %d, want 404", code)
	}
}
