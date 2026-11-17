package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bobbydeveaux/weir/api"
	"github.com/bobbydeveaux/weir/blob"
	"github.com/bobbydeveaux/weir/engine"
	"github.com/bobbydeveaux/weir/internal/testdb"
	"github.com/bobbydeveaux/weir/runner"
	"github.com/bobbydeveaux/weir/worker"
)

const workspaceWorkflow = `
weir: "0.1"
namespace: code
name: fix
steps:
  - id: fix
    kind: ai
    prompt: fix@1
    input: { repo: input.repo }
    workspace:
      handler: test.checkout
      with: { depth: 1 }
    output:
      schema: { type: object }
    budget: { usd: 1 }
  - id: report
    kind: ai
    prompt: fix@1
    input: { changed: steps.fix.files }
    output:
      schema: { type: object }
    budget: { usd: 1 }
`

// editor stands in for an agent in a prepared directory: it has the
// directory prepared, as a real runner would, then reports changes.
type editor struct {
	// changes, when set, is what the agent reports instead of the usual two files.
	changes  []runner.File
	system   string
	prepared string
	limit    *runner.WriteLimit
	input    string
}

func (e *editor) Name() string { return "fake" }
func (e *editor) Decide(context.Context, runner.DecideRequest) (runner.DecideResult, error) {
	return runner.DecideResult{}, nil
}
func (e *editor) Run(ctx context.Context, req runner.RunRequest) (runner.RunResult, error) {
	res := runner.RunResult{Output: json.RawMessage(`{"ok": true}`)}
	if req.Workspace == nil {
		e.input = req.Prompt
		return res, nil
	}
	dir, err := os.MkdirTemp("", "weir-test-")
	if err != nil {
		return res, err
	}
	defer os.RemoveAll(dir)
	if _, err := req.Workspace.Prepare(ctx, dir); err != nil {
		return res, err
	}
	data, _ := os.ReadFile(filepath.Join(dir, "prepared.txt"))
	e.prepared, e.limit, e.system = string(data), req.Writes, req.System
	res.Files = []runner.File{{Name: "main.go", Data: []byte("package main\n")}, {Name: "old.go", Deleted: true}}
	if e.changes != nil {
		res.Files = e.changes
	}
	return res, nil
}

func TestAStepsDirectoryIsPreparedAndItsChangesAreKept(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool := testdb.New(ctx, t)
	eng, err := engine.New(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	eng.Files = blob.NewPostgres(pool)
	reg := worker.NewRegistry()
	eng.WorkspaceExists = reg.WorkspaceExists
	srv := httptest.NewServer(api.New(eng, reg))
	t.Cleanup(srv.Close)
	if _, err := eng.PublishTemplate(ctx, "fix", engine.Template{System: "s", User: "{{input}}"}, "test"); err != nil {
		t.Fatal(err)
	}

	// Nothing is registered yet, so the definition is refused.
	if _, err := eng.Publish(ctx, []byte(workspaceWorkflow), "test"); err == nil || !strings.Contains(err.Error(), "is not registered") {
		t.Fatalf("publish with an unknown handler = %v", err)
	}
	reg.RegisterWorkspace("test.checkout", func(_ context.Context, w worker.Workspace) (runner.Prepared, error) {
		var in struct{ Repo string }
		_ = json.Unmarshal(w.Input, &in)
		note := in.Repo + " depth " + strings.TrimSpace(string(mustJSON(w.With["depth"]))) + " step " + w.Step
		return runner.Prepared{}, os.WriteFile(filepath.Join(w.Dir, "prepared.txt"), []byte(note), 0o600)
	})
	withFiles := strings.Replace(workspaceWorkflow, "    workspace:\n", "    files:\n      - step: report\n    workspace:\n", 1)
	if _, err := eng.Publish(ctx, []byte(withFiles), "test"); err == nil || !strings.Contains(err.Error(), "cannot also list files") {
		t.Fatalf("publish with files and a workspace = %v", err)
	}
	if _, err := eng.Publish(ctx, []byte(workspaceWorkflow), "test"); err != nil {
		t.Fatalf("publish: %v", err)
	}

	runID, _, err := eng.StartRun(ctx, engine.StartRequest{Namespace: "code", Name: "fix", Requester: "alice",
		Input: json.RawMessage(`{"repo": "acme/shop"}`)})
	if err != nil {
		t.Fatal(err)
	}
	agent := &editor{}
	w := &worker.Worker{Engine: eng, Handlers: reg, Runner: agent}
	for i := 0; i < 2; i++ {
		if found, err := w.RunOnce(ctx); err != nil || !found {
			t.Fatalf("RunOnce %d = %v, %v", i, found, err)
		}
	}
	if v, err := eng.Run(ctx, runID); err != nil || v.Status != "succeeded" {
		t.Fatalf("run = %s, %v", mustJSON(v), err)
	}
	// The handler was given the step's input and its fixed settings.
	if agent.prepared != "acme/shop depth 1 step fix" {
		t.Errorf("the directory was prepared with %q", agent.prepared)
	}
	// A step with a workspace always has limits, even when it names none.
	if agent.limit == nil || agent.limit.MaxFiles != 20 || agent.limit.MaxBytes != 8<<20 {
		t.Errorf("limits = %+v, want the defaults", agent.limit)
	}
	// A later step sees what changed, including what was deleted.
	if !strings.Contains(agent.input, `"path": "old.go"`) || !strings.Contains(agent.input, `"deleted": true`) {
		t.Errorf("steps.fix.files resolved to %s", agent.input)
	}

	var files []struct {
		Path    string
		Deleted bool
		Size    int64
	}
	if code := call(t, srv.URL, "GET", "/v1/runs/"+runID+"/files", "", "", &files); code != http.StatusOK || len(files) != 2 {
		t.Fatalf("list = %d, %+v", code, files)
	}
	for _, f := range files {
		if f.Deleted != (f.Path == "old.go") || (f.Deleted && f.Size != 0) {
			t.Errorf("file = %+v", f)
		}
	}
	if code := call(t, srv.URL, "GET", "/v1/runs/"+runID+"/files/fix/main.go", "", "", nil); code != http.StatusOK {
		t.Errorf("downloading a changed file = %d", code)
	}
	if code := call(t, srv.URL, "GET", "/v1/runs/"+runID+"/files/fix/old.go", "", "", nil); code != http.StatusNotFound {
		t.Errorf("downloading a deleted file = %d, want 404", code)
	}
}

func TestAChangeToAForbiddenPathFailsTheStepAndKeepsNothing(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool := testdb.New(ctx, t)
	eng, err := engine.New(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	eng.Files = blob.NewPostgres(pool)
	reg := worker.NewRegistry()
	reg.RegisterWorkspace("test.checkout", func(context.Context, worker.Workspace) (runner.Prepared, error) {
		return runner.Prepared{}, nil
	})
	if _, err := eng.PublishTemplate(ctx, "fix", engine.Template{System: "s", User: "u"}, "test"); err != nil {
		t.Fatal(err)
	}
	testsOnly := strings.Replace(workspaceWorkflow, "    output:\n", "    writes: { only: [\"*_test.go\"], never: [\"billing/\"] }\n    output:\n", 1)
	if _, err := eng.Publish(ctx, []byte(strings.Replace(testsOnly, `"*_test.go"`, `"!main.go"`, 1)), "test"); err == nil || !strings.Contains(err.Error(), "is not a path pattern") {
		t.Fatalf("publish with a negated pattern = %v", err)
	}
	if _, err := eng.Publish(ctx, []byte(testsOnly), "test"); err != nil {
		t.Fatalf("publish: %v", err)
	}

	attempt := func(changes ...runner.File) (status, detail string, files int) {
		t.Helper()
		runID, _, err := eng.StartRun(ctx, engine.StartRequest{Namespace: "code", Name: "fix", Requester: "alice", Input: json.RawMessage(`{"repo": "a/b"}`)})
		if err != nil {
			t.Fatal(err)
		}
		agent := &editor{changes: changes}
		w := &worker.Worker{Engine: eng, Handlers: reg, Runner: agent}
		// Run until nothing is left, so that a later step of this run is not
		// still waiting when the next run starts.
		for {
			found, err := w.RunOnce(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if !found {
				break
			}
		}
		if !strings.Contains(agent.system, "*_test.go") || !strings.Contains(agent.system, "billing/") {
			t.Errorf("the agent was not told the rules: %q", agent.system)
		}
		v, err := eng.Run(ctx, runID)
		if err != nil {
			t.Fatal(err)
		}
		var kept []json.RawMessage
		raw, _ := eng.RunFiles(ctx, runID)
		_ = json.Unmarshal(raw, &kept)
		return v.Steps[0].Status, string(mustJSON(v)), len(kept)
	}
	test := runner.File{Name: "api/server_test.go", Data: []byte("package api\n")}

	if status, detail, files := attempt(test); status != "succeeded" || files != 1 {
		t.Errorf("a test file alone: step %s, %d file(s): %s", status, files, detail)
	}
	for name, change := range map[string]runner.File{
		"is not one of the paths":                      {Name: "api/server.go", Data: []byte("package api\n")},
		"may never change":                             {Name: "billing/charge_test.go", Data: []byte("package billing\n")},
		"is protected":                                 {Name: ".github/workflows/ci_test.go", Data: []byte("x")},
		"is not one of the paths this step may change": {Name: "old.go", Deleted: true},
	} {
		// The allowed file comes first: one bad change still means nothing is kept.
		status, detail, files := attempt(test, change)
		if status == "succeeded" || files != 0 || !strings.Contains(detail, name) {
			t.Errorf("changing %s: step %s, %d file(s) kept, want a failure saying %q: %s", change.Name, status, files, name, detail)
		}
	}
}
