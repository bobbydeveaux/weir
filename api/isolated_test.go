package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bobbydeveaux/weir/agent"
	"github.com/bobbydeveaux/weir/api"
	"github.com/bobbydeveaux/weir/blob"
	"github.com/bobbydeveaux/weir/engine"
	"github.com/bobbydeveaux/weir/internal/testdb"
	"github.com/bobbydeveaux/weir/runner"
	"github.com/bobbydeveaux/weir/worker"
)

// container stands in for an agent's container: given only Weir's address
// and a token, it runs the same program a real one would, with a stand-in
// for the model.
type container struct {
	agent  runner.Runner
	starts []worker.AgentStart
	// instead, when set, replaces what the container does.
	instead func(ctx context.Context, start worker.AgentStart) error
}

func (c *container) Describe() string { return "a test container" }
func (c *container) Launch(ctx context.Context, start worker.AgentStart) error {
	c.starts = append(c.starts, start)
	if c.instead != nil {
		return c.instead(ctx, start)
	}
	return agent.Run(ctx, &agent.Client{BaseURL: start.URL, Token: start.Token}, c.agent)
}

// boxed stands in for the model inside the container. It works in the
// directory it is given, as a real agent would.
type boxed struct {
	seen   runner.RunRequest
	found  string
	change func(dir string)
	fail   error
}

func (c *boxed) Name() string { return "fake" }
func (c *boxed) Decide(context.Context, runner.DecideRequest) (runner.DecideResult, error) {
	return runner.DecideResult{}, nil
}
func (c *boxed) Run(ctx context.Context, req runner.RunRequest) (runner.RunResult, error) {
	c.seen = req
	res := runner.RunResult{Output: json.RawMessage(`{"ok": true}`), Usage: runner.Usage{Provider: "claude", Model: "haiku", CostUSD: 0.02, TokensIn: 10}}
	if c.fail != nil {
		return res, c.fail
	}
	if req.Workspace == nil {
		return res, nil
	}
	dir, err := os.MkdirTemp("", "weir-test-agent-")
	if err != nil {
		return res, err
	}
	defer os.RemoveAll(dir)
	if _, err := req.Workspace.Prepare(ctx, dir); err != nil {
		return res, err
	}
	data, _ := os.ReadFile(filepath.Join(dir, "main.go"))
	c.found = string(data)
	res.Files = []runner.File{{Name: "main.go", Data: []byte("package main // fixed\n")}, {Name: "old.go", Deleted: true}}
	return res, nil
}

func TestAnAgentInItsOwnContainerTalksOnlyToWeir(t *testing.T) {
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
	srv := httptest.NewServer(api.New(eng, reg, api.WithAdminKey("admin-key-for-this-test")))
	t.Cleanup(srv.Close)
	// The directory is prepared by the worker, with a credential the agent
	// must never see.
	reg.RegisterWorkspace("test.checkout", func(_ context.Context, w worker.Workspace) (runner.Prepared, error) {
		_ = os.WriteFile(filepath.Join(w.Dir, "main.go"), []byte("package main\n"), 0o600)
		_ = os.WriteFile(filepath.Join(w.Dir, "old.go"), []byte("package main\n"), 0o600)
		return runner.Prepared{Ignore: []string{"node_modules/"}}, nil
	})
	if _, err := eng.PublishTemplate(ctx, "fix", engine.Template{System: "Be careful.", User: "Fix {{repo}}"}, "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := eng.Publish(ctx, []byte(workspaceWorkflow), "test"); err != nil {
		t.Fatalf("publish: %v", err)
	}
	start := func() string {
		id, _, err := eng.StartRun(ctx, engine.StartRequest{Namespace: "code", Name: "fix", Requester: "alice", Input: json.RawMessage(`{"repo": "acme/shop"}`)})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	drain := func(w *worker.Worker) {
		for {
			found, err := w.RunOnce(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if !found {
				return
			}
		}
	}

	model := &boxed{}
	box := &container{agent: model}
	w := &worker.Worker{Engine: eng, Handlers: reg, Launcher: box, AgentURL: srv.URL,
		Runner: failing{}, Model: func(tier, m string) string { return "resolved-model" }}
	runID := start()
	drain(w)
	v, err := eng.Run(ctx, runID)
	if err != nil || v.Status != "succeeded" {
		t.Fatalf("run = %s, %v", mustJSON(v), err)
	}
	// The container was given an address and a token, and the agent inside
	// was given its job: prompt, model, limits and the prepared directory.
	if len(box.starts) != 2 || box.starts[0].URL != srv.URL || len(box.starts[0].Token) < 32 {
		t.Errorf("starts = %+v", box.starts)
	}
	if model.found != "package main\n" {
		t.Errorf("the prepared directory did not reach the agent: main.go = %q", model.found)
	}
	// The worker's own runner was never used, with or without a workspace.
	var files []struct {
		Path    string
		Deleted bool
	}
	if code := call(t, srv.URL, "GET", "/v1/runs/"+runID+"/files", "", "", nil); code != http.StatusUnauthorized {
		t.Errorf("the API has keys on, but a request with none = %d", code)
	}
	raw, _ := eng.RunFiles(ctx, runID)
	_ = json.Unmarshal(raw, &files)
	if len(files) != 2 {
		t.Errorf("files = %s", raw)
	}
	record, _ := eng.RunRecord(ctx, runID)
	if !strings.Contains(string(record), `"cost_usd": 0.02`) || !strings.Contains(string(record), "Be careful.") {
		t.Errorf("the model call was not recorded with its prompt and cost: %s", record)
	}

	// The token is good for that one attempt. Once the step is done it opens nothing.
	old := box.starts[0].Token
	for _, part := range []string{"job", "workspace"} {
		resp, err := http.Get(srv.URL + "/v1/agent/" + old + "/" + part)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusConflict {
			t.Errorf("a finished step's token fetching its %s = %d, want 409", part, resp.StatusCode)
		}
	}

	// A container that ends without sending anything fails the step.
	box.instead = func(context.Context, worker.AgentStart) error { return errors.New("exit status 137") }
	runID = start()
	drain(w)
	if v, _ := eng.Run(ctx, runID); v.Status == "succeeded" || !strings.Contains(string(mustJSON(v)), "without sending a result") {
		t.Errorf("a container that sent nothing: %s", mustJSON(v))
	}
	// One that sends two results has the second refused.
	box.instead = func(ctx context.Context, s worker.AgentStart) error {
		c := &agent.Client{BaseURL: s.URL, Token: s.Token}
		if err := agent.Run(ctx, c, model); err != nil {
			return err
		}
		if err := agent.Run(ctx, c, model); err == nil || !strings.Contains(err.Error(), "already sent") {
			t.Errorf("a second result = %v, want it refused", err)
		}
		return nil
	}
	runID = start()
	drain(w)
	if v, _ := eng.Run(ctx, runID); v.Status != "succeeded" {
		t.Errorf("run = %s", mustJSON(v))
	}
	// What comes back is the agent's word. More files than the step allows
	// are refused here, whatever the container did.
	box.instead = func(ctx context.Context, s worker.AgentStart) error {
		many := &agent.Result{Output: json.RawMessage(`{"ok": true}`)}
		for i := 0; i < 30; i++ {
			many.Files = append(many.Files, agent.File{Path: "f" + string(rune('a'+i)) + ".txt", Data: []byte("x")})
		}
		body, _ := json.Marshal(many)
		resp, err := http.Post(s.URL+"/v1/agent/"+s.Token+"/result", "application/json", strings.NewReader(string(body)))
		if err == nil {
			resp.Body.Close()
		}
		return err
	}
	runID = start()
	drain(w)
	if v, _ := eng.Run(ctx, runID); v.Status == "succeeded" || !strings.Contains(string(mustJSON(v)), "more than 20 files") {
		t.Errorf("thirty files against a limit of twenty: %s", mustJSON(v))
	}
	// And a path that leaves the directory is refused.
	box.instead = func(ctx context.Context, s worker.AgentStart) error {
		body := `{"output": {"ok": true}, "files": [{"path": "../../etc/passwd", "data": "eA=="}]}`
		resp, err := http.Post(s.URL+"/v1/agent/"+s.Token+"/result", "application/json", strings.NewReader(body))
		if err == nil {
			resp.Body.Close()
		}
		return err
	}
	runID = start()
	drain(w)
	if v, _ := eng.Run(ctx, runID); v.Status == "succeeded" || !strings.Contains(string(mustJSON(v)), "stays inside its directory") {
		t.Errorf("a path outside the directory: %s", mustJSON(v))
	}
	// An agent that fails says why, and its cost is still recorded.
	box.instead, model.fail = nil, errors.New("the model refused")
	runID = start()
	drain(w)
	if v, _ := eng.Run(ctx, runID); v.Status == "succeeded" || !strings.Contains(string(mustJSON(v)), "the model refused") {
		t.Errorf("an agent that failed: %s", mustJSON(v))
	}
}

// failing is a runner that must never be used.
type failing struct{}

func (failing) Name() string { return "cli" }
func (failing) Decide(context.Context, runner.DecideRequest) (runner.DecideResult, error) {
	return runner.DecideResult{}, errors.New("the worker's own runner was used")
}
func (failing) Run(context.Context, runner.RunRequest) (runner.RunResult, error) {
	return runner.RunResult{}, errors.New("the worker's own runner was used")
}

// TestAnAgentRunsInADockerContainerLive starts a real container for a step.
// It runs only when asked:
//
//	WEIR_TEST_AGENT_IMAGE      the image, built from deploy/agent.Dockerfile
//	CLAUDE_CODE_OAUTH_TOKEN    optional; with it the agent really runs
//
// Without a credential for the model the agent cannot sign in, and the test
// checks that the container still fetched its job and sent that back as its
// result, which proves everything but the model call.
func TestAnAgentRunsInADockerContainerLive(t *testing.T) {
	image := os.Getenv("WEIR_TEST_AGENT_IMAGE")
	if image == "" {
		t.Skip("set WEIR_TEST_AGENT_IMAGE to start a real container")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	pool := testdb.New(ctx, t)
	eng, err := engine.New(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	eng.Files = blob.NewPostgres(pool)
	reg := worker.NewRegistry()
	eng.WorkspaceExists = reg.WorkspaceExists
	srv := httptest.NewUnstartedServer(api.New(eng, reg))
	srv.Start()
	t.Cleanup(srv.Close)
	reg.RegisterWorkspace("test.checkout", func(_ context.Context, w worker.Workspace) (runner.Prepared, error) {
		_ = os.WriteFile(filepath.Join(w.Dir, "main.go"), []byte("package main\n"), 0o644)
		_ = os.WriteFile(filepath.Join(w.Dir, "old.go"), []byte("package main\n"), 0o644)
		return runner.Prepared{}, nil
	})
	if _, err := eng.PublishTemplate(ctx, "fix", engine.Template{
		System: "You make small, exact changes to the files in your working directory.",
		User: "1. Add the comment '// fixed' as a new last line of main.go.\n2. Delete old.go with the shell.\n" +
			"3. With the shell, write the output of `whoami` and of `env | grep -c WEIR_ ; true` into probe.txt, one per line.\nChange nothing else.",
	}, "test"); err != nil {
		t.Fatal(err)
	}
	one := strings.Split(workspaceWorkflow, "  - id: report")[0] // the first step alone
	if _, err := eng.Publish(ctx, []byte(one), "test"); err != nil {
		t.Fatalf("publish: %v", err)
	}
	runID, _, err := eng.StartRun(ctx, engine.StartRequest{Namespace: "code", Name: "fix", Requester: "alice", Input: json.RawMessage(`{"repo": "a/b"}`)})
	if err != nil {
		t.Fatal(err)
	}
	// The container reaches this machine by a name, not by "localhost".
	address := strings.Replace(srv.URL, "127.0.0.1", "host.docker.internal", 1)
	w := &worker.Worker{Engine: eng, Handlers: reg, Runner: failing{}, AgentURL: address,
		Launcher: &worker.Docker{Image: image, Env: []string{"CLAUDE_CODE_OAUTH_TOKEN"}},
		Model:    func(string, string) string { return "haiku" }}
	if _, err := w.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	v, err := eng.Run(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	detail := string(mustJSON(v))
	t.Logf("run: %s", detail)
	if strings.Contains(detail, "without sending a result") {
		t.Fatalf("the container never reported back")
	}
	if os.Getenv("CLAUDE_CODE_OAUTH_TOKEN") == "" {
		if v.Status == "succeeded" {
			t.Error("the agent ran with no credential for the model")
		}
		t.Log("no CLAUDE_CODE_OAUTH_TOKEN: the container fetched its job and sent back why it could not run")
		return
	}
	if v.Status != "succeeded" {
		t.Fatalf("run = %s", v.Status)
	}
	raw, _ := eng.RunFiles(ctx, runID)
	t.Logf("files: %s", raw)
	body, _, err := eng.OpenFile(ctx, runID, "fix", "probe.txt")
	if err != nil {
		t.Fatalf("probe.txt: %v", err)
	}
	probe := make([]byte, 200)
	n, _ := body.Read(probe)
	body.Close()
	t.Logf("probe.txt: %q", probe[:n])
	if !strings.HasPrefix(string(probe[:n]), "agent\n") {
		t.Errorf("the agent did not run as the container's own user: %q", probe[:n])
	}
}
