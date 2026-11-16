package api_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bobbydeveaux/weir/api"
	"github.com/bobbydeveaux/weir/engine"
	"github.com/bobbydeveaux/weir/internal/testdb"
	"github.com/bobbydeveaux/weir/runner"
	"github.com/bobbydeveaux/weir/worker"
)

const triage = `
weir: "0.1"
namespace: support
name: triage
steps:
  - id: classify
    kind: decide
    template: classify@1
    labels: [refund, other]
    default: other
  - id: approve
    kind: human
    assign: { role: team_lead }
  - id: done
    kind: deterministic
    handler: core.noop
`

// call makes a request for one workspace and decodes the reply into out.
func call(t *testing.T, base, method, path, workspace, body string, out any) int {
	t.Helper()
	req, err := http.NewRequest(method, base+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Weir-Actor", "ops")
	if workspace != "" {
		req.Header.Set(api.WorkspaceHeader, workspace)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if out != nil && resp.StatusCode < 300 {
		if err := json.Unmarshal(raw, out); err != nil {
			t.Fatalf("%s %s: reply %s: %v", method, path, raw, err)
		}
	}
	return resp.StatusCode
}

func TestWorkspacesAreKeptApartAndCanBeRead(t *testing.T) {
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

	// One worker serves every workspace.
	fake := &runner.Fake{Decisions: map[string]runner.DecideResult{"classify": {Label: "refund", Confidence: 0.9}}}
	w := &worker.Worker{Engine: eng, Runner: fake, Handlers: reg}
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

	if code := call(t, srv.URL, "POST", "/v1/workspaces", "", `{"slug": "acme", "name": "Acme"}`, nil); code != http.StatusCreated {
		t.Fatalf("create workspace = %d", code)
	}
	if code := call(t, srv.URL, "GET", "/v1/definitions", "nobody", "", nil); code != http.StatusNotFound {
		t.Fatalf("an unknown workspace = %d, want 404", code)
	}

	// The template and the workflow live in acme only.
	if code := call(t, srv.URL, "POST", "/v1/templates/classify", "acme", `{"user": "{{input}}"}`, nil); code != http.StatusCreated {
		t.Fatalf("template = %d", code)
	}
	if code := call(t, srv.URL, "POST", "/v1/definitions", "", triage, nil); code == http.StatusCreated {
		t.Fatal("the default workspace published a workflow using acme's template")
	}
	if code := call(t, srv.URL, "POST", "/v1/definitions", "acme", triage, nil); code != http.StatusCreated {
		t.Fatalf("publish = %d", code)
	}
	var started struct{ ID string }
	if code := call(t, srv.URL, "POST", "/v1/runs", "acme", `{"namespace": "support", "name": "triage", "input": {"text": "hi"}}`, &started); code != http.StatusCreated {
		t.Fatalf("start = %d", code)
	}
	if code := call(t, srv.URL, "POST", "/v1/runs", "", `{"namespace": "support", "name": "triage"}`, nil); code == http.StatusCreated {
		t.Fatal("the default workspace started acme's workflow")
	}
	settle()

	// The default workspace sees none of it.
	var defs, runs, tasks []map[string]any
	call(t, srv.URL, "GET", "/v1/definitions", "", "", &defs)
	call(t, srv.URL, "GET", "/v1/runs", "", "", &runs)
	call(t, srv.URL, "GET", "/v1/tasks", "", "", &tasks)
	if len(defs)+len(runs)+len(tasks) != 0 {
		t.Fatalf("the default workspace sees acme's data: %v %v %v", defs, runs, tasks)
	}
	for _, path := range []string{"/v1/runs/" + started.ID, "/v1/runs/" + started.ID + "/record", "/v1/definitions/support/triage"} {
		if code := call(t, srv.URL, "GET", path, "", "", nil); code != http.StatusNotFound {
			t.Errorf("GET %s from the default workspace = %d, want 404", path, code)
		}
	}

	// acme sees all of it.
	call(t, srv.URL, "GET", "/v1/definitions", "acme", "", &defs)
	if len(defs) != 1 || defs[0]["name"] != "triage" || defs[0]["live_version"] != 1.0 || defs[0]["active"] != 1.0 {
		t.Fatalf("definitions = %v", defs)
	}
	if steps, _ := defs[0]["steps"].([]any); len(steps) != 3 {
		t.Errorf("steps = %v, want three", defs[0]["steps"])
	}
	var def map[string]any
	if code := call(t, srv.URL, "GET", "/v1/definitions/support/triage", "acme", "", &def); code != http.StatusOK {
		t.Fatalf("definition = %d", code)
	}
	if src, _ := def["source"].(string); !strings.Contains(src, "kind: decide") {
		t.Errorf("the definition's source was not returned: %v", def["source"])
	}
	call(t, srv.URL, "GET", "/v1/tasks", "acme", "", &tasks)
	if len(tasks) != 1 || tasks[0]["name"] != "triage" {
		t.Fatalf("tasks = %v", tasks)
	}
	taskID := tasks[0]["id"].(string)
	if code := call(t, srv.URL, "POST", "/v1/tasks/"+taskID+"/decision", "", `{"outcome": "approved"}`, nil); code != http.StatusNotFound {
		t.Fatalf("deciding acme's task from the default workspace = %d, want 404", code)
	}
	if code := call(t, srv.URL, "POST", "/v1/tasks/"+taskID+"/decision", "acme", `{"outcome": "approved"}`, nil); code != http.StatusOK {
		t.Fatalf("approve = %d", code)
	}
	settle()

	call(t, srv.URL, "GET", "/v1/runs?namespace=support&status=succeeded", "acme", "", &runs)
	if len(runs) != 1 || runs[0]["id"] != started.ID || runs[0]["steps_done"] != 3.0 {
		t.Fatalf("runs = %v", runs)
	}
	var record map[string]any
	call(t, srv.URL, "GET", "/v1/runs/"+started.ID+"/record", "acme", "", &record)
	calls, _ := record["ai_calls"].([]any)
	if len(calls) != 1 || calls[0].(map[string]any)["template"] != "classify" {
		t.Fatalf("ai_calls = %v", record["ai_calls"])
	}
	var usage map[string]any
	call(t, srv.URL, "GET", "/v1/usage?namespace=support", "acme", "", &usage)
	if totals := usage["totals"].(map[string]any); totals["runs"] != 1.0 || totals["succeeded"] != 1.0 {
		t.Errorf("usage totals = %v", usage["totals"])
	}
	if models, _ := usage["by_model"].([]any); len(models) != 1 || models[0].(map[string]any)["calls"] != 1.0 {
		t.Errorf("usage by model = %v", usage["by_model"])
	}
	call(t, srv.URL, "GET", "/v1/usage", "", "", &usage)
	if totals := usage["totals"].(map[string]any); totals["runs"] != 0.0 {
		t.Errorf("the default workspace's usage counts acme's runs: %v", totals)
	}
	var templates []map[string]any
	call(t, srv.URL, "GET", "/v1/templates", "acme", "", &templates)
	if len(templates) != 1 || templates[0]["key"] != "classify" {
		t.Errorf("templates = %v", templates)
	}
	var registry map[string]any
	call(t, srv.URL, "GET", "/v1/registry", "acme", "", &registry)
	if handlers, _ := registry["handlers"].([]any); len(handlers) < 2 {
		t.Errorf("registry = %v", registry)
	}
}
