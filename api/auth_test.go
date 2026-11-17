package api_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bobbydeveaux/weir/api"
	"github.com/bobbydeveaux/weir/engine"
	"github.com/bobbydeveaux/weir/internal/testdb"
	"github.com/bobbydeveaux/weir/worker"
)

// as makes a request with a key, and optionally a workspace and an actor.
func as(t *testing.T, base, key, method, path, workspace, actor, body string, out any) int {
	t.Helper()
	req, err := http.NewRequest(method, base+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	if workspace != "" {
		req.Header.Set(api.WorkspaceHeader, workspace)
	}
	if actor != "" {
		req.Header.Set("X-Weir-Actor", actor)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if out != nil && resp.StatusCode < 300 {
		if err := json.Unmarshal(raw, out); err != nil {
			t.Fatalf("%s %s: reply %s: %v", method, path, raw, err)
		}
	}
	return resp.StatusCode
}

const signoff = `
weir: "0.1"
namespace: shop
name: signoff
steps:
  - id: approve
    kind: human
    assign: { role: lead }
`

func TestKeysDecideWhoMayDoWhat(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool := testdb.New(ctx, t)
	eng, err := engine.New(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	reg := worker.NewRegistry()
	eng.HandlerExists = reg.Exists
	const admin = "the-admin-key"
	srv := httptest.NewServer(api.New(eng, reg, api.WithAdminKey(admin)))
	defer srv.Close()

	// With authentication on, nothing but the health check answers without a key.
	if code := as(t, srv.URL, "", "GET", "/healthz", "", "", "", nil); code != http.StatusOK {
		t.Errorf("health check without a key = %d", code)
	}
	for _, key := range []string{"", "weir_made_up", "not-even-close"} {
		if code := as(t, srv.URL, key, "GET", "/v1/definitions", "", "", "", nil); code != http.StatusUnauthorized {
			t.Errorf("a request with key %q = %d, want 401", key, code)
		}
	}

	// The admin key sets up two workspaces and a key in each.
	type made struct {
		Secret string
		Key    struct{ ID, Workspace, Role string }
	}
	var app, viewer, rival made
	for _, ws := range []string{"acme", "rival"} {
		if code := as(t, srv.URL, admin, "POST", "/v1/workspaces", "", "", `{"slug": "`+ws+`"}`, nil); code != http.StatusCreated {
			t.Fatalf("create %s = %d", ws, code)
		}
	}
	if code := as(t, srv.URL, admin, "POST", "/v1/keys", "acme", "", `{"name": "the-app", "role": "writer", "act_as": true}`, &app); code != http.StatusCreated {
		t.Fatalf("create key = %d", code)
	}
	as(t, srv.URL, admin, "POST", "/v1/keys", "acme", "", `{"name": "dashboard", "role": "reader"}`, &viewer)
	as(t, srv.URL, admin, "POST", "/v1/keys", "rival", "", `{"name": "rival-app", "role": "admin"}`, &rival)
	if !strings.HasPrefix(app.Secret, "weir_") || app.Key.Workspace != "acme" {
		t.Fatalf("made %+v", app)
	}
	if code := as(t, srv.URL, admin, "POST", "/v1/keys", "acme", "", `{"name": "x", "role": "emperor"}`, nil); code == http.StatusCreated {
		t.Error("a key with a made-up role was created")
	}

	// A workspace's key needs no workspace header, and cannot name another workspace.
	if code := as(t, srv.URL, app.Secret, "POST", "/v1/definitions", "", "", signoff, nil); code != http.StatusCreated {
		t.Fatalf("publish with the app's key = %d", code)
	}
	if code := as(t, srv.URL, app.Secret, "GET", "/v1/definitions", "rival", "", "", nil); code != http.StatusForbidden {
		t.Errorf("the app's key naming another workspace = %d, want 403", code)
	}
	var defs []map[string]any
	as(t, srv.URL, rival.Secret, "GET", "/v1/definitions", "", "", "", &defs)
	if len(defs) != 0 {
		t.Errorf("the other workspace's key sees %v", defs)
	}
	var seen []map[string]any
	as(t, srv.URL, app.Secret, "GET", "/v1/workspaces", "", "", "", &seen)
	if len(seen) != 1 || seen[0]["slug"] != "acme" {
		t.Errorf("a workspace's key is shown workspaces %v, want only its own", seen)
	}

	// Roles: a reader reads; only the admin key makes workspaces; only an admin manages keys.
	if code := as(t, srv.URL, viewer.Secret, "GET", "/v1/runs", "", "", "", nil); code != http.StatusOK {
		t.Errorf("a reader reading = %d", code)
	}
	if code := as(t, srv.URL, viewer.Secret, "POST", "/v1/runs", "", "", `{"namespace": "shop", "name": "signoff"}`, nil); code != http.StatusForbidden {
		t.Errorf("a reader starting a run = %d, want 403", code)
	}
	if code := as(t, srv.URL, app.Secret, "POST", "/v1/workspaces", "", "", `{"slug": "more"}`, nil); code != http.StatusForbidden {
		t.Errorf("a workspace's key creating a workspace = %d, want 403", code)
	}
	if code := as(t, srv.URL, app.Secret, "GET", "/v1/keys", "", "", "", nil); code != http.StatusForbidden {
		t.Errorf("a writer listing keys = %d, want 403", code)
	}
	var keys []map[string]any
	as(t, srv.URL, rival.Secret, "GET", "/v1/keys", "", "", "", &keys)
	if len(keys) != 1 || keys[0]["name"] != "rival-app" || keys[0]["secret"] != nil {
		t.Errorf("an admin of one workspace sees keys %v, want its own one and no secret", keys)
	}

	// Who did it: a key that may speak for its users names them; any other key is itself.
	var started struct{ ID string }
	as(t, srv.URL, app.Secret, "POST", "/v1/runs", "", "alice@example.com", `{"namespace": "shop", "name": "signoff"}`, &started)
	var runs []map[string]any
	as(t, srv.URL, viewer.Secret, "GET", "/v1/runs", "", "", "", &runs)
	if len(runs) != 1 || runs[0]["requester"] != "alice@example.com" {
		t.Fatalf("runs = %v, want one requested by the person the app named", runs)
	}
	var plain made
	as(t, srv.URL, admin, "POST", "/v1/keys", "acme", "", `{"name": "ci", "role": "writer"}`, &plain)
	as(t, srv.URL, plain.Secret, "POST", "/v1/runs", "", "ceo@example.com", `{"namespace": "shop", "name": "signoff"}`, nil)
	as(t, srv.URL, viewer.Secret, "GET", "/v1/runs?requester=ci", "", "", "", &runs)
	if len(runs) != 1 {
		t.Errorf("a key that may not speak for others was recorded as someone else: %v", runs)
	}

	// A revoked key stops working at once.
	if code := as(t, srv.URL, admin, "DELETE", "/v1/keys/"+app.Key.ID, "acme", "", "", nil); code != http.StatusOK {
		t.Fatalf("revoke = %d", code)
	}
	if code := as(t, srv.URL, app.Secret, "GET", "/v1/runs", "", "", "", nil); code != http.StatusUnauthorized {
		t.Errorf("a revoked key = %d, want 401", code)
	}

	// The stored hash is not the key.
	var stored int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM api_keys WHERE encode(key_hash, 'escape') LIKE 'weir_%'`).Scan(&stored); err != nil || stored != 0 {
		t.Errorf("keys are stored in the clear (%d, %v)", stored, err)
	}
}
