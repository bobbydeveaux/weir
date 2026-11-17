package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/bobbydeveaux/weir/api"
	"github.com/bobbydeveaux/weir/blob"
	"github.com/bobbydeveaux/weir/engine"
	"github.com/bobbydeveaux/weir/internal/secrets"
	"github.com/bobbydeveaux/weir/internal/testdb"
	"github.com/bobbydeveaux/weir/worker"
)

const proposeLive = `
weir: "0.1"
namespace: code
name: propose-live
steps:
  - id: change
    kind: ai
    prompt: change@1
    input: { repo: input.repo }
    workspace:
      handler: github.checkout
    writes:
      only: ["/WEIR_TEST.md"]
      max_files: 1
    output:
      schema:
        type: object
        required: [summary]
        properties:
          summary: { type: string }
    budget: { usd: 1, minutes: 5 }
  - id: propose
    kind: deterministic
    handler: github.commit
    input:
      repo: input.repo
      from: '"change"'
      branch: input.branch
      title: '"Test: a pull request opened by Weir (safe to close)"'
      body: '"Opened by an automated test of Weir. An agent in a container of its own added one file, and a separate step committed it from a fresh clone.\n\nAgent''s summary: " + steps.change.output.summary'
`

// TestAnAgentOpensARealPullRequestLive runs the whole chain against the real
// GitHub: a checkout as a GitHub App, a real agent in a container of its own,
// and a commit from a fresh clone that opens a pull request. It changes a
// real repository, so it runs only when every one of these is set:
//
//	WEIR_TEST_GITHUB_APP, WEIR_TEST_GITHUB_APP_KEY   the App and its key's path
//	WEIR_TEST_GITHUB_WRITE_REPO                      a repository to open a pull request on
//	WEIR_TEST_AGENT_IMAGE, CLAUDE_CODE_OAUTH_TOKEN   the agent's image and the model's credential
func TestAnAgentOpensARealPullRequestLive(t *testing.T) {
	app, keyPath, repo := os.Getenv("WEIR_TEST_GITHUB_APP"), os.Getenv("WEIR_TEST_GITHUB_APP_KEY"), os.Getenv("WEIR_TEST_GITHUB_WRITE_REPO")
	image := os.Getenv("WEIR_TEST_AGENT_IMAGE")
	if app == "" || keyPath == "" || repo == "" || image == "" || os.Getenv("CLAUDE_CODE_OAUTH_TOKEN") == "" {
		t.Skip("set WEIR_TEST_GITHUB_APP, WEIR_TEST_GITHUB_APP_KEY, WEIR_TEST_GITHUB_WRITE_REPO, WEIR_TEST_AGENT_IMAGE and CLAUDE_CODE_OAUTH_TOKEN to open a real pull request")
	}
	key, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	pool := testdb.New(ctx, t)
	eng, err := engine.New(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	eng.Files = blob.NewPostgres(pool)
	reg := worker.NewRegistry()
	reg.Secrets = &secrets.Resolver{Getenv: func(name string) string {
		if name == "WEIR_SECRET__DEFAULT__GITHUB_APP_KEY" {
			return string(key)
		}
		return ""
	}}
	worker.RegisterGitHubCheckout(reg, eng, nil)
	worker.RegisterGitHubCommit(reg, eng, nil)
	eng.HandlerExists, eng.WorkspaceExists = reg.Exists, reg.WorkspaceExists
	srv := httptest.NewServer(api.New(eng, reg))
	t.Cleanup(srv.Close)
	if err := eng.PutConnection(ctx, "github", "https://api.github.com",
		engine.Auth{Kind: secrets.GitHubApp, App: app, Secret: "env:GITHUB_APP_KEY"}); err != nil {
		t.Fatal(err)
	}
	if _, err := eng.PublishTemplate(ctx, "change", engine.Template{
		System: "You make one small, exact change to the repository in your working directory.",
		User: "Create a file called WEIR_TEST.md at the top of the repository. It must hold exactly two lines: " +
			"'# Weir test' and 'Added by an automated test of Weir. Safe to delete.'. Change nothing else. " +
			"In your summary, say in one sentence what kind of project this repository is.",
	}, "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := eng.Publish(ctx, []byte(proposeLive), "test"); err != nil {
		t.Fatalf("publish: %v", err)
	}
	branch := fmt.Sprintf("weir/test-%d", time.Now().Unix())
	if named := os.Getenv("WEIR_TEST_GITHUB_BRANCH"); named != "" {
		branch = named // a branch an earlier go already pushed
	}
	input, _ := json.Marshal(map[string]string{"repo": repo, "branch": branch})
	runID, _, err := eng.StartRun(ctx, engine.StartRequest{Namespace: "code", Name: "propose-live", Requester: "test", Input: input})
	if err != nil {
		t.Fatal(err)
	}
	w := &worker.Worker{Engine: eng, Handlers: reg, Runner: failing{},
		AgentURL: strings.Replace(srv.URL, "127.0.0.1", "host.docker.internal", 1),
		Launcher: &worker.Docker{Image: image, Env: []string{"CLAUDE_CODE_OAUTH_TOKEN"}},
		Model:    func(string, string) string { return "haiku" }}
	for {
		found, err := w.RunOnce(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !found {
			break
		}
	}
	v, err := eng.Run(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range v.Steps {
		t.Logf("step %s: %s %s %s", s.Key, s.Status, s.Output, s.Error)
	}
	files, _ := eng.RunFiles(ctx, runID)
	t.Logf("files: %s", files)
	t.Logf("spent $%.4f; output %s", v.SpentUSD, v.Output)
	if v.Status != "succeeded" {
		t.Fatalf("run = %s", v.Status)
	}
	var out struct {
		Pushed      bool
		PullRequest struct {
			Number int
			URL    string
			Opened bool
		} `json:"pull_request"`
	}
	_ = json.Unmarshal(v.Output, &out)
	t.Logf("pull request: %s", out.PullRequest.URL)
	if !out.PullRequest.Opened || out.PullRequest.URL == "" {
		t.Errorf("no pull request was opened: %s", v.Output)
	}
}
