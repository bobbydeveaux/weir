package secrets

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func env(vars map[string]string) func(string) string {
	return func(k string) string { return vars[k] }
}

func TestAWorkspaceReadsOnlyItsOwnSecrets(t *testing.T) {
	r := &Resolver{Getenv: env(map[string]string{
		"WEIR_SECRET__ACME__GITHUB_TOKEN":  "acme-token",
		"WEIR_SECRET__RIVAL__GITHUB_TOKEN": "rival-token",
		"WEIR_ADMIN_KEY":                   "the-admin-key",
		"WEIR_DATABASE_URL":                "postgres://secret",
		"GITHUB_TOKEN":                     "the-servers-own",
	})}
	if v, err := r.Resolve("acme", "env:GITHUB_TOKEN"); err != nil || v != "acme-token" {
		t.Fatalf("acme's own secret = %q, %v", v, err)
	}
	if v, _ := r.Resolve("rival", "env:GITHUB_TOKEN"); v != "rival-token" {
		t.Errorf("rival's own secret = %q", v)
	}
	// Nothing a workspace can write reaches the server's settings or another workspace.
	for _, ref := range []string{"env:WEIR_ADMIN_KEY", "env:WEIR_DATABASE_URL", "env:WEIR_SECRET__RIVAL__GITHUB_TOKEN",
		"env:RIVAL__GITHUB_TOKEN", "env:__RIVAL__GITHUB_TOKEN", "env:github_token", "env:", "GITHUB_TOKEN", "vault:x"} {
		if v, err := r.Resolve("acme", ref); err == nil {
			t.Errorf("acme resolved %q to %q", ref, v)
		}
	}
	// Two different pairs of workspace and name never share a variable.
	a, _ := r.Where("a-b", "env:C")
	b, _ := r.Where("a", "env:B_C")
	if a == b || a != "WEIR_SECRET__A_B__C" {
		t.Errorf("workspace a-b / C is read from %q and workspace a / B_C from %q", a, b)
	}
	if _, err := r.Where("a--b", "env:C"); err == nil {
		t.Error("a workspace with a double hyphen could collide with another and was allowed")
	}
}

func TestFileSecretsStayInsideTheWorkspacesDirectory(t *testing.T) {
	dir := t.TempDir()
	for path, value := range map[string]string{"acme/token": "acme-file\n", "rival/token": "rival-file", "outside": "top-level"} {
		full := filepath.Join(dir, path)
		_ = os.MkdirAll(filepath.Dir(full), 0o700)
		if err := os.WriteFile(full, []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	r := &Resolver{Getenv: env(nil), Dir: dir}
	if v, err := r.Resolve("acme", "file:token"); err != nil || v != "acme-file" {
		t.Fatalf("acme's file = %q, %v (the trailing newline should be dropped)", v, err)
	}
	for _, ref := range []string{"file:../rival/token", "file:../outside", "file:/etc/passwd", "file:..", "file:.hidden", "file:a/b", `file:a\b`} {
		if v, err := r.Resolve("acme", ref); err == nil {
			t.Errorf("acme resolved %q to %q", ref, v)
		}
	}
	if _, err := (&Resolver{Getenv: env(nil)}).Resolve("acme", "file:token"); err == nil || !strings.Contains(err.Error(), "WEIR_SECRETS_DIR") {
		t.Errorf("with no directory set, err = %v", err)
	}
}

func TestAMissingSecretSaysWhereToPutItAndSendsNothing(t *testing.T) {
	r := &Resolver{Getenv: env(nil)}
	req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	err := r.Apply(req, "acme", Auth{Kind: Bearer, Secret: "env:GITHUB_TOKEN"})
	if err == nil || !strings.Contains(err.Error(), "WEIR_SECRET__ACME__GITHUB_TOKEN") {
		t.Fatalf("err = %v, want it to name the variable to set", err)
	}
	if len(req.Header) != 0 {
		t.Errorf("headers were set although the secret is missing: %v", req.Header)
	}
}

func TestEachKindOfAuthSignsTheRequest(t *testing.T) {
	r := &Resolver{Getenv: env(map[string]string{"WEIR_SECRET__ACME__KEY": "s3cret"})}
	for _, tc := range []struct {
		auth          Auth
		header, value string
	}{
		{Auth{Kind: Bearer, Secret: "env:KEY"}, "Authorization", "Bearer s3cret"},
		{Auth{Kind: Header, Secret: "env:KEY", Header: "X-Api-Key"}, "X-Api-Key", "s3cret"},
		{Auth{Kind: Basic, Secret: "env:KEY", Username: "weir"}, "Authorization", "Basic d2VpcjpzM2NyZXQ="},
	} {
		req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
		if err := r.Apply(req, "acme", tc.auth); err != nil || req.Header.Get(tc.header) != tc.value {
			t.Errorf("%s: %s = %q, %v", tc.auth.Kind, tc.header, req.Header.Get(tc.header), err)
		}
	}
	req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	if err := r.Apply(req, "acme", Auth{}); err != nil || len(req.Header) != 0 {
		t.Errorf("a connection with no auth changed the request: %v %v", req.Header, err)
	}
	for _, bad := range []Auth{
		{Kind: "magic", Secret: "env:KEY"}, {Kind: Header, Secret: "env:KEY"}, {Kind: Header, Secret: "env:KEY", Header: "Host"},
		{Kind: Basic, Secret: "env:KEY"}, {Kind: Basic, Secret: "env:KEY", Username: "a:b"}, {Kind: Bearer}, {Secret: "env:KEY"},
	} {
		if err := bad.Check(); err == nil {
			t.Errorf("%+v was accepted", bad)
		}
	}
}

func TestACredentialIsNotFollowedToAnotherHost(t *testing.T) {
	var got string
	elsewhere := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { got = r.Header.Get("X-Api-Key") }))
	defer elsewhere.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL+"/steal", http.StatusFound)
	}))
	defer origin.Close()

	req, _ := http.NewRequest(http.MethodGet, origin.URL, nil)
	req.Header.Set("X-Api-Key", "s3cret")
	resp, err := (&http.Client{CheckRedirect: SameHost}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound || got != "" {
		t.Errorf("status %d, and the other host received %q", resp.StatusCode, got)
	}
}
