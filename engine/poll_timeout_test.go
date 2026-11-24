package engine_test

import (
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bobbydeveaux/weir/engine"
)

// A poll step never parks, so no timer watches it. Its timeout has to be
// enforced when a poll comes back, or a third party that never finishes is
// asked for ever.
func TestAPollStepStopsAtItsTimeout(t *testing.T) {
	h := newHarness(t)
	tp := newThirdParty(t)
	tp.reply = func(int) string { return `{"status": "pending"}` }
	if err := h.eng.PutConnection(h.ctx, "scanner", tp.server.URL, engine.Auth{}); err != nil {
		t.Fatal(err)
	}
	h.publish(`
weir: "0.1"
namespace: test
name: poll-for-ever
steps:
  - id: scan
    kind: external
    connection: scanner
    call: { method: GET, path: /status }
    resume: poll
    timeout: 60ms
    poll: { every: 5ms, until: response.status == "done" }
`)
	id := h.start("poll-for-ever", "alice", `{}`)

	// Without the timeout, the worker always finds another poll to make and
	// settle gives up only after ten seconds, with the run still running.
	h.settle()
	v := h.run(id)
	wantStatus(t, v, "blocked")
	if errorType(v.Error) != "timeout" {
		t.Errorf("run error = %s, want timeout", v.Error)
	}
	s := step(v, "scan")
	if s == nil || s.Status != "failed" || errorType(s.Error) != "timeout" {
		t.Errorf("scan = %+v, want it failed with the error type timeout", s)
	}
	if !slices.Contains(eventTypes(v), "attempt.timed_out") {
		t.Errorf("events = %v, want the last attempt closed as timed out", eventTypes(v))
	}

	calls := func() int {
		tp.mu.Lock()
		defer tp.mu.Unlock()
		return len(tp.bodies)
	}
	made := calls()
	if made < 2 {
		t.Errorf("the third party was called %d times, want it polled more than once before the timeout", made)
	}
	time.Sleep(50 * time.Millisecond)
	h.settle()
	if again := calls(); again != made {
		t.Errorf("the third party was called %d more times after the step timed out", again-made)
	}
}

// A timeout that has not passed changes nothing about a poll.
func TestAPollStepThatFinishesInTimeIsNotStopped(t *testing.T) {
	h := newHarness(t)
	tp := newThirdParty(t)
	tp.reply = func(n int) string {
		if n < 3 {
			return `{"status": "pending"}`
		}
		return `{"status": "done"}`
	}
	if err := h.eng.PutConnection(h.ctx, "scanner", tp.server.URL, engine.Auth{}); err != nil {
		t.Fatal(err)
	}
	h.publish(`
weir: "0.1"
namespace: test
name: poll-in-time
steps:
  - id: scan
    kind: external
    connection: scanner
    call: { method: GET, path: /status }
    resume: poll
    timeout: 1h
    poll: { every: 5ms, until: response.status == "done" }
`)
	id := h.start("poll-in-time", "alice", `{}`)
	h.settle()
	v := h.run(id)
	wantStatus(t, v, "succeeded")
	if len(tp.bodies) != 3 {
		t.Errorf("the third party was called %d times, want 3", len(tp.bodies))
	}
	if s := step(v, "scan"); s.Attempts != 1 {
		t.Errorf("scan used %d attempts, want 1", s.Attempts)
	}
}

// A step that timed out and is resumed gets its whole timeout again, as a
// step that waits for a callback does.
func TestAResumedPollStepGetsItsTimeoutAgain(t *testing.T) {
	h := newHarness(t)
	tp := newThirdParty(t)
	var done atomic.Bool
	tp.reply = func(int) string {
		if done.Load() {
			return `{"status": "done"}`
		}
		return `{"status": "pending"}`
	}
	if err := h.eng.PutConnection(h.ctx, "scanner", tp.server.URL, engine.Auth{}); err != nil {
		t.Fatal(err)
	}
	h.publish(`
weir: "0.1"
namespace: test
name: poll-again
steps:
  - id: scan
    kind: external
    connection: scanner
    call: { method: GET, path: /status }
    resume: poll
    timeout: 80ms
    poll: { every: 5ms, until: response.status == "done" }
`)
	id := h.start("poll-again", "alice", `{}`)
	h.settle()
	wantStatus(t, h.run(id), "blocked")

	tp.mu.Lock()
	before := len(tp.bodies)
	tp.mu.Unlock()
	if err := h.eng.Resume(h.ctx, id, "bob", 0); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	// Three polls into the new timeout the third party finishes. Had the old
	// clock still been running, the first poll after resuming would have
	// timed the step out again.
	for i := 0; i < 3; i++ {
		if _, err := h.worker.RunOnce(h.ctx); err != nil {
			t.Fatal(err)
		}
		time.Sleep(6 * time.Millisecond)
	}
	done.Store(true)
	tp.mu.Lock()
	after := len(tp.bodies)
	tp.mu.Unlock()
	if after-before < 2 {
		t.Fatalf("the third party was polled %d times after the resume, want more than one", after-before)
	}
	h.settle()
	wantStatus(t, h.run(id), "succeeded")
}
