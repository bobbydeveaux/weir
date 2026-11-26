package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bobbydeveaux/weir/engine"
	"github.com/bobbydeveaux/weir/internal/testdb"
	"github.com/bobbydeveaux/weir/worker"
)

// fakeCheck is one check on the head commit of the stand-in's pull request.
type fakeCheck struct {
	Name       string
	Conclusion string // empty while it runs
	Required   bool
	Status     bool  // a commit status, not a check run
	Job        int64 // a GitHub Actions job, whose log can be asked for
}

// fakeChecks is enough of GitHub for the checks and merge handlers: one
// pull request, number 8 of acme/app, on the head commit "abc123".
type fakeChecks struct {
	mu         sync.Mutex
	state      string // OPEN, CLOSED or MERGED
	head       string
	checks     []fakeCheck
	expected   []string          // required by branch protection
	rulesets   []string          // required by a ruleset
	running    int               // how many more reads show every check as running
	hidden     int               // how many more reads show no checks at all
	openSuite  bool              // a set of checks has started and not finished
	base       map[string]string // check name -> conclusion on the base branch
	logs       map[int64]string
	refuse     int    // the status GitHub answers a merge with, when not 200
	noAuto     string // why auto-merge cannot be turned on
	remaining  string // X-RateLimit-Remaining, when set
	reads      int
	sent       []string // every request, as "METHOD path"
	merges     []map[string]any
	autoMerges []map[string]any
	srv        *httptest.Server
}

func newFakeChecks(t *testing.T) *fakeChecks {
	g := &fakeChecks{state: "OPEN", head: "abc123", base: map[string]string{}, logs: map[int64]string{}}
	reply := func(w http.ResponseWriter, status int, body any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}
	g.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		defer g.mu.Unlock()
		g.sent = append(g.sent, r.Method+" "+r.URL.Path)
		raw, _ := io.ReadAll(r.Body)
		var in struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		_ = json.Unmarshal(raw, &in)
		path := strings.TrimPrefix(r.URL.Path, "/repos/acme/app")
		switch {
		case r.URL.Path == "/graphql" && strings.Contains(in.Query, "enablePullRequestAutoMerge"):
			if g.noAuto != "" {
				reply(w, 200, map[string]any{"errors": []map[string]string{{"message": g.noAuto}}})
				return
			}
			g.autoMerges = append(g.autoMerges, in.Variables)
			reply(w, 200, map[string]any{"data": map[string]any{}})
		case r.URL.Path == "/graphql":
			if g.remaining != "" {
				w.Header().Set("X-RateLimit-Remaining", g.remaining)
				w.Header().Set("X-RateLimit-Reset", fmt.Sprint(time.Now().Add(200*time.Millisecond).Unix()+1))
			}
			reply(w, 200, map[string]any{"data": map[string]any{"repository": map[string]any{"pullRequest": g.pull()}}})
		case path == "/rules/branches/main":
			checks := []map[string]string{}
			for _, name := range g.rulesets {
				checks = append(checks, map[string]string{"context": name})
			}
			reply(w, 200, []map[string]any{{"type": "required_status_checks", "parameters": map[string]any{"required_status_checks": checks}}})
		case path == "/commits/main/check-runs":
			runs := []map[string]string{}
			for name, conclusion := range g.base {
				runs = append(runs, map[string]string{"name": name, "status": "completed", "conclusion": conclusion})
			}
			reply(w, 200, map[string]any{"check_runs": runs})
		case path == "/commits/main/status":
			reply(w, 200, map[string]any{"statuses": []any{}})
		case strings.HasPrefix(path, "/actions/jobs/"):
			var job int64
			_, _ = fmt.Sscanf(path, "/actions/jobs/%d/logs", &job)
			log, ok := g.logs[job]
			if !ok {
				reply(w, 403, map[string]string{"message": "Resource not accessible by integration"})
				return
			}
			_, _ = io.WriteString(w, log)
		case path == "/pulls/8/merge" && r.Method == http.MethodPut:
			var merge map[string]any
			_ = json.Unmarshal(raw, &merge)
			if g.refuse != 0 {
				reply(w, g.refuse, map[string]string{"message": "At least 1 approving review is required by reviewers with write access."})
				return
			}
			g.merges = append(g.merges, merge)
			g.state = "MERGED"
			reply(w, 200, map[string]any{"sha": "merged1", "merged": true})
		default:
			reply(w, 404, map[string]string{"message": "Not Found"})
		}
	}))
	t.Cleanup(g.srv.Close)
	return g
}

// pull is the pull request as GraphQL gives it. Called with the lock held.
func (g *fakeChecks) pull() map[string]any {
	g.reads++
	nodes := []map[string]any{}
	for _, c := range g.checks {
		if g.hidden > 0 {
			break
		}
		conclusion := c.Conclusion
		if g.running > 0 {
			conclusion = ""
		}
		if c.Status {
			state := "PENDING"
			if conclusion != "" {
				state = strings.ToUpper(conclusion)
			}
			nodes = append(nodes, map[string]any{"__typename": "StatusContext", "context": c.Name, "state": state, "isRequired": c.Required})
			continue
		}
		node := map[string]any{"__typename": "CheckRun", "name": c.Name, "status": "IN_PROGRESS", "isRequired": c.Required,
			"detailsUrl": "https://github.test/acme/app/runs/" + c.Name, "databaseId": c.Job}
		if conclusion != "" {
			node["status"], node["conclusion"] = "COMPLETED", strings.ToUpper(conclusion)
		}
		if c.Job != 0 {
			node["checkSuite"] = map[string]any{"app": map[string]string{"slug": "github-actions"}}
		}
		nodes = append(nodes, node)
	}
	if g.hidden > 0 {
		g.hidden--
	} else if g.running > 0 {
		g.running--
	}
	suites := []map[string]any{}
	if g.openSuite {
		suites = append(suites, map[string]any{"status": "IN_PROGRESS", "checkRuns": map[string]int{"totalCount": 1}})
	}
	// A set of checks an installed App never fills: it must not be waited for.
	suites = append(suites, map[string]any{"status": "QUEUED", "checkRuns": map[string]int{"totalCount": 0}})
	pr := map[string]any{"id": "PR_8", "state": g.state, "mergeStateStatus": "CLEAN", "baseRefName": "main", "headRefOid": g.head,
		"baseRef": map[string]any{"refUpdateRule": map[string]any{"requiredStatusCheckContexts": g.expected}},
		"commits": map[string]any{"nodes": []map[string]any{{"commit": map[string]any{
			"checkSuites":       map[string]any{"nodes": suites},
			"statusCheckRollup": map[string]any{"contexts": map[string]any{"nodes": nodes}}}}}}}
	if g.state == "MERGED" {
		pr["mergeCommit"] = map[string]string{"oid": "merged1"}
	}
	return pr
}

const checksAndMerge = `
weir: "0.1"
namespace: code
name: %s
steps:
  - id: work
    kind: deterministic
    handler: %s
    timeout: %s
    input:
      repo: '"acme/app"'
      pull_request: 8
      count: 'has(input.count) ? input.count : "required"'
      checks: 'has(input.checks) ? input.checks : []'
      every: '"5ms"'
      settle: '"150ms"'
      method: '"squash"'
      head_sha: 'has(input.head_sha) ? input.head_sha : ""'
      allow_no_checks: 'has(input.allow_no_checks) && input.allow_no_checks'
`

// checksBench is a database, a worker and the stand-in, with one workflow
// published for each of the three handlers.
type checksBench struct {
	t   *testing.T
	ctx context.Context
	eng *engine.Engine
	w   *worker.Worker
	gh  *fakeChecks
}

func newChecksBench(t *testing.T, ctx context.Context) *checksBench {
	eng, err := engine.New(ctx, testdb.New(ctx, t))
	if err != nil {
		t.Fatal(err)
	}
	gh := newFakeChecks(t)
	reg := worker.NewRegistry()
	worker.RegisterGitHubChecks(reg, eng, nil)
	worker.RegisterGitHubMerge(reg, eng, nil)
	eng.HandlerExists, eng.HandlerRules = reg.Exists, reg.Rules
	if err := eng.PutConnection(ctx, "github", gh.srv.URL, engine.Auth{}); err != nil {
		t.Fatal(err)
	}
	b := &checksBench{t: t, ctx: ctx, eng: eng, gh: gh, w: &worker.Worker{Engine: eng, Handlers: reg}}
	for name, handler := range map[string]string{"check": "github.checks", "merge": "github.merge_pull_request", "auto": "github.enable_auto_merge"} {
		b.publish(fmt.Sprintf(checksAndMerge, name, handler, "20s"))
	}
	return b
}

func (b *checksBench) publish(source string) *engine.Published {
	b.t.Helper()
	out, err := b.eng.Publish(b.ctx, []byte(source), "test")
	if err != nil {
		b.t.Fatalf("publish: %v", err)
	}
	return out
}

// run starts a run and works it until it stops, waiting out the delays of a
// step that asked to be run again.
func (b *checksBench) run(name, input string) engine.RunView {
	b.t.Helper()
	id, _, err := b.eng.StartRun(b.ctx, engine.StartRequest{Namespace: "code", Name: name, Requester: "alice", Input: json.RawMessage(input)})
	if err != nil {
		b.t.Fatal(err)
	}
	for {
		if _, err := b.w.RunOnce(b.ctx); err != nil {
			b.t.Fatal(err)
		}
		v, err := b.eng.Run(b.ctx, id)
		if err != nil {
			b.t.Fatal(err)
		}
		if v.Status != "running" && v.Status != "pending" {
			return *v
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// checked is the output of "github.checks".
type checked struct {
	State  string `json:"state"`
	Head   string `json:"head_sha"`
	Failed []struct {
		Name        string `json:"name"`
		Conclusion  string `json:"conclusion"`
		Log         string `json:"log"`
		FailsOnBase *bool  `json:"fails_on_base"`
	} `json:"failed"`
	Other []struct {
		Name       string `json:"name"`
		Conclusion string `json:"conclusion"`
	} `json:"other"`
}

func (b *checksBench) checks(input string) (checked, engine.RunView) {
	b.t.Helper()
	v := b.run("check", input)
	var out checked
	if v.Status != "succeeded" {
		b.t.Fatalf("run = %s", mustJSON(v))
	}
	_ = json.Unmarshal(v.Output, &out)
	return out, v
}

func TestAHandlerWaitsForAPullRequestsChecks(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	b := newChecksBench(t, ctx)
	gh := b.gh
	set := func(change func()) {
		gh.mu.Lock()
		defer gh.mu.Unlock()
		gh.checks, gh.expected, gh.rulesets, gh.running, gh.hidden, gh.openSuite = nil, nil, nil, 0, 0, false
		change()
	}

	// Every required check passes. An optional one that failed decides nothing.
	set(func() {
		gh.checks = []fakeCheck{{Name: "test", Conclusion: "success", Required: true}, {Name: "scan", Conclusion: "success", Required: true, Status: true},
			{Name: "lint-experimental", Conclusion: "failure"}}
	})
	out, _ := b.checks(`{}`)
	if out.State != "passed" || out.Head != "abc123" || len(out.Failed) != 0 || len(out.Other) != 1 || out.Other[0].Name != "lint-experimental" || out.Other[0].Conclusion != "failure" {
		t.Errorf("required checks passed, an optional one failed: %+v", out)
	}

	// A required check is still running: the step waits, and uses one attempt.
	set(func() {
		gh.checks = []fakeCheck{{Name: "test", Conclusion: "success", Required: true}}
		gh.running = 2
	})
	gh.reads = 0
	out, v := b.checks(`{}`)
	if out.State != "passed" || gh.reads != 3 || v.Steps[0].Attempts != 1 {
		t.Errorf("a check that was running: %+v after %d reads and %d attempts", out, gh.reads, v.Steps[0].Attempts)
	}

	// A required check that has not been reported yet is still to come,
	// whether a branch rule or a ruleset requires it.
	for _, where := range []*[]string{&gh.expected, &gh.rulesets} {
		set(func() {
			*where = []string{"test"}
			gh.checks = []fakeCheck{{Name: "test", Conclusion: "success", Required: true}}
			gh.hidden = 2
		})
		gh.reads = 0
		if out, _ := b.checks(`{}`); out.State != "passed" || gh.reads != 3 {
			t.Errorf("a required check not yet reported: %+v after %d reads", out, gh.reads)
		}
	}

	// A required check fails. Its log is the end of the job's log, and it
	// says how the check stands on the base branch.
	set(func() {
		gh.checks = []fakeCheck{{Name: "test", Conclusion: "failure", Required: true, Job: 41}, {Name: "scan", Conclusion: "failure", Required: true, Job: 42},
			{Name: "build", Conclusion: "skipped", Required: true}}
		gh.logs = map[int64]string{41: strings.Repeat("x", 1<<20) + "the end"}
		gh.base = map[string]string{"test": "success", "scan": "failure"}
	})
	out, _ = b.checks(`{}`)
	if out.State != "failed" || len(out.Failed) != 2 {
		t.Fatalf("a required check failed, and a skipped one passed as GitHub passes it: %+v", out)
	}
	test, scan := out.Failed[0], out.Failed[1]
	if test.Name != "test" || len(test.Log) != 16<<10 || !strings.HasSuffix(test.Log, "the end") || test.FailsOnBase == nil || *test.FailsOnBase {
		t.Errorf("the check the pull request broke: %d bytes of log, fails_on_base %v", len(test.Log), test.FailsOnBase)
	}
	// The log of the other cannot be read, and the step still succeeds.
	if scan.Name != "scan" || scan.Log != "" || scan.FailsOnBase == nil || !*scan.FailsOnBase {
		t.Errorf("the check that fails on the base too: %+v", scan)
	}

	// Nothing is required and nothing is named.
	set(func() { gh.checks = []fakeCheck{{Name: "deploy", Conclusion: "failure"}} })
	if out, _ := b.checks(`{}`); out.State != "none" {
		t.Errorf("nothing required: %+v", out)
	}
	// The same repository, with every check counting.
	if out, _ := b.checks(`{"count": "all"}`); out.State != "failed" || len(out.Failed) != 1 || out.Failed[0].Name != "deploy" {
		t.Errorf("every check counting, on a repository with no rules: %+v", out)
	}
	set(func() {
		gh.checks = []fakeCheck{{Name: "a", Conclusion: "skipped"}, {Name: "b", Conclusion: "cancelled"}}
	})
	if out, _ := b.checks(`{"count": "all"}`); out.State != "failed" || len(out.Failed) != 1 || out.Failed[0].Name != "b" {
		t.Errorf("skipped and cancelled: %+v", out)
	}
	// A check the step names must really have succeeded.
	set(func() { gh.checks = []fakeCheck{{Name: "test", Conclusion: "skipped"}} })
	if out, _ := b.checks(`{"checks": ["test"]}`); out.State != "failed" || out.Failed[0].Conclusion != "skipped" {
		t.Errorf("a named check that was skipped: %+v", out)
	}

	// With every check counting, a commit with no check yet is given time
	// for one to appear, and a set of checks that has started is waited for.
	set(func() {
		gh.checks = []fakeCheck{{Name: "deploy", Conclusion: "success"}}
		gh.hidden = 3
	})
	if out, _ := b.checks(`{"count": "all"}`); out.State != "passed" {
		t.Errorf("straight after a push: %+v", out)
	}
	set(func() {})
	began := time.Now()
	if out, _ := b.checks(`{"count": "all"}`); out.State != "none" || time.Since(began) < 150*time.Millisecond {
		t.Errorf("a repository with no pipeline: %+v after %s", out, time.Since(began))
	}
	set(func() {
		gh.checks = []fakeCheck{{Name: "first", Conclusion: "success"}}
		gh.openSuite = true
	})
	go func() {
		time.Sleep(60 * time.Millisecond)
		gh.mu.Lock()
		gh.openSuite = false
		gh.mu.Unlock()
	}()
	began = time.Now()
	if out, _ := b.checks(`{"count": "all"}`); out.State != "passed" || time.Since(began) < 60*time.Millisecond {
		t.Errorf("a later job of the same pipeline: %+v after %s", out, time.Since(began))
	}

	// Near the rate limit, the next look waits for the limit to reset.
	set(func() {
		gh.checks = []fakeCheck{{Name: "test", Conclusion: "success", Required: true}}
		gh.running, gh.remaining = 1, "5"
	})
	began = time.Now()
	if out, _ := b.checks(`{}`); out.State != "passed" || time.Since(began) < 200*time.Millisecond {
		t.Errorf("near the rate limit: %+v after %s", out, time.Since(began))
	}
	gh.mu.Lock()
	gh.remaining = ""
	gh.mu.Unlock()

	// Checks that never finish stop at the step's timeout.
	b.publish(fmt.Sprintf(checksAndMerge, "check-briefly", "github.checks", "80ms"))
	set(func() {
		gh.checks = []fakeCheck{{Name: "test", Required: true}}
	})
	if v := b.run("check-briefly", `{}`); v.Status != "blocked" || !strings.Contains(string(v.Error), `"timeout"`) {
		t.Errorf("checks that never finish: %s", mustJSON(v))
	}
	// Nothing was ever written to GitHub.
	for _, call := range gh.sent {
		if !strings.HasPrefix(call, "GET ") && call != "POST /graphql" {
			t.Errorf("the checks handler sent %s", call)
		}
	}
}

func TestPublishingChecksAStepThatWaitsOrMerges(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	b := newChecksBench(t, ctx)
	refused := func(what, source, want string) {
		t.Helper()
		if _, err := b.eng.Publish(ctx, []byte(source), "test"); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: publishing said %v, and should name %q", what, err, want)
		}
	}
	step := `
weir: "0.1"
namespace: code
name: bad
steps:
  - id: work
    kind: deterministic
    handler: %s
    %s
    input: { repo: '"acme/app"', pull_request: 8%s }
`
	refused("no timeout", fmt.Sprintf(step, "github.checks", "", ""), "must set a timeout")
	refused("a count that is not one of the two", fmt.Sprintf(step, "github.checks", "timeout: 1h", `, count: '"some"'`), "required, all")
	refused("no method", fmt.Sprintf(step, "github.merge_pull_request", "", ""), "merge, squash or rebase")
	refused("a method that is not one of the three", fmt.Sprintf(step, "github.merge_pull_request", "", `, method: '"fast"'`), "merge, squash, rebase")
	refused("auto-merge with no method", fmt.Sprintf(step, "github.enable_auto_merge", "", ""), "merge, squash or rebase")
	b.publish(fmt.Sprintf(step, "github.checks", "timeout: 2h", `, count: '"all"'`))
}

// merged is the output of the two handlers that merge.
type merged struct {
	Merged  bool   `json:"merged"`
	Enabled bool   `json:"enabled"`
	Reason  string `json:"reason"`
	Commit  string `json:"commit"`
	Head    string `json:"head_sha"`
	Checks  []struct {
		Name       string `json:"name"`
		Conclusion string `json:"conclusion"`
	} `json:"checks"`
}

func TestAHandlerMergesAPullRequestWhoseChecksPassed(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	b := newChecksBench(t, ctx)
	gh := b.gh
	merge := func(name, input string) merged {
		t.Helper()
		v := b.run(name, input)
		if v.Status != "succeeded" {
			t.Fatalf("run = %s", mustJSON(v))
		}
		var out merged
		_ = json.Unmarshal(v.Output, &out)
		return out
	}
	gh.checks = []fakeCheck{{Name: "test", Conclusion: "success", Required: true}, {Name: "scan", Conclusion: "success", Required: true}}

	// A connection that does not allow merging: refused before GitHub is asked anything.
	for _, name := range []string{"merge", "auto"} {
		if v := b.run(name, `{}`); v.Status != "blocked" || !strings.Contains(string(v.Error), "allow_merge") {
			t.Errorf("%s on a connection that does not allow merging: %s", name, mustJSON(v))
		}
	}
	if len(gh.sent) != 0 {
		t.Errorf("GitHub was sent %v through a connection that does not allow merging", gh.sent)
	}
	if err := b.eng.AllowMerge(ctx, "github", true); err != nil {
		t.Fatal(err)
	}

	// Each refusal is an answer, and merges nothing.
	refusals := []struct {
		what, input, want string
		change            func()
	}{
		{"a required check failed", `{}`, "test", func() { gh.checks[0].Conclusion = "failure" }},
		{"a required check is running", `{}`, "not finished", func() { gh.checks[0].Conclusion = "" }},
		{"the branch moved", `{"head_sha": "0ld"}`, "branch moved", func() {}},
		{"a rule refuses", `{}`, "approving review", func() { gh.refuse = 405 }},
		{"no check counts", `{}`, "allow_no_checks", func() { gh.checks = nil }},
		{"a red pipeline on a repository with no rules", `{"count": "all"}`, "deploy", func() { gh.checks = []fakeCheck{{Name: "deploy", Conclusion: "failure"}} }},
		{"the pull request is closed", `{}`, "closed", func() { gh.state = "CLOSED" }},
	}
	for _, r := range refusals {
		gh.mu.Lock()
		was, state := append([]fakeCheck{}, gh.checks...), gh.state
		r.change()
		gh.mu.Unlock()
		out := merge("merge", r.input)
		if out.Merged || !strings.Contains(out.Reason, r.want) || len(gh.merges) != 0 {
			t.Errorf("%s: %+v", r.what, out)
		}
		gh.mu.Lock()
		gh.checks, gh.state, gh.refuse = was, state, 0
		gh.mu.Unlock()
	}

	// A squash merge, at the commit that was checked. What was seen is kept.
	out := merge("merge", `{"head_sha": "abc123"}`)
	if !out.Merged || out.Commit != "merged1" || out.Head != "abc123" || len(out.Checks) != 2 || out.Checks[0].Name != "test" || out.Checks[0].Conclusion != "success" {
		t.Errorf("a squash merge: %+v", out)
	}
	if len(gh.merges) != 1 || gh.merges[0]["merge_method"] != "squash" || gh.merges[0]["sha"] != "abc123" {
		t.Errorf("GitHub was asked to merge with %v", gh.merges)
	}
	// Run again: it says so, and gives the same commit.
	if again := merge("merge", `{}`); again.Merged || !strings.Contains(again.Reason, "already merged") || again.Commit != "merged1" || len(gh.merges) != 1 {
		t.Errorf("a second go at the merge: %+v", again)
	}

	// No check counts, and the step allows that.
	gh.mu.Lock()
	gh.state, gh.checks, gh.merges = "OPEN", nil, nil
	gh.mu.Unlock()
	if out := merge("merge", `{"allow_no_checks": true}`); !out.Merged {
		t.Errorf("no checks, and that is allowed: %+v", out)
	}

	// GitHub's own auto-merge, turned on and refused.
	if out := merge("auto", `{}`); !out.Enabled || len(gh.autoMerges) != 1 || gh.autoMerges[0]["method"] != "SQUASH" || gh.autoMerges[0]["id"] != "PR_8" {
		t.Errorf("auto-merge: %+v, asked with %v", out, gh.autoMerges)
	}
	gh.mu.Lock()
	gh.noAuto = "Auto merge is not allowed for this repository"
	gh.mu.Unlock()
	if out := merge("auto", `{}`); out.Enabled || !strings.Contains(out.Reason, "not allowed") || len(gh.autoMerges) != 1 {
		t.Errorf("auto-merge on a repository that has it turned off: %+v", out)
	}
}

const loop = `
weir: "0.1"
namespace: code
name: %s
steps:
  - id: check
    kind: deterministic
    handler: test.ready_third
    timeout: %s
    next:
      - when: %s
        go: fix
      - go: stop
  - id: fix
    kind: deterministic
    handler: core.noop
    next: [{ go: check }]
  - id: stop
    kind: deterministic
    handler: core.noop
    end: true
`

func TestARuleStopsALoopAndAHandlerAsksToBeRunAgain(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	b := newChecksBench(t, ctx)
	// A handler that is ready the third time it is run, for each visit.
	var mu sync.Mutex
	runs, never := map[string]int{}, false
	b.w.Handlers.Register("test.ready_third", func(_ context.Context, c worker.Call) (json.RawMessage, error) {
		mu.Lock()
		defer mu.Unlock()
		runs[c.IdempotencyKey]++
		if never || c.Tries < 2 {
			return nil, worker.NotYet(5 * time.Millisecond)
		}
		if c.Waited <= 0 {
			return nil, fmt.Errorf("the handler was not told how long it had waited")
		}
		return json.RawMessage(`{}`), nil
	})

	// A loop with a limit: no warning, three visits to check and two to fix.
	if out := b.publish(fmt.Sprintf(loop, "bounded", "20s", "steps.check.visit < 3")); len(out.Warnings) != 0 {
		t.Errorf("a loop with a limit was warned of: %v", out.Warnings)
	}
	v := b.run("bounded", `{}`)
	visits := map[string]int{}
	for _, s := range v.Steps {
		visits[s.Key]++
		// Being run again uses none of the step's attempts.
		if s.Key == "check" && s.Attempts != 1 {
			t.Errorf("visit %d of check used %d attempts", s.Visit, s.Attempts)
		}
	}
	if v.Status != "succeeded" || visits["check"] != 3 || visits["fix"] != 2 || visits["stop"] != 1 {
		t.Errorf("the loop: %s with visits %v", v.Status, visits)
	}
	for key, n := range runs {
		if n != 3 {
			t.Errorf("the handler ran %d times for visit %s", n, key)
		}
	}

	// A loop without one is published, with a warning that names it.
	out := b.publish(fmt.Sprintf(loop, "unbounded", "20s", `'"a" == "a"'`))
	if len(out.Warnings) != 1 || !strings.Contains(out.Warnings[0], "check → fix → check") {
		t.Errorf("a loop with no limit: %v", out.Warnings)
	}

	// A handler that is never ready stops at the step's timeout.
	b.publish(fmt.Sprintf(loop, "never", "60ms", "steps.check.visit < 3"))
	mu.Lock()
	never, runs = true, map[string]int{}
	mu.Unlock()
	v = b.run("never", `{}`)
	if v.Status != "blocked" || !strings.Contains(string(v.Error), `"timeout"`) {
		t.Fatalf("a handler that is never ready: %s", mustJSON(v))
	}
	mu.Lock()
	ran := runs[""]
	for _, n := range runs {
		ran = n
	}
	mu.Unlock()
	time.Sleep(30 * time.Millisecond)
	if found, _ := b.w.RunOnce(ctx); found {
		t.Error("the handler was run again after its step timed out")
	}
	if ran < 2 {
		t.Errorf("the handler ran %d times before its timeout", ran)
	}
}
