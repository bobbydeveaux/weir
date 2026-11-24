package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bobbydeveaux/weir/api"
	"github.com/bobbydeveaux/weir/engine"
	"github.com/bobbydeveaux/weir/internal/secrets"
	"github.com/bobbydeveaux/weir/internal/testdb"
	"github.com/bobbydeveaux/weir/runner"
	"github.com/bobbydeveaux/weir/worker"
)

// A run waits for its pull request to be merged, and GitHub's own signed
// webhook is what moves it on.
func TestASignedWebhookMovesARunThatIsWaiting(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool := testdb.New(ctx, t)
	eng, err := engine.New(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	const secret = "acme-webhook-secret"
	reg := worker.NewRegistry()
	reg.Secrets = &secrets.Resolver{Getenv: func(k string) string {
		return map[string]string{"WEIR_SECRET__ACME__GH_WEBHOOK": secret, "WEIR_SECRET__RIVAL__GH_WEBHOOK": secret}[k]
	}}
	eng.HandlerExists = reg.Exists
	srv := httptest.NewServer(api.New(eng, reg))
	defer srv.Close()

	const waits = `
weir: "0.1"
namespace: code
name: ship
steps:
  - id: merged
    kind: external
    resume: event
    event: github.pull_request
    when: event.data.action == "closed" && event.data.pull_request.merged && event.data.number == input.number
    timeout: 1h
  - id: note
    kind: deterministic
    handler: core.echo
    input: { merged_by: steps.merged.output.data.pull_request.merged_by }
`
	gh := `{"base_url": "https://api.github.com", "webhook": {"kind": "github", "secret": "env:GH_WEBHOOK"}}`
	for _, ws := range []string{"acme", "rival"} {
		call(t, srv.URL, "POST", "/v1/workspaces", "", `{"slug": "`+ws+`"}`, nil)
		if code := call(t, srv.URL, "PUT", "/v1/connections/github", ws, gh, nil); code != http.StatusOK {
			t.Fatalf("%s: connect = %d", ws, code)
		}
		if code := call(t, srv.URL, "POST", "/v1/definitions", ws, waits, nil); code != http.StatusCreated {
			t.Fatalf("%s: publish = %d", ws, code)
		}
	}
	var acme, rival struct{ ID string }
	call(t, srv.URL, "POST", "/v1/runs", "acme", `{"namespace": "code", "name": "ship", "input": {"number": 42}}`, &acme)
	call(t, srv.URL, "POST", "/v1/runs", "rival", `{"namespace": "code", "name": "ship", "input": {"number": 42}}`, &rival)

	w := &worker.Worker{Engine: eng, Runner: &runner.Fake{}, Handlers: reg}
	settle := func() {
		t.Helper()
		for idle := 0; idle < 6; {
			if found, err := w.RunOnce(ctx); err != nil {
				t.Fatalf("worker: %v", err)
			} else if found {
				idle = 0
			} else {
				idle++
			}
		}
	}
	status := func(ws, id string) string {
		t.Helper()
		var run struct{ Status string }
		call(t, srv.URL, "GET", "/v1/runs/"+id, ws, "", &run)
		return run.Status
	}
	settle()
	if got := status("acme", acme.ID); got != "waiting" {
		t.Fatalf("acme's run is %s, want waiting", got)
	}

	send := func(ws, delivery, body string) map[string]any {
		t.Helper()
		code, out := deliver(t, srv.URL+"/v1/webhooks/"+ws+"/github", body, map[string]string{
			"X-Hub-Signature-256": "sha256=" + sign(secret, body), "X-GitHub-Event": "pull_request", "X-GitHub-Delivery": delivery})
		if code != http.StatusAccepted {
			t.Fatalf("webhook %s = %d %v", delivery, code, out)
		}
		return out
	}
	// Closed without being merged is not what this step waits for.
	if out := send("acme", "d-1", `{"action": "closed", "number": 42, "pull_request": {"merged": false}}`); out["resumed"] != 0.0 {
		t.Fatalf("a pull request closed unmerged = %v, want nothing resumed", out)
	}
	if out := send("acme", "d-2", `{"action": "closed", "number": 42, "pull_request": {"merged": true, "merged_by": "bob"}}`); out["resumed"] != 1.0 || out["started"] != 0.0 {
		t.Fatalf("the merge = %v, want one step resumed and nothing started", out)
	}
	settle()
	if got := status("acme", acme.ID); got != "succeeded" {
		t.Errorf("acme's run is %s, want succeeded", got)
	}
	// The same pull request number in another workspace is another pull request.
	if got := status("rival", rival.ID); got != "waiting" {
		t.Errorf("rival's run is %s after acme's webhook, want it still waiting", got)
	}

	// The list of events says what each one moved.
	var events []struct {
		Key     string
		Resumed []struct {
			RunID string `json:"run_id"`
			Step  string
		}
	}
	call(t, srv.URL, "GET", "/v1/events", "acme", "", &events)
	found := false
	for _, e := range events {
		if e.Key == "d-2" {
			found = len(e.Resumed) == 1 && e.Resumed[0].RunID == acme.ID && e.Resumed[0].Step == "merged"
		} else if len(e.Resumed) != 0 {
			t.Errorf("event %s is listed as resuming %v", e.Key, e.Resumed)
		}
	}
	if !found {
		t.Errorf("events = %+v, want d-2 listed as resuming acme's merged step", events)
	}
	var theirs []struct{ Key string }
	call(t, srv.URL, "GET", "/v1/events", "rival", "", &theirs)
	if len(theirs) != 0 {
		t.Errorf("rival can see %d of acme's events", len(theirs))
	}
}
