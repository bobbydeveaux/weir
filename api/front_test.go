package api_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bobbydeveaux/weir/api"
	"github.com/bobbydeveaux/weir/engine"
	"github.com/bobbydeveaux/weir/internal/testdb"
	"github.com/bobbydeveaux/weir/worker"
)

// ask makes a request with the given headers and returns the status and body.
func ask(t *testing.T, method, url, body string, headers map[string]string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(raw)
}

func TestBehindASignInProxy(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool := testdb.New(ctx, t)
	eng, err := engine.New(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	reg := worker.NewRegistry()
	eng.HandlerExists = reg.Exists

	dir := t.TempDir()
	for name, text := range map[string]string{"index.html": "<p>the console</p>", "app.js": "console.log('weir')"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	const admin = "the-admin-key"
	srv := httptest.NewServer(api.New(eng, reg, api.WithAdminKey(admin), api.WithProxyUser("X-Signed-In"), api.WithPrefix("/api"), api.WithConsole(dir)))
	defer srv.Close()

	// The proxy sends a credential of its own as well as the person's address.
	person := map[string]string{"X-Signed-In": "accounts.google.com:lead@example.com", "Authorization": "Bearer the-proxys-own-token"}

	// Nobody is let in without a key or the proxy's word.
	if code, _ := ask(t, "GET", srv.URL+"/v1/definitions", "", nil); code != http.StatusUnauthorized {
		t.Errorf("a request with neither a key nor a signed-in person = %d, want 401", code)
	}
	// A signed-in person needs no key, and is the actor without saying so.
	if code, body := ask(t, "GET", srv.URL+"/v1/me", "", person); code != http.StatusOK || !strings.Contains(body, `"name":"lead@example.com"`) || !strings.Contains(body, `"signed_in":true`) {
		t.Errorf("who the server takes a signed-in person to be = %d %s", code, body)
	}
	if code, body := ask(t, "POST", srv.URL+"/v1/workspaces", `{"slug":"acme","name":"Acme"}`, person); code != http.StatusCreated {
		t.Errorf("a signed-in person creating a workspace = %d %s, want 201", code, body)
	}
	// A key still works, for a caller that is not a person.
	if code, body := ask(t, "GET", srv.URL+"/v1/me", "", map[string]string{"Authorization": "Bearer " + admin}); code != http.StatusOK || !strings.Contains(body, `"signed_in":false`) {
		t.Errorf("who the server takes the admin key to be = %d %s", code, body)
	}

	// The API answers under the prefix as well.
	if code, _ := ask(t, "GET", srv.URL+"/api/healthz", "", nil); code != http.StatusOK {
		t.Errorf("health check under the prefix = %d", code)
	}
	if code, body := ask(t, "GET", srv.URL+"/api/v1/workspaces", "", person); code != http.StatusOK || !strings.Contains(body, "acme") {
		t.Errorf("workspaces under the prefix = %d %s", code, body)
	}

	// Everything that is not the API is the console.
	for _, page := range []string{"/", "/runs/some-run", "/workflows"} {
		if code, body := ask(t, "GET", srv.URL+page, "", nil); code != http.StatusOK || !strings.Contains(body, "the console") {
			t.Errorf("GET %s = %d %q, want the console's page", page, code, body)
		}
	}
	if code, body := ask(t, "GET", srv.URL+"/app.js", "", nil); code != http.StatusOK || !strings.Contains(body, "console.log") {
		t.Errorf("GET /app.js = %d %q, want the file", code, body)
	}
	// The console never answers for the API, nor for a path outside its directory.
	if code, body := ask(t, "GET", srv.URL+"/v1/no-such-thing", "", person); code != http.StatusNotFound || strings.Contains(body, "the console") {
		t.Errorf("GET /v1/no-such-thing = %d %q, want the API's 404", code, body)
	}
	if _, body := ask(t, "GET", srv.URL+"/..%2f..%2fetc/passwd", "", nil); strings.Contains(body, "root:") {
		t.Errorf("a path outside the console's directory was served: %q", body)
	}

	// A server not told to trust the header does not.
	plain := httptest.NewServer(api.New(eng, reg, api.WithAdminKey(admin)))
	defer plain.Close()
	if code, _ := ask(t, "GET", plain.URL+"/v1/definitions", "", person); code != http.StatusUnauthorized {
		t.Errorf("a server that trusts no proxy, given the header = %d, want 401", code)
	}
	if code, body := ask(t, "GET", plain.URL+"/", "", nil); strings.Contains(body, "the console") {
		t.Errorf("a server with no console served one: %d %q", code, body)
	}
}

func TestAConnectionCanSignInAsTheServer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool := testdb.New(ctx, t)
	eng, err := engine.New(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	reg := worker.NewRegistry()
	srv := httptest.NewServer(api.New(eng, reg))
	defer srv.Close()

	who := map[string]string{"X-Weir-Actor": "lead", "Content-Type": "application/json"}
	if code, body := ask(t, "PUT", srv.URL+"/v1/connections/my-app", `{"base_url":"https://my-app.example.com","auth":{"kind":"id-token"}}`, who); code >= 300 {
		t.Fatalf("a connection that signs in as the server = %d %s", code, body)
	}
	if code, body := ask(t, "GET", srv.URL+"/v1/connections", "", nil); code != http.StatusOK || !strings.Contains(body, `"kind": "id-token"`) && !strings.Contains(body, `"kind":"id-token"`) {
		t.Errorf("the connection as listed = %d %s, want its kind shown", code, body)
	}
	// It has no secret, so naming one is a mistake to refuse.
	if code, _ := ask(t, "PUT", srv.URL+"/v1/connections/other", `{"base_url":"https://other.example.com","auth":{"kind":"id-token","secret":"env:TOKEN"}}`, who); code != http.StatusBadRequest {
		t.Errorf("an id-token connection with a secret = %d, want 400", code)
	}
}
