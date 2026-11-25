package engine_test

import (
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/bobbydeveaux/weir/engine"
	"github.com/bobbydeveaux/weir/runner"
)

// These tests run the workflows in examples/ exactly as they are written, with
// a scripted model and stand-in third parties, so they need no key and cost
// nothing. An example that stops publishing or stops running fails here.

// example publishes the workflow in examples/<name> and the templates it uses.
func (h *harness) example(name string, templates ...string) {
	h.t.Helper()
	source, err := os.ReadFile("../examples/" + name + "/workflow.yaml")
	if err != nil {
		h.t.Fatal(err)
	}
	for _, key := range templates {
		h.template(key)
	}
	h.publish(string(source))
}

// started returns the one run an event started.
func started(t *testing.T, out *engine.EventResult) string {
	t.Helper()
	if len(out.Runs) != 1 {
		t.Fatalf("the event started %d runs, want 1", len(out.Runs))
	}
	return out.Runs[0].RunID
}

// decide answers the one open task.
func (h *harness) decide(who, outcome string) error {
	h.t.Helper()
	tasks, err := h.eng.Tasks(h.ctx)
	if err != nil || len(tasks) != 1 {
		h.t.Fatalf("Tasks = %v, %v; want one task", tasks, err)
	}
	return h.eng.Decide(h.ctx, tasks[0].ID, who, outcome, "")
}

func ran(v *engine.RunView) []string {
	var keys []string
	for _, s := range v.Steps {
		keys = append(keys, s.Key)
	}
	return keys
}

func wantSteps(t *testing.T, v *engine.RunView, want ...string) {
	t.Helper()
	if got := ran(v); !slices.Equal(got, want) {
		t.Errorf("steps = %v, want %v", got, want)
	}
}

const kettle = `{"from": "sam@example.com", "message": "My kettle arrived cracked. Can I have my money back please?"}`

func (h *harness) supportInbox(label string) string {
	h.t.Helper()
	h.example("support-inbox", "support-classify", "support-reply")
	h.fake.Decisions["classify"] = runner.DecideResult{Label: label, Confidence: 0.95}
	h.fake.Outputs["draft"] = []json.RawMessage{json.RawMessage(`{"reply": "Sorry about the kettle. A colleague will confirm your refund."}`)}
	h.fake.CostUSD = 0.01
	id, _, err := h.eng.StartRun(h.ctx, engine.StartRequest{Namespace: "examples", Name: "support-inbox", Requester: "alice", Input: json.RawMessage(kettle)})
	if err != nil {
		h.t.Fatal(err)
	}
	h.settle()
	return id
}

func TestTheSupportInboxExampleSendsAnApprovedReply(t *testing.T) {
	h := newHarness(t)
	id := h.supportInbox("refund")
	wantStatus(t, h.run(id), "waiting")

	// alice started the run, so she cannot approve the reply.
	if err := h.decide("alice", "approved"); !errors.Is(err, engine.ErrNotAllowed) {
		t.Fatalf("the requester approving = %v, want ErrNotAllowed", err)
	}
	if err := h.decide("bob", "approved"); err != nil {
		t.Fatal(err)
	}
	h.settle()
	v := h.run(id)
	wantStatus(t, v, "succeeded")
	wantSteps(t, v, "classify", "draft", "approve", "send")
	if out := string(v.Output); !strings.Contains(out, "sam@example.com") || !strings.Contains(out, "A colleague will confirm") {
		t.Errorf("output = %s, want the drafted reply addressed to the sender", out)
	}
}

func TestTheSupportInboxExampleFilesNoiseWithoutAnAgent(t *testing.T) {
	h := newHarness(t)
	v := h.run(h.supportInbox("other"))
	wantStatus(t, v, "succeeded")
	wantSteps(t, v, "classify", "file")
	if slices.Contains(h.fake.Calls, "draft") {
		t.Errorf("model calls = %v, want no agent for a message labelled other", h.fake.Calls)
	}
	if v.SpentUSD != 0 {
		t.Errorf("spent = %v, want nothing", v.SpentUSD)
	}
}

func TestTheSupportInboxExampleFilesARejectedReply(t *testing.T) {
	h := newHarness(t)
	id := h.supportInbox("refund")
	if err := h.decide("bob", "rejected"); err != nil {
		t.Fatal(err)
	}
	h.settle()
	v := h.run(id)
	wantStatus(t, v, "succeeded")
	wantSteps(t, v, "classify", "draft", "approve", "file")
}

func (h *harness) bugAt2am(label string, confidence float64) (*thirdParty, func(severity string) *engine.EventResult) {
	h.t.Helper()
	tp := newThirdParty(h.t)
	if err := h.eng.PutConnection(h.ctx, "errors", tp.server.URL, engine.Auth{}); err != nil {
		h.t.Fatal(err)
	}
	h.example("bug-at-2am", "incident-route", "incident-fix")
	h.fake.Decisions["route"] = runner.DecideResult{Label: label, Confidence: confidence}
	h.fake.Outputs["fix"] = []json.RawMessage{json.RawMessage(`{"branch": "fix/checkout-nil-basket", "summary": "Check the basket before reading it"}`)}
	h.fake.CostUSD = 3.20
	n := 0
	return tp, func(severity string) *engine.EventResult {
		n++
		return h.emit("errors.new", "err-"+string(rune('0'+n)),
			`{"id": "E-7", "severity": "`+severity+`", "service": "checkout", "message": "nil basket in Checkout.total"}`)
	}
}

func TestTheBugAt2amExampleFixesDeploysAndConfirms(t *testing.T) {
	h := newHarness(t)
	tp, report := h.bugAt2am("fix", 0.95)

	// The rule drops a warning before any model is called.
	if out := report("warning"); len(out.Runs) != 0 {
		t.Fatalf("a warning started %d runs, want none", len(out.Runs))
	}
	if len(h.fake.Calls) != 0 {
		t.Fatalf("model calls after a warning = %v, want none", h.fake.Calls)
	}

	id := started(t, report("error"))
	h.settle()
	v := h.run(id)
	wantStatus(t, v, "waiting")
	wantSteps(t, v, "route", "open_issue", "fix", "approve")
	if len(tp.bodies) != 0 {
		t.Fatalf("the error tracker was called before anyone approved the fix")
	}

	if err := h.decide("oncall", "approved"); err != nil {
		t.Fatal(err)
	}
	h.settle()
	v = h.run(id)
	wantStatus(t, v, "waiting") // for the error tracker
	if s := step(v, "deploy"); s == nil || !strings.Contains(string(s.Output), "fix/checkout-nil-basket") {
		t.Fatalf("deploy = %+v, want it given the branch the agent wrote", s)
	}
	if len(tp.bodies) != 1 || !strings.Contains(tp.bodies[0], "E-7") {
		t.Fatalf("the error tracker was sent %v, want one call about error E-7", tp.bodies)
	}

	if err := h.eng.Complete(h.ctx, tp.tokens[0], engine.Result{Output: json.RawMessage(`{"stopped": true}`), Actor: "connection:errors"}); err != nil {
		t.Fatalf("callback: %v", err)
	}
	v = h.run(id)
	wantStatus(t, v, "succeeded")
	wantSteps(t, v, "route", "open_issue", "fix", "approve", "deploy", "verify")
	if v.SpentUSD != 3.20 {
		t.Errorf("spent = %v, want 3.20", v.SpentUSD)
	}
}

func TestTheBugAt2amExampleGivesAnUnsureCallToAPerson(t *testing.T) {
	h := newHarness(t)
	_, report := h.bugAt2am("fix", 0.4) // below the step's 0.8
	id := started(t, report("error"))
	h.settle()
	v := h.run(id)
	wantStatus(t, v, "waiting")
	wantSteps(t, v, "route", "page")
	if slices.Contains(h.fake.Calls, "fix") {
		t.Errorf("model calls = %v, want no agent when the classifier is unsure", h.fake.Calls)
	}
}

func TestTheBugAt2amExamplePagesWhenTheFixIsRejected(t *testing.T) {
	h := newHarness(t)
	tp, report := h.bugAt2am("fix", 0.95)
	id := started(t, report("error"))
	h.settle()
	if err := h.decide("oncall", "rejected"); err != nil {
		t.Fatal(err)
	}
	h.settle()
	v := h.run(id)
	wantStatus(t, v, "waiting")
	wantSteps(t, v, "route", "open_issue", "fix", "approve", "page")
	if len(tp.bodies) != 0 {
		t.Errorf("a rejected fix reached the error tracker: %v", tp.bodies)
	}
}

func (h *harness) vulnerableLibrary() (*thirdParty, string) {
	h.t.Helper()
	tp := newThirdParty(h.t)
	tp.reply = func(n int) string {
		if n < 2 {
			return `{"status": "open"}`
		}
		return `{"status": "resolved"}`
	}
	if err := h.eng.PutConnection(h.ctx, "scanner", tp.server.URL, engine.Auth{}); err != nil {
		h.t.Fatal(err)
	}
	h.example("vulnerable-library", "finding-assess", "finding-fix")
	h.fake.Outputs["assess"] = []json.RawMessage{json.RawMessage(`{"called": true, "plan": "Move to 2.4.1; add a test for the parser first"}`)}
	h.fake.Outputs["fix"] = []json.RawMessage{json.RawMessage(`{"branch": "fix/yamlparse-2.4.1", "version": "2.4.1"}`)}
	h.fake.CostUSD = 1.50

	finding := func(id, severity string) *engine.EventResult {
		return h.emit("scanner.finding", id, `{"id": "CVE-2026-0001", "severity": "`+severity+`", "service": "payments",
			"library": "yamlparse", "fixed_in": "2.4.1", "internet_facing": true}`)
	}
	if out := finding("f-1", "low"); len(out.Runs) != 0 {
		h.t.Fatalf("a low finding started %d runs, want none", len(out.Runs))
	}
	id := started(h.t, finding("f-2", "critical"))
	h.settle()
	return tp, id
}

func TestTheVulnerableLibraryExampleEndsWhenTheScannerSaysSo(t *testing.T) {
	h := newHarness(t)
	tp, id := h.vulnerableLibrary()
	v := h.run(id)
	wantStatus(t, v, "waiting")
	wantSteps(t, v, "rank", "assess", "approve")

	if err := h.decide("carol", "approved"); err != nil {
		t.Fatal(err)
	}
	h.settle()
	v = h.run(id)
	wantStatus(t, v, "running") // the scanner still reports the finding
	if s := step(v, "open_pull_request"); s == nil || !strings.Contains(string(s.Output), "fix/yamlparse-2.4.1") || !strings.Contains(string(s.Output), "Move to 2.4.1") {
		t.Fatalf("open_pull_request = %+v, want the branch and the assessment as evidence", s)
	}
	if len(tp.bodies) != 1 {
		t.Fatalf("the scanner was asked %d times, want once so far", len(tp.bodies))
	}

	// Fifteen minutes pass.
	if _, err := h.pool.Exec(h.ctx, `UPDATE steps SET not_before = now() WHERE status = 'runnable'`); err != nil {
		t.Fatal(err)
	}
	h.settle()
	v = h.run(id)
	wantStatus(t, v, "succeeded")
	wantSteps(t, v, "rank", "assess", "approve", "fix", "open_pull_request", "verify")
	if !strings.Contains(string(v.Output), "resolved") {
		t.Errorf("output = %s, want the scanner's last reply", v.Output)
	}
	if v.SpentUSD != 3.00 {
		t.Errorf("spent = %v, want 3.00 for the two agents", v.SpentUSD)
	}
}

func TestTheVulnerableLibraryExampleStopsWhenThePlanIsRejected(t *testing.T) {
	h := newHarness(t)
	tp, id := h.vulnerableLibrary()
	if err := h.decide("carol", "rejected"); err != nil {
		t.Fatal(err)
	}
	h.settle()
	v := h.run(id)
	wantStatus(t, v, "succeeded")
	wantSteps(t, v, "rank", "assess", "approve", "declined")
	if slices.Contains(h.fake.Calls, "fix") || len(tp.bodies) != 0 {
		t.Errorf("a rejected plan still ran the fix or asked the scanner (calls %v)", h.fake.Calls)
	}
}
