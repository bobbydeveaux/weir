package api_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bobbydeveaux/weir/api"
	"github.com/bobbydeveaux/weir/engine"
	"github.com/bobbydeveaux/weir/internal/secrets"
	"github.com/bobbydeveaux/weir/internal/testdb"
	"github.com/bobbydeveaux/weir/worker"
)

// sign returns the hex HMAC-SHA256 of body under secret.
func sign(secret, body string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	return hex.EncodeToString(mac.Sum(nil))
}

// deliver posts a webhook with the given headers and returns the status and reply.
func deliver(t *testing.T, url, body string, headers map[string]string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest("POST", url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return resp.StatusCode, out
}

func TestASignedWebhookBecomesAnEvent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool := testdb.New(ctx, t)
	eng, err := engine.New(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	const acmeSecret, rivalSecret = "acme-webhook-secret", "rival-webhook-secret"
	reg := worker.NewRegistry()
	reg.Secrets = &secrets.Resolver{Getenv: func(k string) string {
		return map[string]string{
			"WEIR_SECRET__ACME__GH_WEBHOOK": acmeSecret, "WEIR_SECRET__RIVAL__GH_WEBHOOK": rivalSecret,
			"WEIR_SECRET__ACME__BILLING": "billing-secret",
		}[k]
	}}
	eng.HandlerExists = reg.Exists
	// API keys are on: a webhook must still get through without one.
	const admin = "the-admin-key"
	srv := httptest.NewServer(api.New(eng, reg, api.WithAdminKey(admin)))
	defer srv.Close()

	gh := `{"base_url": "https://api.github.com", "webhook": {"kind": "github", "secret": "env:GH_WEBHOOK"}}`
	var made map[string]any
	for _, ws := range []string{"acme", "rival"} {
		as(t, srv.URL, admin, "POST", "/v1/workspaces", "", "", `{"slug": "`+ws+`"}`, nil)
		if code := as(t, srv.URL, admin, "PUT", "/v1/connections/github", ws, "", gh, &made); code != http.StatusOK {
			t.Fatalf("%s: connect = %d", ws, code)
		}
	}
	hook, _ := made["webhook"].(map[string]any)
	if hook["path"] != "/v1/webhooks/rival/github" || hook["secret_found"] != true {
		t.Errorf("the reply says the webhook is %v", hook)
	}
	// A workflow that starts when a pull request is opened, and only then.
	const onPR = `
weir: "0.1"
namespace: code
name: review
triggers:
  - event: github.pull_request
    when: event.data.action == "opened"
    input: { number: event.data.number }
steps:
  - id: note
    kind: deterministic
    handler: core.echo
`
	if code := as(t, srv.URL, admin, "POST", "/v1/definitions", "acme", "", onPR, nil); code != http.StatusCreated {
		t.Fatalf("publish = %d", code)
	}

	url := srv.URL + "/v1/webhooks/acme/github"
	body := `{"action": "opened", "number": 42}`
	good := map[string]string{"X-Hub-Signature-256": "sha256=" + sign(acmeSecret, body), "X-GitHub-Event": "pull_request", "X-GitHub-Delivery": "d-1"}

	code, out := deliver(t, url, body, good)
	if code != http.StatusAccepted || out["started"] != 1.0 || out["duplicate"] != false {
		t.Fatalf("a correctly signed webhook = %d %v", code, out)
	}
	var runs []map[string]any
	as(t, srv.URL, admin, "GET", "/v1/runs", "acme", "", "", &runs)
	if len(runs) != 1 || runs[0]["requester"] != "webhook:github" {
		t.Fatalf("runs = %v, want one started by the webhook", runs)
	}
	var run struct{ Input map[string]any }
	as(t, srv.URL, admin, "GET", "/v1/runs/"+runs[0]["id"].(string), "acme", "", "", &run)
	if run.Input["number"] != 42.0 {
		t.Errorf("the run's input = %v", run.Input)
	}

	// The same delivery again starts nothing new.
	if code, out := deliver(t, url, body, good); code != http.StatusAccepted || out["duplicate"] != true {
		t.Errorf("a repeated delivery = %d %v", code, out)
	}

	// Everything that should be refused, and must record nothing.
	with := func(change func(map[string]string)) map[string]string {
		h := map[string]string{}
		for k, v := range good {
			h[k] = v
		}
		h["X-GitHub-Delivery"] = "d-refused"
		change(h)
		return h
	}
	for name, tc := range map[string]struct {
		url, body string
		headers   map[string]string
		want      int
	}{
		"no signature":               {url, body, with(func(h map[string]string) { delete(h, "X-Hub-Signature-256") }), 401},
		"a signature of other bytes": {url, `{"action": "opened", "number": 43}`, with(func(map[string]string) {}), 401},
		"signed with another secret": {url, body, with(func(h map[string]string) { h["X-Hub-Signature-256"] = "sha256=" + sign("guess", body) }), 401},
		"signed by the other workspace": {url, body, with(func(h map[string]string) {
			h["X-Hub-Signature-256"] = "sha256=" + sign(rivalSecret, body)
		}), 401},
		"not hex":               {url, body, with(func(h map[string]string) { h["X-Hub-Signature-256"] = "sha256=zzzz" }), 401},
		"the wrong prefix":      {url, body, with(func(h map[string]string) { h["X-Hub-Signature-256"] = "sha1=" + sign(acmeSecret, body) }), 401},
		"no event name":         {url, body, with(func(h map[string]string) { delete(h, "X-GitHub-Event") }), 400},
		"an unknown workspace":  {srv.URL + "/v1/webhooks/nobody/github", body, good, 404},
		"an unknown connection": {srv.URL + "/v1/webhooks/acme/gitlab", body, good, 404},
	} {
		if code, _ := deliver(t, tc.url, tc.body, tc.headers); code != tc.want {
			t.Errorf("%s = %d, want %d", name, code, tc.want)
		}
	}
	var events []map[string]any
	as(t, srv.URL, admin, "GET", "/v1/events", "acme", "", "", &events)
	as(t, srv.URL, admin, "GET", "/v1/runs", "acme", "", "", &runs)
	if len(events) != 1 || len(runs) != 1 || events[0]["type"] != "github.pull_request" || events[0]["key"] != "d-1" {
		t.Fatalf("after the refused requests there are %d events and %d runs: %v", len(events), len(runs), events)
	}

	// A signed event that no trigger wants is received and starts nothing.
	closed := `{"action": "closed", "number": 42}`
	code, out = deliver(t, url, closed, map[string]string{"X-Hub-Signature-256": "sha256=" + sign(acmeSecret, closed),
		"X-GitHub-Event": "pull_request", "X-GitHub-Delivery": "d-2"})
	if code != http.StatusAccepted || out["started"] != 0.0 {
		t.Errorf("a closed pull request = %d %v, want it received and nothing started", code, out)
	}

	// rival's own webhook works with rival's secret, and its events are rival's alone.
	if code, _ := deliver(t, srv.URL+"/v1/webhooks/rival/github", body, map[string]string{
		"X-Hub-Signature-256": "sha256=" + sign(rivalSecret, body), "X-GitHub-Event": "pull_request", "X-GitHub-Delivery": "d-1"}); code != http.StatusAccepted {
		t.Errorf("rival's own webhook = %d", code)
	}
	as(t, srv.URL, admin, "GET", "/v1/runs", "acme", "", "", &runs)
	if len(runs) != 1 {
		t.Errorf("rival's webhook started a run in acme: %v", runs)
	}

	// A sender with its own header names: the general kind.
	billing := `{"base_url": "https://billing.example", "webhook": {"kind": "hmac", "secret": "env:BILLING",
		"header": "X-Signature", "event_header": "X-Event", "id_header": "X-Delivery"}}`
	if code := as(t, srv.URL, admin, "PUT", "/v1/connections/billing", "acme", "", billing, nil); code != http.StatusOK {
		t.Fatalf("connect billing = %d", code)
	}
	paid := `{"invoice": "INV-7"}`
	code, _ = deliver(t, srv.URL+"/v1/webhooks/acme/billing", paid, map[string]string{"X-Signature": sign("billing-secret", paid), "X-Event": "invoice.paid", "X-Delivery": "b-1"})
	as(t, srv.URL, admin, "GET", "/v1/events?limit=1", "acme", "", "", &events)
	if code != http.StatusAccepted || events[0]["type"] != "billing.invoice.paid" {
		t.Errorf("the general kind = %d, newest event %v", code, events[0])
	}

	// Settings that make no sense are refused, a missing secret is not a way in, and receiving can be switched off.
	for _, bad := range []string{
		`{"base_url": "http://x", "webhook": {"kind": "github"}}`,
		`{"base_url": "http://x", "webhook": {"kind": "github", "secret": "env:GH_WEBHOOK", "header": "X-Mine"}}`,
		`{"base_url": "http://x", "webhook": {"kind": "hmac", "secret": "env:BILLING"}}`,
		`{"base_url": "http://x", "webhook": {"kind": "hmac", "secret": "env:BILLING", "header": "X-S"}}`,
		`{"base_url": "http://x", "webhook": {"kind": "telepathy", "secret": "env:BILLING"}}`,
	} {
		if code := as(t, srv.URL, admin, "PUT", "/v1/connections/bad", "acme", "", bad, nil); code != http.StatusBadRequest {
			t.Errorf("%s = %d, want 400", bad, code)
		}
	}
	as(t, srv.URL, admin, "PUT", "/v1/connections/unset", "acme", "", `{"base_url": "http://x", "webhook": {"kind": "github", "secret": "env:NOT_SET"}}`, nil)
	if code, _ := deliver(t, srv.URL+"/v1/webhooks/acme/unset", body, map[string]string{
		"X-Hub-Signature-256": "sha256=" + sign("", body), "X-GitHub-Event": "pull_request"}); code != http.StatusServiceUnavailable {
		t.Errorf("a webhook whose secret is not set = %d, want 503", code)
	}
	as(t, srv.URL, admin, "PUT", "/v1/connections/github", "acme", "", `{"base_url": "https://api.github.com", "webhook": {"kind": "none"}}`, nil)
	if code, _ := deliver(t, url, body, good); code != http.StatusNotFound {
		t.Errorf("a webhook that was switched off = %d, want 404", code)
	}
	for _, s := range []string{acmeSecret, rivalSecret} {
		var n int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM connections t WHERE to_jsonb(t)::text LIKE '%' || $1 || '%'`, s).Scan(&n); err != nil || n != 0 {
			t.Errorf("a webhook secret is stored (%d, %v)", n, err)
		}
	}
}
