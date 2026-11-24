package engine_test

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/bobbydeveaux/weir/engine"
)

// waitsForMerge waits for a pull request to be closed, and goes one way if it
// was merged and another if it was not.
const waitsForMerge = `
weir: "0.1"
namespace: test
name: merge
steps:
  - id: merged
    kind: external
    resume: event
    event: github.pull_request
    when: event.data.action == "closed" && event.data.number == input.number
    timeout: 1h
    next:
      - when: "!output.data.merged"
        go: abandoned
  - id: deploy
    kind: deterministic
    handler: core.echo
    input: { deployed: steps.merged.output.data.number }
    end: true
  - id: abandoned
    kind: deterministic
    handler: core.echo
    input: { abandoned: steps.merged.output.data.number }
`

func (h *harness) emit(typ, id, data string) *engine.EventResult {
	h.t.Helper()
	out, err := h.eng.Emit(h.ctx, engine.Event{Type: typ, ID: id, Source: "github", Data: json.RawMessage(data)}, "webhook:github")
	if err != nil {
		h.t.Fatalf("emit %s %s: %v", typ, id, err)
	}
	return out
}

func pullRequest(action string, number int, merged bool) string {
	return fmt.Sprintf(`{"action": %q, "number": %d, "merged": %t}`, action, number, merged)
}

func TestAStepWaitsForTheEventThatIsAboutItsRun(t *testing.T) {
	h := newHarness(t)
	h.publish(waitsForMerge)
	one := h.start("merge", "alice", `{"number": 1}`)
	two := h.start("merge", "alice", `{"number": 2}`)
	h.settle()
	for _, id := range []string{one, two} {
		v := h.run(id)
		wantStatus(t, v, "waiting")
		if s := step(v, "merged"); s == nil || s.Status != "waiting" {
			t.Fatalf("merged = %+v, want it waiting", s)
		}
	}

	// Events that are about another pull request, or are not the one waited
	// for, move nothing.
	for i, data := range []string{pullRequest("closed", 3, true), pullRequest("opened", 1, false)} {
		if out := h.emit("github.pull_request", fmt.Sprintf("other-%d", i), data); len(out.Resumed) != 0 {
			t.Fatalf("an event that is not this run's resumed %+v", out.Resumed)
		}
	}
	if out := h.emit("github.push", "push-1", pullRequest("closed", 1, true)); len(out.Resumed) != 0 {
		t.Fatalf("an event of another type resumed %+v", out.Resumed)
	}
	wantStatus(t, h.run(one), "waiting")

	// The one that is about run one moves run one, and only run one.
	out := h.emit("github.pull_request", "d-1", pullRequest("closed", 1, true))
	if len(out.Resumed) != 1 || out.Resumed[0].RunID != one || out.Resumed[0].Step != "merged" || out.Resumed[0].Error != "" {
		t.Fatalf("resumed = %+v, want run one's merged step", out.Resumed)
	}
	h.settle()
	v := h.run(one)
	wantStatus(t, v, "succeeded")
	if !strings.Contains(string(v.Output), `"deployed"`) {
		t.Errorf("run one's output = %s, want it to have deployed", v.Output)
	}
	var took struct {
		Type, ID string
		SentBy   string `json:"sent_by"`
		Data     struct{ Number int }
	}
	if err := json.Unmarshal(step(v, "merged").Output, &took); err != nil {
		t.Fatal(err)
	}
	if took.Type != "github.pull_request" || took.ID != "d-1" || took.SentBy != "webhook:github" || took.Data.Number != 1 {
		t.Errorf("merged's output = %s, want the event it took", step(v, "merged").Output)
	}
	wantStatus(t, h.run(two), "waiting")

	// The same delivery again changes nothing, and reports what it did then.
	again := h.emit("github.pull_request", "d-1", pullRequest("closed", 1, true))
	if !again.Duplicate || len(again.Resumed) != 1 || again.Resumed[0].RunID != one {
		t.Errorf("a repeated event = %+v, want it recognised with what it resumed", again)
	}

	// A pull request closed without being merged also ends the wait, and the
	// run can go another way.
	h.emit("github.pull_request", "d-2", pullRequest("closed", 2, false))
	h.settle()
	v = h.run(two)
	wantStatus(t, v, "succeeded")
	if !strings.Contains(string(v.Output), `"abandoned"`) || step(v, "deploy") != nil {
		t.Errorf("run two's output = %s, want it abandoned and never deployed", v.Output)
	}
}

func TestAnEventThatArrivesBeforeTheStepStartsWaitingIsNotMissed(t *testing.T) {
	h := newHarness(t)
	h.publish(waitsForMerge)

	// An event from before the run started is not this run's, even though
	// the rule would match it.
	h.emit("github.pull_request", "old", pullRequest("closed", 7, true))
	id := h.start("merge", "alice", `{"number": 7}`)
	h.settle()
	wantStatus(t, h.run(id), "waiting")

	// The run has started and its step is ready, but no worker has reached
	// it. The event finds nothing waiting; the step must find the event.
	early := h.start("merge", "alice", `{"number": 8}`)
	if out := h.emit("github.pull_request", "early", pullRequest("closed", 8, true)); len(out.Resumed) != 0 {
		t.Fatalf("resumed = %+v before any step was waiting", out.Resumed)
	}
	h.settle()
	v := h.run(early)
	wantStatus(t, v, "succeeded")
	if !strings.Contains(string(step(v, "merged").Output), `"early"`) {
		t.Errorf("merged's output = %s, want the event that came early", step(v, "merged").Output)
	}
	wantStatus(t, h.run(id), "waiting")
}

func TestARunDoesNotTakeTheSameEventTwice(t *testing.T) {
	h := newHarness(t)
	h.publish(`
weir: "0.1"
namespace: test
name: twice
steps:
  - id: first
    kind: external
    resume: event
    event: review.approved
    when: event.data.change == input.change
    timeout: 1h
  - id: second
    kind: external
    resume: event
    event: review.approved
    when: event.data.change == input.change
    timeout: 1h
`)
	id := h.start("twice", "alice", `{"change": "c-1"}`)
	h.settle()
	h.emit("review.approved", "a-1", `{"change": "c-1", "by": "bob"}`)
	h.settle()
	v := h.run(id)
	wantStatus(t, v, "waiting")
	if s := step(v, "second"); s == nil || s.Status != "waiting" {
		t.Fatalf("second = %+v, want it waiting for an approval of its own", s)
	}
	h.emit("review.approved", "a-2", `{"change": "c-1", "by": "carol"}`)
	h.settle()
	v = h.run(id)
	wantStatus(t, v, "succeeded")
	if !strings.Contains(string(step(v, "second").Output), "carol") {
		t.Errorf("second's output = %s, want the second approval", step(v, "second").Output)
	}
}

func TestAnEventInAnotherWorkspaceMovesNothing(t *testing.T) {
	h := newHarness(t)
	h.publish(waitsForMerge)
	id := h.start("merge", "alice", `{"number": 1}`)
	h.settle()

	if err := h.eng.CreateWorkspace(h.ctx, "rival", "Rival"); err != nil {
		t.Fatal(err)
	}
	rival, err := h.eng.In(h.ctx, "rival")
	if err != nil {
		t.Fatal(err)
	}
	out, err := rival.Emit(h.ctx, engine.Event{Type: "github.pull_request", ID: "d-1",
		Data: json.RawMessage(pullRequest("closed", 1, true))}, "webhook:github")
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Resumed) != 0 {
		t.Fatalf("another workspace's event resumed %+v", out.Resumed)
	}
	h.settle()
	wantStatus(t, h.run(id), "waiting")
}

func TestAStepThatWaitsForAnEventTimesOut(t *testing.T) {
	h := newHarness(t)
	h.publish(strings.Replace(waitsForMerge, "timeout: 1h", "timeout: 50ms", 1))
	id := h.start("merge", "alice", `{"number": 1}`)
	h.settle()
	wantStatus(t, h.run(id), "waiting")

	time.Sleep(80 * time.Millisecond)
	swept, err := h.eng.Sweep(h.ctx)
	if err != nil || swept.Timeouts != 1 {
		t.Fatalf("Sweep = %+v, %v; want one timeout", swept, err)
	}
	v := h.run(id)
	wantStatus(t, v, "blocked")
	if errorType(v.Error) != "timeout" {
		t.Errorf("run error = %s, want timeout", v.Error)
	}
	// The event comes too late: nothing is waiting for it any more.
	if out := h.emit("github.pull_request", "late", pullRequest("closed", 1, true)); len(out.Resumed) != 0 {
		t.Errorf("an event resumed a step that had timed out: %+v", out.Resumed)
	}
	wantStatus(t, h.run(id), "blocked")
}

func TestAStepCanCallAThirdPartyAndThenWaitForItsEvent(t *testing.T) {
	h := newHarness(t)
	tp := newThirdParty(t)
	if err := h.eng.PutConnection(h.ctx, "scanner", tp.server.URL, engine.Auth{}); err != nil {
		t.Fatal(err)
	}
	h.publish(`
weir: "0.1"
namespace: test
name: scan
steps:
  - id: scan
    kind: external
    connection: scanner
    call: { path: /scans }
    input: { target: input.target }
    resume: event
    event: scanner.finished
    when: event.data.target == input.target
    timeout: 1h
`)
	id := h.start("scan", "alice", `{"target": "api"}`)
	h.settle()
	wantStatus(t, h.run(id), "waiting")
	if len(tp.bodies) != 1 || !strings.Contains(tp.bodies[0], `"api"`) || tp.tokens[0] != "" {
		t.Fatalf("the third party got %v with token %q; want one call, and no token to return", tp.bodies, tp.tokens)
	}
	h.emit("scanner.finished", "s-1", `{"target": "api", "findings": 0}`)
	h.settle()
	wantStatus(t, h.run(id), "succeeded")
	if len(tp.bodies) != 1 {
		t.Errorf("the third party was called %d times, want once", len(tp.bodies))
	}
}

func TestPublishRefusesAWaitThatCouldTakeAnyEventOrWaitForEver(t *testing.T) {
	h := newHarness(t)
	wait := func(fields string) string {
		return `
weir: "0.1"
namespace: test
name: bad
steps:
  - id: wait
    kind: external
    resume: event
` + fields
	}
	for want, fields := range map[string]string{
		"needs when":         "    event: thing.happened\n    timeout: 1h\n",
		"must set a timeout": "    event: thing.happened\n    when: event.data.id == input.id\n",
		"needs event":        "    when: event.data.id == input.id\n    timeout: 1h\n",
		"must declare the call": "    event: thing.happened\n    when: \"true\"\n    timeout: 1h\n" +
			"    connection: scanner\n",
	} {
		if _, err := h.eng.Publish(h.ctx, []byte(wait(fields)), "test"); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Publish = %v, want an error that says %q", err, want)
		}
	}
	// when and event belong to resume: event only.
	_, err := h.eng.Publish(h.ctx, []byte(`
weir: "0.1"
namespace: test
name: bad
steps:
  - id: call
    kind: external
    connection: scanner
    call: { path: /scans }
    resume: callback
    when: event.data.id == input.id
    timeout: 1h
`), "test")
	if err == nil || !strings.Contains(err.Error(), "only allowed with resume: event") {
		t.Errorf("Publish = %v, want when refused on a callback step", err)
	}
}

// Steps start waiting while their events arrive. Whichever comes first, the
// step or its event, no run may be left waiting.
func TestEventsAndWaitsThatRaceLoseNothing(t *testing.T) {
	h := newHarness(t)
	h.publish(waitsForMerge)
	const n = 30
	ids := make([]string, n)
	for i := range ids {
		ids[i] = h.start("merge", "alice", fmt.Sprintf(`{"number": %d}`, i+1))
	}
	done := make(chan error, 1)
	go func() {
		for i := range ids {
			_, err := h.eng.Emit(h.ctx, engine.Event{Type: "github.pull_request", ID: fmt.Sprintf("race-%d", i+1),
				Data: json.RawMessage(pullRequest("closed", i+1, true))}, "webhook:github")
			if err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	h.settle()
	if err := <-done; err != nil {
		t.Fatalf("emit: %v", err)
	}
	h.settle()
	for i, id := range ids {
		if v := h.run(id); v.Status != "succeeded" {
			t.Errorf("run %d is %s, want succeeded: its event was missed", i+1, v.Status)
		}
	}
}

// The example in examples/wait-for-merge is run as it is written.
func TestTheWaitForMergeExampleRuns(t *testing.T) {
	h := newHarness(t)
	source, err := os.ReadFile("../examples/wait-for-merge/workflow.yaml")
	if err != nil {
		t.Fatal(err)
	}
	h.publish(string(source))
	const url = "https://github.com/acme/shop/pull/42"
	start := func() string {
		t.Helper()
		id, _, err := h.eng.StartRun(h.ctx, engine.StartRequest{Namespace: "examples", Name: "wait-for-merge",
			Requester: "alice", Input: json.RawMessage(`{"pull_request": "` + url + `"}`)})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	closed := func(merged bool) string {
		return fmt.Sprintf(`{"action": "closed", "pull_request": {"html_url": %q, "merged": %t, "merged_by": {"login": "bob"}}}`, url, merged)
	}

	id := start()
	h.settle()
	h.emit("github.pull_request", "x-1", strings.Replace(closed(true), "/pull/42", "/pull/43", 1))
	h.settle()
	wantStatus(t, h.run(id), "waiting")
	h.emit("github.pull_request", "x-2", closed(true))
	h.settle()
	v := h.run(id)
	wantStatus(t, v, "succeeded")
	if !strings.Contains(string(v.Output), `"merged"`) || !strings.Contains(string(v.Output), `"bob"`) {
		t.Errorf("output = %s, want it merged by bob", v.Output)
	}

	id = start()
	h.settle()
	h.emit("github.pull_request", "x-3", closed(false))
	h.settle()
	v = h.run(id)
	wantStatus(t, v, "succeeded")
	if !strings.Contains(string(v.Output), "closed without being merged") {
		t.Errorf("output = %s, want it closed without being merged", v.Output)
	}
}
