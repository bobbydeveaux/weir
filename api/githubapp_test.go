package api_test

import (
	"context"
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

func TestAGitHubAppConnectionSaysWhichSecretToSet(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	eng, err := engine.New(ctx, testdb.New(ctx, t))
	if err != nil {
		t.Fatal(err)
	}
	reg := worker.NewRegistry()
	reg.Secrets = &secrets.Resolver{Getenv: func(string) string { return "" }}
	srv := httptest.NewServer(api.New(eng, reg))
	t.Cleanup(srv.Close)
	call(t, srv.URL, "POST", "/v1/workspaces", "", `{"slug": "acme"}`, nil)

	if code := call(t, srv.URL, "PUT", "/v1/connections/github", "acme",
		`{"base_url": "https://api.github.com", "auth": {"kind": "github-app", "secret": "env:GITHUB_APP_KEY"}}`, nil); code != http.StatusBadRequest {
		t.Errorf("a GitHub App connection with no app = %d, want 400", code)
	}
	if code := call(t, srv.URL, "PUT", "/v1/connections/kb", "acme",
		`{"base_url": "https://kb.example", "kind": "mcp", "auth": {"kind": "github-app", "app": "Iv23x", "secret": "env:K"}}`, nil); code != http.StatusBadRequest {
		t.Errorf("an MCP connection signing in as a GitHub App = %d, want 400", code)
	}
	var put struct {
		SecretFound    bool   `json:"secret_found"`
		SecretReadFrom string `json:"secret_read_from"`
	}
	if code := call(t, srv.URL, "PUT", "/v1/connections/github", "acme",
		`{"base_url": "https://api.github.com", "auth": {"kind": "github-app", "app": "Iv23x", "secret": "env:GITHUB_APP_KEY"}}`, &put); code != http.StatusOK {
		t.Fatalf("put = %d", code)
	}
	if put.SecretFound || put.SecretReadFrom != "WEIR_SECRET__ACME__GITHUB_APP_KEY" {
		t.Errorf("put = %+v, want the name of the secret to set", put)
	}

	// Before the key is set, checking says what is missing rather than failing.
	var check struct {
		Checked, OK    bool
		Detail         string
		SecretReadFrom string `json:"secret_read_from"`
	}
	if code := call(t, srv.URL, "POST", "/v1/connections/github/check", "acme", "", &check); code != http.StatusOK {
		t.Fatalf("check = %d", code)
	}
	if !check.Checked || check.OK || !strings.Contains(check.Detail, "WEIR_SECRET__ACME__GITHUB_APP_KEY") {
		t.Errorf("check = %+v", check)
	}
	// Another workspace cannot check it, and a plain connection has nothing to check.
	if code := call(t, srv.URL, "POST", "/v1/connections/github/check", "", "", nil); code != http.StatusNotFound {
		t.Errorf("another workspace checking the connection = %d, want 404", code)
	}
	call(t, srv.URL, "PUT", "/v1/connections/plain", "acme", `{"base_url": "https://example.com"}`, nil)
	check.Checked = true
	if code := call(t, srv.URL, "POST", "/v1/connections/plain/check", "acme", "", &check); code != http.StatusOK || check.Checked {
		t.Errorf("checking a plain connection = %d, %+v", code, check)
	}
}
