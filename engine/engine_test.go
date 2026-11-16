package engine_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bobbydeveaux/weir/engine"
	"github.com/bobbydeveaux/weir/internal/testdb"
	"github.com/bobbydeveaux/weir/runner"
	"github.com/bobbydeveaux/weir/worker"
)

// harness is an engine, a worker and a fake runner on a private schema, so
// tests cannot see each other's steps.
type harness struct {
	t      *testing.T
	ctx    context.Context
	pool   *pgxpool.Pool
	eng    *engine.Engine
	worker *worker.Worker
	fake   *runner.Fake
	reg    *worker.Registry
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	pool := testdb.New(ctx, t)

	eng, err := engine.New(ctx, pool)
	if err != nil {
		t.Fatalf("engine.New: %v", err)
	}
	eng.BackoffBase = time.Millisecond
	reg := worker.NewRegistry()
	eng.HandlerExists = reg.Exists
	fake := &runner.Fake{Decisions: map[string]runner.DecideResult{}, Outputs: map[string][]json.RawMessage{}, Errors: map[string]error{}}
	w := &worker.Worker{Engine: eng, Runner: fake, Handlers: reg, ID: "test-worker", Lease: 5 * time.Second,
		CallbackURL: "http://weir.test", PollEvery: 5 * time.Millisecond}
	return &harness{t: t, ctx: ctx, pool: pool, eng: eng, worker: w, fake: fake, reg: reg}
}

func (h *harness) template(key string) {
	h.t.Helper()
	if _, err := h.eng.PublishTemplate(h.ctx, key, engine.Template{User: "{{input}}"}, "test"); err != nil {
		h.t.Fatalf("publish template %s: %v", key, err)
	}
}

func (h *harness) publish(yaml string) {
	h.t.Helper()
	if _, err := h.eng.Publish(h.ctx, []byte(yaml), "test"); err != nil {
		h.t.Fatalf("publish: %v", err)
	}
}

func (h *harness) start(name, requester, input string) string {
	h.t.Helper()
	id, _, err := h.eng.StartRun(h.ctx, engine.StartRequest{Namespace: "test", Name: name, Requester: requester, Input: json.RawMessage(input)})
	if err != nil {
		h.t.Fatalf("start %s: %v", name, err)
	}
	return id
}

// settle runs the worker until there has been nothing to do for a while.
func (h *harness) settle() {
	h.t.Helper()
	idle := 0
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline) && idle < 8; {
		found, err := h.worker.RunOnce(h.ctx)
		if err != nil {
			h.t.Fatalf("worker: %v", err)
		}
		if found {
			idle = 0
			continue
		}
		idle++
		time.Sleep(5 * time.Millisecond)
	}
}

func (h *harness) run(id string) *engine.RunView {
	h.t.Helper()
	v, err := h.eng.Run(h.ctx, id)
	if err != nil {
		h.t.Fatalf("read run: %v", err)
	}
	return v
}

func step(v *engine.RunView, key string) *engine.StepView {
	for i := len(v.Steps) - 1; i >= 0; i-- {
		if v.Steps[i].Key == key {
			return &v.Steps[i]
		}
	}
	return nil
}

func eventTypes(v *engine.RunView) []string {
	var out []string
	for _, e := range v.Events {
		out = append(out, e.Type)
	}
	return out
}

func wantStatus(t *testing.T, v *engine.RunView, want string) {
	t.Helper()
	if v.Status != want {
		t.Fatalf("run status = %s, want %s (error: %s; events: %v)", v.Status, want, v.Error, eventTypes(v))
	}
}

func errorType(raw json.RawMessage) string {
	var f struct {
		Type string `json:"type"`
	}
	_ = json.Unmarshal(raw, &f)
	return f.Type
}

const allKinds = `
weir: "0.1"
namespace: test
name: all-kinds
budget: { usd: 50 }
steps:
  - id: route
    kind: decide
    template: route@1
    labels: [fix, needs_human]
    confidence: 0.8
    default: needs_human
    next:
      - when: label == "fix"
        go: open_issue
      - go: needs_human
  - id: open_issue
    kind: deterministic
    handler: test.open_issue
    input:
      title: input.title
  - id: fix
    kind: ai
    prompt: fix@1
    input:
      issue: steps.open_issue.output.number
    output:
      schema:
        type: object
        required: [branch]
        properties:
          branch: { type: string }
    budget: { usd: 15 }
  - id: approve
    kind: human
    assign: { role: approver, not: run.requester }
    timeout: 1h
    next:
      - when: outcome == "rejected"
        go: needs_human
  - id: verify
    kind: external
    connection: detector
    call:
      path: /verify
      body:
        branch: steps.fix.output.branch
    resume: callback
    timeout: 1h
    end: true
  - id: needs_human
    kind: human
    assign: { role: on_call }
`

// thirdParty records what an external step sent it.
type thirdParty struct {
	mu     sync.Mutex
	server *httptest.Server
	tokens []string
	bodies []string
	reply  func(n int) string
}

func newThirdParty(t *testing.T) *thirdParty {
	tp := &thirdParty{reply: func(int) string { return `{"accepted": true}` }}
	tp.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		tp.mu.Lock()
		tp.tokens = append(tp.tokens, r.Header.Get("Weir-Callback-Token"))
		tp.bodies = append(tp.bodies, string(body))
		n := len(tp.bodies)
		tp.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, tp.reply(n))
	}))
	t.Cleanup(tp.server.Close)
	return tp
}

func (h *harness) setUpAllKinds() *thirdParty {
	h.t.Helper()
	tp := newThirdParty(h.t)
	h.template("route")
	h.template("fix")
	if err := h.eng.PutConnection(h.ctx, "detector", tp.server.URL); err != nil {
		h.t.Fatal(err)
	}
	h.reg.Register("test.open_issue", func(context.Context, worker.Call) (json.RawMessage, error) {
		return json.RawMessage(`{"number": 482}`), nil
	})
	h.fake.Decisions["route"] = runner.DecideResult{Label: "fix", Confidence: 0.95}
	h.fake.Outputs["fix"] = []json.RawMessage{json.RawMessage(`{"branch": "fix/482"}`)}
	h.fake.CostUSD = 3.20
	h.publish(allKinds)
	return tp
}

func TestARunThroughAllFiveKinds(t *testing.T) {
	h := newHarness(t)
	tp := h.setUpAllKinds()
	id := h.start("all-kinds", "alice", `{"title": "Checkout is down"}`)

	h.settle()
	v := h.run(id)
	wantStatus(t, v, "waiting")
	if s := step(v, "approve"); s == nil || s.Status != "waiting" {
		t.Fatalf("approve step = %+v, want it waiting", s)
	}

	tasks, err := h.eng.Tasks(h.ctx)
	if err != nil || len(tasks) != 1 {
		t.Fatalf("Tasks = %v, %v; want one task", tasks, err)
	}
	if err := h.eng.Decide(h.ctx, tasks[0].ID, "bob", "approved", "looks right"); err != nil {
		t.Fatalf("Decide: %v", err)
	}

	h.settle()
	v = h.run(id)
	wantStatus(t, v, "waiting")
	if len(tp.tokens) != 1 || tp.tokens[0] == "" {
		t.Fatalf("the third party was sent %d tokens, want 1", len(tp.tokens))
	}
	if !strings.Contains(tp.bodies[0], "fix/482") {
		t.Fatalf("the third party was sent %s, want the branch from the fix step", tp.bodies[0])
	}

	callback := engine.Result{Output: json.RawMessage(`{"cleared": true}`), Actor: "connection:detector"}
	if err := h.eng.Complete(h.ctx, tp.tokens[0], callback); err != nil {
		t.Fatalf("callback: %v", err)
	}
	v = h.run(id)
	wantStatus(t, v, "succeeded")
	if v.SpentUSD != 3.20 {
		t.Errorf("spent = %v, want 3.20", v.SpentUSD)
	}
	if s := step(v, "needs_human"); s != nil {
		t.Errorf("needs_human ran, but the run should have ended at verify")
	}

	want := []string{
		"run.started",
		"step.ready", "attempt.started", "step.succeeded", // route
		"step.ready", "attempt.started", "step.succeeded", // open_issue
		"step.ready", "attempt.started", "step.succeeded", // fix
		"step.waiting", "step.succeeded", // approve
		"step.ready", "attempt.started", "step.waiting", "step.succeeded", // verify
		"run.succeeded",
	}
	if got := eventTypes(v); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("event log:\n got %v\nwant %v", got, want)
	}

	// A repeated callback must change nothing.
	if err := h.eng.Complete(h.ctx, tp.tokens[0], callback); !errors.Is(err, engine.ErrStaleToken) {
		t.Fatalf("second callback = %v, want ErrStaleToken", err)
	}
}

func TestTheRequesterCannotApproveTheirOwnRun(t *testing.T) {
	h := newHarness(t)
	h.setUpAllKinds()
	id := h.start("all-kinds", "alice", `{"title": "Checkout is down"}`)
	h.settle()

	tasks, _ := h.eng.Tasks(h.ctx)
	if len(tasks) != 1 {
		t.Fatalf("tasks = %d, want 1", len(tasks))
	}
	if err := h.eng.Decide(h.ctx, tasks[0].ID, "alice", "approved", ""); !errors.Is(err, engine.ErrNotAllowed) {
		t.Fatalf("Decide by the requester = %v, want ErrNotAllowed", err)
	}
	if err := h.eng.Decide(h.ctx, tasks[0].ID, "bob", "maybe", ""); err == nil {
		t.Fatal("an outcome that is not on the list was accepted")
	}
	wantStatus(t, h.run(id), "waiting")

	if err := h.eng.Decide(h.ctx, tasks[0].ID, "bob", "rejected", ""); err != nil {
		t.Fatalf("Decide: %v", err)
	}
	v := h.run(id)
	if s := step(v, "needs_human"); s == nil || s.Status != "waiting" {
		t.Fatalf("after a rejection the run should wait at needs_human; steps: %+v", v.Steps)
	}
	if err := h.eng.Decide(h.ctx, tasks[0].ID, "bob", "approved", ""); !errors.Is(err, engine.ErrTaskClosed) {
		t.Fatalf("deciding a closed task = %v, want ErrTaskClosed", err)
	}
}

func TestAnUnsureClassifierFallsBackToTheDefault(t *testing.T) {
	h := newHarness(t)
	h.setUpAllKinds()
	h.fake.Decisions["route"] = runner.DecideResult{Label: "fix", Confidence: 0.4}
	id := h.start("all-kinds", "alice", `{"title": "Something odd"}`)
	h.settle()

	v := h.run(id)
	if s := step(v, "route"); s == nil || s.Label != "needs_human" {
		t.Fatalf("route = %+v, want the default label", s)
	}
	if s := step(v, "needs_human"); s == nil || s.Status != "waiting" {
		t.Fatalf("the run should be waiting at needs_human; steps: %+v", v.Steps)
	}
	if s := step(v, "fix"); s != nil {
		t.Error("the agent ran although the classifier was unsure")
	}

	// A classifier that is down gives the same safe result.
	h.fake.Errors["route"] = errors.New("provider unavailable")
	id = h.start("all-kinds", "alice", `{"title": "Another"}`)
	h.settle()
	if s := step(h.run(id), "route"); s == nil || s.Status != "succeeded" || s.Label != "needs_human" {
		t.Fatalf("route with a failed classifier = %+v, want it to succeed with the default", s)
	}
}

func TestAKilledWorkerDoesNotLoseOrRepeatAStep(t *testing.T) {
	h := newHarness(t)
	calls := 0
	h.reg.Register("test.count", func(context.Context, worker.Call) (json.RawMessage, error) {
		calls++
		return json.RawMessage(`{"ok": true}`), nil
	})
	h.publish(`
weir: "0.1"
namespace: test
name: crash
steps:
  - id: work
    kind: deterministic
    handler: test.count
`)
	id := h.start("crash", "alice", `{}`)

	// A worker claims the step and then dies without reporting back.
	lost, err := h.eng.Claim(h.ctx, []string{"deterministic"}, "doomed", 30*time.Millisecond)
	if err != nil || lost == nil {
		t.Fatalf("Claim = %v, %v; want work", lost, err)
	}
	if again, _ := h.eng.Claim(h.ctx, []string{"deterministic"}, "other", time.Second); again != nil {
		t.Fatal("a step that is already held was handed to a second worker")
	}
	time.Sleep(60 * time.Millisecond)
	swept, err := h.eng.Sweep(h.ctx)
	if err != nil || swept.ExpiredLeases != 1 {
		t.Fatalf("Sweep = %+v, %v; want one expired lease", swept, err)
	}

	h.settle()
	v := h.run(id)
	wantStatus(t, v, "succeeded")
	if calls != 1 {
		t.Errorf("the handler ran %d times, want exactly once", calls)
	}

	// The dead worker's token is worthless now.
	if err := h.eng.Complete(h.ctx, lost.Token, engine.Result{Output: json.RawMessage(`{}`)}); !errors.Is(err, engine.ErrStaleToken) {
		t.Fatalf("completing with the lost token = %v, want ErrStaleToken", err)
	}
}

func TestRetriesThenBlocksThenResumes(t *testing.T) {
	h := newHarness(t)
	calls := 0
	h.reg.Register("test.flaky", func(context.Context, worker.Call) (json.RawMessage, error) {
		calls++
		if calls <= 2 {
			return nil, errors.New("upstream is down")
		}
		return json.RawMessage(`{"ok": true}`), nil
	})
	h.publish(`
weir: "0.1"
namespace: test
name: flaky
steps:
  - id: first
    kind: deterministic
    handler: core.noop
  - id: work
    kind: deterministic
    handler: test.flaky
    retry: { attempts: 2, backoff: 1ms }
`)
	id := h.start("flaky", "alice", `{}`)
	h.settle()

	v := h.run(id)
	wantStatus(t, v, "blocked")
	if s := step(v, "work"); s.Status != "failed" || s.Attempts != 2 {
		t.Fatalf("work = %+v, want it failed after 2 attempts", s)
	}
	if s := step(v, "first"); s.Status != "succeeded" {
		t.Fatalf("first = %+v, want its result kept", s)
	}

	if err := h.eng.Resume(h.ctx, id, "ops", 0); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	h.settle()
	v = h.run(id)
	wantStatus(t, v, "succeeded")
	if calls != 3 {
		t.Errorf("the handler ran %d times, want 3", calls)
	}
	if err := h.eng.Resume(h.ctx, id, "ops", 0); !errors.Is(err, engine.ErrNotBlocked) {
		t.Fatalf("resuming a finished run = %v, want ErrNotBlocked", err)
	}
}

func TestCancelUndoesCompletedStepsInReverse(t *testing.T) {
	h := newHarness(t)
	var undone []string
	h.reg.Register("test.make", func(_ context.Context, c worker.Call) (json.RawMessage, error) {
		return json.RawMessage(`{"made": "` + c.Step + `"}`), nil
	})
	h.reg.Register("test.undo", func(_ context.Context, c worker.Call) (json.RawMessage, error) {
		var in struct {
			Output struct {
				Made string `json:"made"`
			} `json:"output"`
		}
		_ = json.Unmarshal(c.Input, &in)
		undone = append(undone, in.Output.Made)
		return json.RawMessage(`{}`), nil
	})
	h.reg.Register("test.boom", func(context.Context, worker.Call) (json.RawMessage, error) {
		return nil, worker.Permanent(errors.New("cannot be done"))
	})
	h.publish(`
weir: "0.1"
namespace: test
name: saga
steps:
  - id: one
    kind: deterministic
    handler: test.make
    compensate: test.undo
  - id: two
    kind: deterministic
    handler: test.make
    compensate: test.undo
  - id: three
    kind: deterministic
    handler: test.boom
    on_failure: cancel
`)
	id := h.start("saga", "alice", `{}`)
	h.settle()

	v := h.run(id)
	wantStatus(t, v, "failed")
	if strings.Join(undone, ",") != "two,one" {
		t.Errorf("undone = %v, want two then one", undone)
	}
	for _, key := range []string{"one", "two"} {
		if s := step(v, key); s.Status != "compensated" {
			t.Errorf("%s = %s, want compensated", key, s.Status)
		}
	}
	if errorType(v.Error) != "handler_failed" {
		t.Errorf("run error = %s, want the original failure", v.Error)
	}
}

func TestContinueCarriesOnPastAFailedStep(t *testing.T) {
	h := newHarness(t)
	h.reg.Register("test.boom", func(context.Context, worker.Call) (json.RawMessage, error) {
		return nil, worker.Permanent(errors.New("optional thing failed"))
	})
	h.publish(`
weir: "0.1"
namespace: test
name: optional
steps:
  - id: notify
    kind: deterministic
    handler: test.boom
    on_failure: continue
  - id: work
    kind: deterministic
    handler: core.noop
`)
	id := h.start("optional", "alice", `{}`)
	h.settle()
	v := h.run(id)
	wantStatus(t, v, "succeeded")
	if s := step(v, "notify"); s.Status != "failed" {
		t.Errorf("notify = %s, want failed and recorded", s.Status)
	}
}

func TestARunStopsWhenItsBudgetIsSpent(t *testing.T) {
	h := newHarness(t)
	h.template("fix")
	h.fake.Outputs["fix"] = []json.RawMessage{json.RawMessage(`{"branch": "b"}`)}
	h.fake.CostUSD = 6
	h.publish(`
weir: "0.1"
namespace: test
name: budget
budget: { usd: 5 }
steps:
  - id: fix
    kind: ai
    prompt: fix@1
    output:
      schema: { type: object, required: [branch] }
    budget: { usd: 10 }
  - id: after
    kind: deterministic
    handler: core.noop
`)
	id := h.start("budget", "alice", `{}`)
	h.settle()

	v := h.run(id)
	wantStatus(t, v, "blocked")
	if errorType(v.Error) != "budget_exceeded" {
		t.Fatalf("run error = %s, want budget_exceeded", v.Error)
	}
	if s := step(v, "after"); s != nil {
		t.Fatal("a step ran after the budget was spent")
	}
	if err := h.eng.Resume(h.ctx, id, "ops", 0); !errors.Is(err, engine.ErrNotAllowed) {
		t.Fatalf("resuming without more budget = %v, want ErrNotAllowed", err)
	}
	if err := h.eng.Resume(h.ctx, id, "ops", 20); err != nil {
		t.Fatalf("Resume with a larger budget: %v", err)
	}
	h.settle()
	wantStatus(t, h.run(id), "succeeded")
}

func TestOutputThatFailsItsSchemaIsRejected(t *testing.T) {
	h := newHarness(t)
	h.template("fix")
	h.fake.Outputs["fix"] = []json.RawMessage{json.RawMessage(`{"wrong": 1}`)}
	h.publish(`
weir: "0.1"
namespace: test
name: schema
steps:
  - id: fix
    kind: ai
    prompt: fix@1
    output:
      schema: { type: object, required: [branch] }
    budget: { usd: 10 }
`)
	id := h.start("schema", "alice", `{}`)
	h.settle()
	v := h.run(id)
	wantStatus(t, v, "blocked")
	if s := step(v, "fix"); s.Status != "failed" || errorType(s.Error) != "output_invalid" {
		t.Fatalf("fix = %+v (error %s), want it failed with output_invalid", s, s.Error)
	}
}

func TestAnExternalStepPollsUntilItsConditionHolds(t *testing.T) {
	h := newHarness(t)
	tp := newThirdParty(t)
	tp.reply = func(n int) string {
		if n < 3 {
			return `{"status": "pending"}`
		}
		return `{"status": "done", "finding": "gone"}`
	}
	if err := h.eng.PutConnection(h.ctx, "scanner", tp.server.URL); err != nil {
		t.Fatal(err)
	}
	h.publish(`
weir: "0.1"
namespace: test
name: poll
steps:
  - id: scan
    kind: external
    connection: scanner
    call: { method: GET, path: /status }
    resume: poll
    poll: { every: 5ms, until: response.status == "done" }
`)
	id := h.start("poll", "alice", `{}`)
	h.settle()
	v := h.run(id)
	wantStatus(t, v, "succeeded")
	if len(tp.bodies) != 3 {
		t.Errorf("the third party was called %d times, want 3", len(tp.bodies))
	}
	if s := step(v, "scan"); s.Attempts != 1 || !strings.Contains(string(s.Output), "gone") {
		t.Errorf("scan = %+v, want one counted attempt and the final reply as output", s)
	}
}

func TestAHumanStepEscalatesAndThenTimesOut(t *testing.T) {
	h := newHarness(t)
	h.publish(`
weir: "0.1"
namespace: test
name: slow
steps:
  - id: approve
    kind: human
    assign: { role: approver }
    timeout: 250ms
    escalate:
      - { after: 60ms, role: lead }
`)
	id := h.start("slow", "alice", `{}`)

	time.Sleep(100 * time.Millisecond)
	swept, err := h.eng.Sweep(h.ctx)
	if err != nil || swept.Escalations != 1 {
		t.Fatalf("Sweep = %+v, %v; want one escalation", swept, err)
	}
	tasks, _ := h.eng.Tasks(h.ctx)
	if len(tasks) != 1 || tasks[0].Role != "lead" || tasks[0].Level != 1 {
		t.Fatalf("tasks = %+v, want it escalated to lead", tasks)
	}

	time.Sleep(220 * time.Millisecond)
	swept, err = h.eng.Sweep(h.ctx)
	if err != nil || swept.Timeouts != 1 {
		t.Fatalf("Sweep = %+v, %v; want one timeout", swept, err)
	}
	v := h.run(id)
	wantStatus(t, v, "blocked")
	if errorType(v.Error) != "timeout" {
		t.Errorf("run error = %s, want timeout", v.Error)
	}
	if tasks, _ := h.eng.Tasks(h.ctx); len(tasks) != 0 {
		t.Errorf("an expired task is still in the inbox: %+v", tasks)
	}
}

func TestStartingTwiceWithTheSameKeyReturnsTheSameRun(t *testing.T) {
	h := newHarness(t)
	h.publish(`
weir: "0.1"
namespace: test
name: once
steps:
  - id: work
    kind: deterministic
    handler: core.noop
`)
	req := engine.StartRequest{Namespace: "test", Name: "once", Requester: "alice", IdempotencyKey: "order-42"}
	first, created, err := h.eng.StartRun(h.ctx, req)
	if err != nil || !created {
		t.Fatalf("first start = %s, %v, %v", first, created, err)
	}
	second, created, err := h.eng.StartRun(h.ctx, req)
	if err != nil || created || second != first {
		t.Fatalf("second start = %s, %v, %v; want the first run back", second, created, err)
	}
}

func TestPublishRefusesWhatCannotRun(t *testing.T) {
	h := newHarness(t)
	_, err := h.eng.Publish(h.ctx, []byte(`
weir: "0.1"
namespace: test
name: bad
steps:
  - id: work
    kind: deterministic
    handler: nobody.home
`), "test")
	if err == nil || !strings.Contains(err.Error(), "is not registered") {
		t.Fatalf("Publish with an unknown handler = %v", err)
	}
	_, err = h.eng.Publish(h.ctx, []byte(`
weir: "0.1"
namespace: test
name: bad
steps:
  - id: route
    kind: decide
    template: missing@1
    labels: [a, b]
    default: a
`), "test")
	if !errors.Is(err, engine.ErrNotFound) {
		t.Fatalf("Publish with a missing template = %v, want ErrNotFound", err)
	}
}

func TestTheDatabaseRefusesToRewriteHistory(t *testing.T) {
	h := newHarness(t)
	h.publish(`
weir: "0.1"
namespace: test
name: history
steps:
  - id: work
    kind: deterministic
    handler: core.noop
`)
	h.start("history", "alice", `{}`)
	for _, sql := range []string{
		`UPDATE definition_versions SET source = 'tampered'`,
		`DELETE FROM definition_versions`,
		`UPDATE events SET actor = 'someone else'`,
		`DELETE FROM events`,
	} {
		if _, err := h.pool.Exec(h.ctx, sql); err == nil {
			t.Errorf("%q succeeded, want the database to refuse", sql)
		}
	}
}

func TestPublishingAgainMakesANewVersion(t *testing.T) {
	h := newHarness(t)
	const def = `
weir: "0.1"
namespace: test
name: versions
steps:
  - id: work
    kind: deterministic
    handler: core.noop
`
	first, err := h.eng.Publish(h.ctx, []byte(def), "test")
	if err != nil {
		t.Fatal(err)
	}
	second, err := h.eng.Publish(h.ctx, []byte(def), "test")
	if err != nil {
		t.Fatal(err)
	}
	if first.Version != 1 || second.Version != 2 {
		t.Fatalf("versions = %d, %d; want 1, 2", first.Version, second.Version)
	}
}
