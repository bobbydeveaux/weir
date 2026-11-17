package api_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bobbydeveaux/weir/blob"
	"github.com/bobbydeveaux/weir/engine"
	"github.com/bobbydeveaux/weir/internal/testdb"
	"github.com/bobbydeveaux/weir/runner"
	"github.com/bobbydeveaux/weir/worker"
)

const fixAndCommit = `
weir: "0.1"
namespace: code
name: fix-and-propose
steps:
  - id: fix
    kind: ai
    prompt: fix@1
    input: { repo: '"acme/app"' }
    workspace:
      handler: github.checkout
      with: { connection: github, depth: 1 }
    output:
      schema: { type: object }
    budget: { usd: 1 }
  - id: propose
    kind: deterministic
    handler: github.commit
    input:
      repo: '"acme/app"'
      from: '"fix"'
      branch: input.branch
      title: '"Fix the build"'
      body: '"Made by a test."'
`

// gitIn runs git in dir with no configuration from this machine.
func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// fixer stands in for an agent in a checkout: it reads a file to show the
// checkout arrived, then reports what it changed.
type fixer struct{ found string }

func (f *fixer) Name() string { return "fake" }
func (f *fixer) Decide(context.Context, runner.DecideRequest) (runner.DecideResult, error) {
	return runner.DecideResult{}, nil
}
func (f *fixer) Run(ctx context.Context, req runner.RunRequest) (runner.RunResult, error) {
	res := runner.RunResult{Output: json.RawMessage(`{"ok": true}`)}
	dir, err := os.MkdirTemp("", "weir-test-fix-")
	if err != nil {
		return res, err
	}
	defer os.RemoveAll(dir)
	if _, err := req.Workspace.Prepare(ctx, dir); err != nil {
		return res, err
	}
	data, _ := os.ReadFile(filepath.Join(dir, "main.go"))
	f.found = string(data)
	res.Files = []runner.File{
		{Name: "main.go", Data: []byte("package main // fixed\n")},
		{Name: "docs/new.md", Data: []byte("# New\n")},
		{Name: "old.go", Deleted: true},
	}
	return res, nil
}

func TestAnAgentsChangesBecomeABranchAndAPullRequest(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	// A repository to clone from and push to, standing in for GitHub's git side.
	seed := t.TempDir()
	gitIn(t, seed, "init", "--quiet", "--initial-branch", "main")
	_ = os.WriteFile(filepath.Join(seed, "main.go"), []byte("package main\n"), 0o644)
	_ = os.WriteFile(filepath.Join(seed, "old.go"), []byte("package main // old\n"), 0o644)
	gitIn(t, seed, "add", "-A")
	gitIn(t, seed, "commit", "--quiet", "-m", "first")
	origin := filepath.Join(t.TempDir(), "app.git")
	gitIn(t, seed, "clone", "--quiet", "--bare", seed, origin)

	pool := testdb.New(ctx, t)
	eng, err := engine.New(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	eng.Files = blob.NewPostgres(pool)
	gh := newFakeGitHub(t)
	t.Cleanup(gh.srv.Close)
	reg := worker.NewRegistry()
	reg.GitRemote = func(_, repo string) (string, error) { return origin, nil }
	worker.RegisterGitHubCheckout(reg, eng, nil)
	worker.RegisterGitHubCommit(reg, eng, nil)
	eng.HandlerExists, eng.WorkspaceExists = reg.Exists, reg.WorkspaceExists
	if err := eng.PutConnection(ctx, "github", gh.srv.URL, engine.Auth{}); err != nil {
		t.Fatal(err)
	}
	if _, err := eng.PublishTemplate(ctx, "fix", engine.Template{System: "s", User: "u"}, "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := eng.Publish(ctx, []byte(fixAndCommit), "test"); err != nil {
		t.Fatalf("publish: %v", err)
	}

	agent := &fixer{}
	w := &worker.Worker{Engine: eng, Handlers: reg, Runner: agent}
	run := func(branch string) engine.RunView {
		t.Helper()
		id, _, err := eng.StartRun(ctx, engine.StartRequest{Namespace: "code", Name: "fix-and-propose", Requester: "alice",
			Input: json.RawMessage(`{"branch": "` + branch + `"}`)})
		if err != nil {
			t.Fatal(err)
		}
		for {
			found, err := w.RunOnce(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if !found {
				break
			}
		}
		v, err := eng.Run(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return *v
	}

	v := run("weir/fix-1")
	if v.Status != "succeeded" {
		t.Fatalf("run = %s", mustJSON(v))
	}
	// The agent started inside the checkout.
	if agent.found != "package main\n" {
		t.Errorf("the checkout did not reach the agent: %q", agent.found)
	}
	var out struct {
		Changed, Pushed bool
		Commit, Base    string
		PullRequest     struct {
			Number int
			Opened bool
		} `json:"pull_request"`
	}
	_ = json.Unmarshal(v.Output, &out)
	if !out.Changed || !out.Pushed || len(out.Commit) < 7 || out.Base != "main" || !out.PullRequest.Opened || out.PullRequest.Number == 0 {
		t.Errorf("output = %s", v.Output)
	}

	// The branch on the remote holds exactly the agent's changes, and main is untouched.
	check := t.TempDir()
	gitIn(t, check, "clone", "--quiet", "--branch", "weir/fix-1", origin, ".")
	if data, _ := os.ReadFile(filepath.Join(check, "main.go")); string(data) != "package main // fixed\n" {
		t.Errorf("main.go on the branch = %q", data)
	}
	if data, _ := os.ReadFile(filepath.Join(check, "docs", "new.md")); string(data) != "# New\n" {
		t.Errorf("docs/new.md on the branch = %q", data)
	}
	if _, err := os.Stat(filepath.Join(check, "old.go")); err == nil {
		t.Error("old.go is still on the branch")
	}
	if log := gitIn(t, check, "log", "--format=%an|%s", "-1"); strings.TrimSpace(log) != "weir|Fix the build" {
		t.Errorf("the commit is %q", log)
	}
	if main := gitIn(t, check, "show", "origin/main:main.go"); main != "package main\n" {
		t.Errorf("main was changed: %q", main)
	}
	gh.mu.Lock()
	pulls := len(gh.pulls)
	gh.branches["weir/fix-1"] = map[string]string{} // GitHub now knows the branch
	gh.mu.Unlock()
	if pulls != 1 {
		t.Errorf("%d pull requests were opened", pulls)
	}

	// The same branch again, as a retry would: nothing is pushed twice, and
	// the pull request that is open is the answer.
	again := run("weir/fix-1")
	_ = json.Unmarshal(again.Output, &out)
	if again.Status != "succeeded" || out.Pushed || out.PullRequest.Opened || out.PullRequest.Number == 0 {
		t.Errorf("a second go at the same branch: %s", mustJSON(again))
	}
	// The default branch is never written to.
	if v := run("main"); v.Status == "succeeded" || !strings.Contains(string(mustJSON(v)), "must go on a new branch") {
		t.Errorf("committing to main: %s", mustJSON(v))
	}
}
