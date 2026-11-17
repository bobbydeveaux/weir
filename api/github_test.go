package api_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bobbydeveaux/weir/api"
	"github.com/bobbydeveaux/weir/engine"
	"github.com/bobbydeveaux/weir/internal/secrets"
	"github.com/bobbydeveaux/weir/internal/testdb"
	"github.com/bobbydeveaux/weir/worker"
)

// fakeGitHub is enough of GitHub's API for the tools: one repository, with
// branches, files, pull requests and comments held in memory.
type fakeGitHub struct {
	mu       sync.Mutex
	branches map[string]map[string]string // branch -> path -> content
	pulls    []map[string]any
	comments []string
	writes   []string // every request that changed something
	tokens   map[string]bool
	srv      *httptest.Server
}

func newFakeGitHub(t *testing.T) *fakeGitHub {
	g := &fakeGitHub{tokens: map[string]bool{},
		branches: map[string]map[string]string{"main": {"README.md": "# App\n", "src/main.go": "package main\n"}}}
	sha := func(s string) string { return fmt.Sprintf("%x", len(s)*7919+int(s[0])) }
	reply := func(w http.ResponseWriter, status int, body any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}
	g.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		defer g.mu.Unlock()
		g.tokens[r.Header.Get("Authorization")] = true
		path, ok := strings.CutPrefix(r.URL.Path, "/repos/acme/app")
		if !ok {
			reply(w, 404, map[string]string{"message": "Not Found"})
			return
		}
		if r.Method != http.MethodGet {
			g.writes = append(g.writes, r.Method+" "+path)
		}
		var in map[string]any
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &in)
		branch := r.URL.Query().Get("ref")
		if branch == "" {
			branch = "main"
		}
		switch {
		case path == "" && r.Method == http.MethodGet:
			reply(w, 200, map[string]any{"default_branch": "main"})
		case strings.HasPrefix(path, "/git/ref/heads/"):
			name := strings.TrimPrefix(path, "/git/ref/heads/")
			if _, ok := g.branches[name]; !ok {
				reply(w, 404, map[string]string{"message": "Not Found"})
				return
			}
			reply(w, 200, map[string]any{"object": map[string]string{"sha": "abc123"}})
		case path == "/git/refs" && r.Method == http.MethodPost:
			name := strings.TrimPrefix(in["ref"].(string), "refs/heads/")
			if _, exists := g.branches[name]; exists {
				reply(w, 422, map[string]string{"message": "Reference already exists"})
				return
			}
			copyOf := map[string]string{}
			for k, v := range g.branches["main"] {
				copyOf[k] = v
			}
			g.branches[name] = copyOf
			reply(w, 201, map[string]any{"ref": in["ref"]})
		case strings.HasPrefix(path, "/contents/") || path == "/contents":
			file := strings.TrimPrefix(strings.TrimPrefix(path, "/contents"), "/")
			if r.Method == http.MethodPut {
				to := in["branch"].(string)
				content, _ := base64.StdEncoding.DecodeString(in["content"].(string))
				g.branches[to][file] = string(content)
				reply(w, 200, map[string]any{"commit": map[string]string{"sha": "c0ffee"}})
				return
			}
			files, ok := g.branches[branch]
			if !ok {
				reply(w, 404, map[string]string{"message": "No commit found for the ref"})
				return
			}
			if content, ok := files[file]; ok {
				reply(w, 200, map[string]any{"type": "file", "encoding": "base64", "size": len(content), "sha": sha(content),
					"content": base64.StdEncoding.EncodeToString([]byte(content))})
				return
			}
			var entries []map[string]any
			for p := range files {
				if file == "" || strings.HasPrefix(p, file+"/") {
					entries = append(entries, map[string]any{"name": p, "path": p, "type": "file", "size": len(files[p])})
				}
			}
			if entries == nil {
				reply(w, 404, map[string]string{"message": "Not Found"})
				return
			}
			reply(w, 200, entries)
		case path == "/pulls" && r.Method == http.MethodPost:
			for _, p := range g.pulls {
				if p["head"] == in["head"] {
					reply(w, 422, map[string]string{"message": "A pull request already exists for acme:" + in["head"].(string)})
					return
				}
			}
			pr := map[string]any{"number": 8 + len(g.pulls), "html_url": "https://github.test/acme/app/pull/8", "head": in["head"], "title": in["title"]}
			g.pulls = append(g.pulls, pr)
			reply(w, 201, pr)
		case path == "/pulls" && r.Method == http.MethodGet:
			var open []map[string]any
			for _, p := range g.pulls {
				if "acme:"+p["head"].(string) == r.URL.Query().Get("head") {
					open = append(open, p)
				}
			}
			reply(w, 200, open)
		case path == "/pulls/7":
			reply(w, 200, map[string]any{"number": 7, "title": "Fix the greeting", "body": "It said helo.", "state": "open",
				"user": map[string]string{"login": "alice"}, "head": map[string]string{"ref": "fix/greeting", "sha": "feed"},
				"base": map[string]string{"ref": "main"}})
		case path == "/pulls/7/files":
			reply(w, 200, []map[string]any{{"filename": "src/main.go", "status": "modified", "additions": 1, "deletions": 1, "patch": "-helo\n+hello"}})
		case strings.HasPrefix(path, "/issues/") && strings.HasSuffix(path, "/comments"):
			g.comments = append(g.comments, in["body"].(string))
			reply(w, 201, map[string]any{"html_url": "https://github.test/acme/app/pull/7#c1"})
		default:
			reply(w, 404, map[string]string{"message": "Not Found"})
		}
	}))
	t.Cleanup(g.srv.Close)
	return g
}

const coder = `
weir: "0.1"
namespace: code
name: fix
steps:
  - id: work
    kind: ai
    prompt: look@1
    tools:
      read: [github.get_file, github.list_files, github.get_pull_request]
      write: [github.create_branch, github.put_file, github.open_pull_request, github.comment]
    budget: { usd: 1 }
    output: { schema: { type: object } }
`

func TestAnAgentReadsARepositoryAndProposesAChange(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool := testdb.New(ctx, t)
	eng, err := engine.New(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	const token = "ghp-test-token-value"
	reg := worker.NewRegistry()
	reg.Secrets = &secrets.Resolver{Getenv: func(k string) string { return map[string]string{"WEIR_SECRET__DEFAULT__GH_TOKEN": token}[k] }}
	worker.RegisterGitHubTools(reg, eng, nil)
	eng.HandlerExists, eng.ToolClass = reg.Exists, reg.ToolClass
	srv := httptest.NewServer(api.New(eng, reg))
	defer srv.Close()
	gh := newFakeGitHub(t)

	auth := `"auth": {"kind": "bearer", "secret": "env:GH_TOKEN"}`
	// "github" is for one repository. "everything" can reach any the token covers.
	call(t, srv.URL, "PUT", "/v1/connections/github", "", `{"base_url": "`+gh.srv.URL+`/repos/acme/app", `+auth+`}`, nil)
	call(t, srv.URL, "PUT", "/v1/connections/everything", "", `{"base_url": "`+gh.srv.URL+`", `+auth+`}`, nil)
	call(t, srv.URL, "POST", "/v1/templates/look", "", `{"user": "look"}`, nil)

	// A tool that changes things cannot be listed as a read.
	if code := call(t, srv.URL, "POST", "/v1/definitions", "", strings.Replace(coder, "read: [github.get_file,", "read: [github.put_file, github.get_file,", 1), nil); code == http.StatusCreated {
		t.Error("a workflow listing github.put_file as a read was published")
	}
	if code := call(t, srv.URL, "POST", "/v1/definitions", "", coder, nil); code != http.StatusCreated {
		t.Fatalf("publish = %d", code)
	}
	call(t, srv.URL, "POST", "/v1/runs", "", `{"namespace": "code", "name": "fix"}`, nil)
	work, err := eng.Claim(ctx, []string{"ai"}, "test", time.Minute)
	if err != nil || work == nil {
		t.Fatalf("claim: %v %v", work, err)
	}
	use := func(tool, args string) (map[string]any, error) {
		raw, err := worker.CallTool(ctx, eng, reg, work.Token, tool, json.RawMessage(args))
		var out map[string]any
		_ = json.Unmarshal(raw, &out)
		return out, err
	}
	must := func(tool, args string) map[string]any {
		t.Helper()
		out, err := use(tool, args)
		if err != nil {
			t.Fatalf("%s %s: %v", tool, args, err)
		}
		return out
	}
	refused := func(why, tool, args, want string) {
		t.Helper()
		if out, err := use(tool, args); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: got %v, %v; want an error containing %q", why, out, err, want)
		}
	}

	// Reading.
	if out := must("github.get_file", `{"path": "README.md"}`); out["content"] != "# App\n" {
		t.Errorf("get_file = %v", out)
	}
	if out := must("github.list_files", `{"path": "src"}`); len(out["entries"].([]any)) != 1 {
		t.Errorf("list_files = %v", out)
	}
	pr := must("github.get_pull_request", `{"number": 7}`)
	if pr["title"] != "Fix the greeting" || pr["head"] != "fix/greeting" || len(pr["files"].([]any)) != 1 {
		t.Errorf("get_pull_request = %v", pr)
	}
	refused("a file that is not there", "github.get_file", `{"path": "nope.md"}`, "not in the repository")
	refused("a path that climbs out", "github.get_file", `{"path": "../secrets"}`, "..")

	// Proposing a change: a branch, a commit, a pull request, a comment. Each is safe to repeat.
	if out := must("github.create_branch", `{"branch": "fix/typo"}`); out["created"] != true || out["from"] != "main" {
		t.Errorf("create_branch = %v", out)
	}
	if out := must("github.create_branch", `{"branch": "fix/typo"}`); out["created"] != false {
		t.Errorf("creating the branch again = %v, want it reported as already there", out)
	}
	put := `{"branch": "fix/typo", "path": "README.md", "content": "# The App\n", "message": "Fix the title"}`
	if out := must("github.put_file", put); out["changed"] != true {
		t.Errorf("put_file = %v", out)
	}
	if out := must("github.put_file", put); out["changed"] != false {
		t.Errorf("writing the same content again = %v, want nothing changed", out)
	}
	if out := must("github.get_file", `{"path": "README.md", "ref": "fix/typo"}`); out["content"] != "# The App\n" {
		t.Errorf("the branch's file = %v", out)
	}
	opened := must("github.open_pull_request", `{"head": "fix/typo", "title": "Fix the title", "body": "It was wrong."}`)
	again := must("github.open_pull_request", `{"head": "fix/typo", "title": "Fix the title"}`)
	if opened["opened"] != true || again["opened"] != false || again["number"] != opened["number"] {
		t.Errorf("open_pull_request = %v, then %v", opened, again)
	}
	must("github.comment", `{"number": 7, "body": "Looks right."}`)

	// The default branch cannot be written to, and nothing was sent when it was tried.
	before := len(gh.writes)
	refused("writing to the default branch", "github.put_file",
		`{"branch": "main", "path": "README.md", "content": "pwned", "message": "x"}`, "default branch")
	if len(gh.writes) != before || gh.branches["main"]["README.md"] != "# App\n" {
		t.Errorf("the default branch was written to: %v", gh.writes[before:])
	}

	// A connection for one repository cannot be turned on another. One for all needs to be told which.
	refused("another repository on a pinned connection", "github.get_file", `{"repo": "acme/payroll", "path": "README.md"}`, "is for the repository acme/app")
	refused("no repository on an unpinned connection", "github.get_file", `{"connection": "everything", "path": "README.md"}`, "say which repository")
	if out := must("github.get_file", `{"connection": "everything", "repo": "acme/app", "path": "README.md"}`); out["content"] != "# App\n" {
		t.Errorf("an unpinned connection with the repository named = %v", out)
	}

	// Every call carried the token, and the token is nowhere in the record.
	if len(gh.tokens) != 1 || !gh.tokens["Bearer "+token] {
		t.Errorf("GitHub was called with %v", gh.tokens)
	}
	if strings.Join(gh.comments, "") != "Looks right." {
		t.Errorf("comments = %v", gh.comments)
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM tool_calls t WHERE to_jsonb(t)::text LIKE '%' || $1 || '%'`, token).Scan(&n); err != nil || n != 0 {
		t.Errorf("the token is in %d recorded tool call(s) (%v)", n, err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM tool_calls WHERE status = 'succeeded'`).Scan(&n); err != nil || n < 12 {
		t.Errorf("%d successful tool calls were recorded (%v), want every one", n, err)
	}
}

// TestGitHubReadsLive reads a public repository on the real GitHub, with no
// token. It runs only when asked, and proves the read tools match GitHub's
// API as it is, not only as the stand-in above imagines it.
func TestGitHubReadsLive(t *testing.T) {
	repo := os.Getenv("WEIR_TEST_GITHUB")
	if repo == "" {
		t.Skip("set WEIR_TEST_GITHUB=owner/name (a public repository) to read from the real GitHub")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool := testdb.New(ctx, t)
	eng, err := engine.New(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	reg := worker.NewRegistry()
	worker.RegisterGitHubTools(reg, eng, nil)
	eng.HandlerExists, eng.ToolClass = reg.Exists, reg.ToolClass
	srv := httptest.NewServer(api.New(eng, reg))
	defer srv.Close()

	call(t, srv.URL, "PUT", "/v1/connections/github", "", `{"base_url": "https://api.github.com/repos/`+repo+`"}`, nil)
	call(t, srv.URL, "POST", "/v1/templates/look", "", `{"user": "look"}`, nil)
	if code := call(t, srv.URL, "POST", "/v1/definitions", "", coder, nil); code != http.StatusCreated {
		t.Fatalf("publish = %d", code)
	}
	call(t, srv.URL, "POST", "/v1/runs", "", `{"namespace": "code", "name": "fix"}`, nil)
	work, err := eng.Claim(ctx, []string{"ai"}, "test", time.Minute)
	if err != nil || work == nil {
		t.Fatalf("claim: %v %v", work, err)
	}
	use := func(tool, args string) map[string]any {
		t.Helper()
		raw, err := worker.CallTool(ctx, eng, reg, work.Token, tool, json.RawMessage(args))
		if err != nil {
			t.Fatalf("%s %s: %v", tool, args, err)
		}
		var out map[string]any
		_ = json.Unmarshal(raw, &out)
		return out
	}
	readme := use("github.get_file", `{"path": "README.md"}`)
	entries := use("github.list_files", `{}`)["entries"].([]any)
	t.Logf("README.md is %d bytes; the top of the repository has %d entries", len(readme["content"].(string)), len(entries))
	if len(readme["content"].(string)) == 0 || len(entries) == 0 {
		t.Error("the real GitHub returned nothing")
	}
	if n := os.Getenv("WEIR_TEST_GITHUB_PR"); n != "" {
		pr := use("github.get_pull_request", `{"number": `+n+`}`)
		t.Logf("pull request %s: %q, %d changed file(s), %s into %s", n, pr["title"], len(pr["files"].([]any)), pr["head"], pr["base"])
		if pr["title"] == "" || pr["base"] == "" {
			t.Errorf("the pull request came back incomplete: %v", pr)
		}
	}
}
