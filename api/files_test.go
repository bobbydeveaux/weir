package api_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bobbydeveaux/weir/api"
	"github.com/bobbydeveaux/weir/blob"
	"github.com/bobbydeveaux/weir/engine"
	"github.com/bobbydeveaux/weir/internal/testdb"
	"github.com/bobbydeveaux/weir/runner"
	"github.com/bobbydeveaux/weir/worker"
)

const writingWorkflow = `
weir: "0.1"
namespace: docs
name: draft-and-review
steps:
  - id: draft
    kind: ai
    prompt: draft@1
    writes: { max_files: 5 }
    output:
      schema: { type: object }
    budget: { usd: 1 }
  - id: review
    kind: ai
    prompt: review@1
    input: { written: steps.draft.files }
    files:
      - step: draft
    output:
      schema: { type: object }
    budget: { usd: 1 }
`

// writer stands in for an agent: the draft step writes files, and the review
// step notes what it was given.
type writer struct {
	mu    sync.Mutex
	write []runner.File
	limit *runner.WriteLimit
	given []runner.File
	input string
}

func (w *writer) Name() string { return "fake" }
func (w *writer) Decide(context.Context, runner.DecideRequest) (runner.DecideResult, error) {
	return runner.DecideResult{}, nil
}
func (w *writer) Run(_ context.Context, req runner.RunRequest) (runner.RunResult, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	res := runner.RunResult{Output: json.RawMessage(`{"ok": true}`)}
	if req.Step == "draft" {
		w.limit, res.Files = req.Writes, w.write
	} else {
		w.given, w.input = req.Files, req.Prompt
	}
	return res, nil
}

func TestAnAgentsFilesAreKeptForLaterStepsAndTheApplication(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool := testdb.New(ctx, t)
	eng, err := engine.New(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	eng.Files = blob.NewPostgres(pool)
	reg := worker.NewRegistry()
	srv := httptest.NewServer(api.New(eng, reg))
	t.Cleanup(srv.Close)

	for _, key := range []string{"draft", "review"} {
		if _, err := eng.PublishTemplate(ctx, key, engine.Template{System: "s", User: "{{input}}"}, "test"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := eng.Publish(ctx, []byte(writingWorkflow), "test"); err != nil {
		t.Fatalf("publish: %v", err)
	}
	runID, _, err := eng.StartRun(ctx, engine.StartRequest{Namespace: "docs", Name: "draft-and-review", Requester: "alice"})
	if err != nil {
		t.Fatal(err)
	}

	agent := &writer{write: []runner.File{
		{Name: "report.md", Data: []byte("# Report\n")},
		{Name: ".config/lint/rules.yml", Data: []byte("on: push\n")},
		{Name: "notes/a file with spaces.txt", Data: []byte("hello")},
	}}
	w := &worker.Worker{Engine: eng, Handlers: reg, Runner: agent}
	for i := 0; i < 2; i++ {
		if found, err := w.RunOnce(ctx); err != nil || !found {
			t.Fatalf("RunOnce %d = %v, %v", i, found, err)
		}
	}
	if v, err := eng.Run(ctx, runID); err != nil || v.Status != "succeeded" {
		t.Fatalf("run = %+v, %v", v, err)
	}
	if agent.limit == nil || agent.limit.MaxFiles != 5 || agent.limit.MaxBytes != 8<<20 {
		t.Errorf("the agent's limits = %+v, want 5 files and the default 8 MB", agent.limit)
	}

	// The later step was given the files, under the paths they were written at.
	var names []string
	for _, f := range agent.given {
		names = append(names, f.Name+"="+string(f.Data))
	}
	sort.Strings(names)
	if got := strings.Join(names, "|"); got != ".config/lint/rules.yml=on: push\n|notes/a file with spaces.txt=hello|report.md=# Report\n" {
		t.Errorf("the review step was given %q", got)
	}
	// And an expression can see what was written, without the bytes.
	if !strings.Contains(agent.input, `"path": "report.md"`) || !strings.Contains(agent.input, `"size": 9`) || strings.Contains(agent.input, "blob_key") {
		t.Errorf("steps.draft.files resolved to %s", agent.input)
	}

	// The application can list them and download each one.
	var files []struct {
		Step, Path, SHA256 string
		Size               int64
		ContentType        string `json:"content_type"`
	}
	if code := call(t, srv.URL, "GET", "/v1/runs/"+runID+"/files", "", "", &files); code != http.StatusOK || len(files) != 3 {
		t.Fatalf("list = %d, %+v", code, files)
	}
	for _, f := range files {
		if f.Step != "draft" || f.Size == 0 || len(f.SHA256) != 64 {
			t.Errorf("file = %+v", f)
		}
	}
	resp, err := http.Get(srv.URL + "/v1/runs/" + runID + "/files/draft/notes/a%20file%20with%20spaces.txt")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != "hello" {
		t.Fatalf("download = %d %q", resp.StatusCode, body)
	}
	if d := resp.Header.Get("Content-Disposition"); !strings.HasPrefix(d, "attachment") {
		t.Errorf("Content-Disposition = %q, want a download, never a page", d)
	}
	if resp.Header.Get("X-Content-Type-Options") != "nosniff" || resp.Header.Get("Weir-File-Sha256") == "" {
		t.Errorf("headers = %v", resp.Header)
	}

	// Another workspace sees none of it.
	call(t, srv.URL, "POST", "/v1/workspaces", "", `{"slug": "rival"}`, nil)
	if code := call(t, srv.URL, "GET", "/v1/runs/"+runID+"/files", "rival", "", nil); code != http.StatusNotFound {
		t.Errorf("another workspace listing the files = %d, want 404", code)
	}
	if code := call(t, srv.URL, "GET", "/v1/runs/"+runID+"/files/draft/report.md", "rival", "", nil); code != http.StatusNotFound {
		t.Errorf("another workspace downloading a file = %d, want 404", code)
	}
	if code := call(t, srv.URL, "GET", "/v1/runs/"+runID+"/files/draft/nothing.txt", "", "", nil); code != http.StatusNotFound {
		t.Errorf("a file that was never written = %d, want 404", code)
	}
}

func TestAStepThatCannotKeepItsFilesFails(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool := testdb.New(ctx, t)
	eng, err := engine.New(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	// No file store at all.
	for _, key := range []string{"draft", "review"} {
		if _, err := eng.PublishTemplate(ctx, key, engine.Template{System: "s", User: "u"}, "test"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := eng.Publish(ctx, []byte(writingWorkflow), "test"); err != nil {
		t.Fatal(err)
	}
	runID, _, err := eng.StartRun(ctx, engine.StartRequest{Namespace: "docs", Name: "draft-and-review", Requester: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	w := &worker.Worker{Engine: eng, Handlers: worker.NewRegistry(), Runner: &writer{write: []runner.File{{Name: "a.txt", Data: []byte("a")}}}}
	if _, err := w.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	v, err := eng.Run(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	if v.Status == "succeeded" || !strings.Contains(string(mustJSON(v)), "no file store") {
		t.Errorf("run = %s, want it to say there is no file store", mustJSON(v))
	}
}

func mustJSON(v any) []byte { raw, _ := json.Marshal(v); return raw }

func TestAStepMayOnlyReadTheFilesOfAStepThatWrites(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	eng, err := engine.New(ctx, testdb.New(ctx, t))
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"draft", "review"} {
		if _, err := eng.PublishTemplate(ctx, key, engine.Template{System: "s", User: "u"}, "test"); err != nil {
			t.Fatal(err)
		}
	}
	for change, want := range map[string]string{
		"    writes: { max_files: 5 }\n":   "not a step that writes files",
		"      - step: draft":              "names the step itself",
		"    writes: { max_files: 5 }":     "max_files must be between",
		"      - step: draft\n    output:": "must not also set",
	} {
		with := map[string]string{
			"not a step that writes files": "",
			"names the step itself":        "      - step: review",
			"max_files must be between":    "    writes: { max_files: 5000 }",
			"must not also set":            "      - step: draft\n        name: x.txt\n    output:",
		}[want]
		_, err := eng.Publish(ctx, []byte(strings.Replace(writingWorkflow, change, with, 1)), "test")
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("publish = %v, want an error saying %q", err, want)
		}
	}
}
