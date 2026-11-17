package worker

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/bobbydeveaux/weir/engine"
	"github.com/bobbydeveaux/weir/internal/secrets"
)

// appGitHub stands in for GitHub's App endpoints. It accepts only a request
// signed by the App's key, and notes what tokens were asked for.
type appGitHub struct {
	key     *rsa.PublicKey
	asked   []map[string]any
	lookups int
	srv     *httptest.Server
}

func (g *appGitHub) signed(t *testing.T, r *http.Request) bool {
	parts := strings.Split(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), ".")
	if len(parts) != 3 {
		return false
	}
	sig, _ := base64.RawURLEncoding.DecodeString(parts[2])
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if rsa.VerifyPKCS1v15(g.key, crypto.SHA256, sum[:], sig) != nil {
		return false
	}
	var claims struct {
		Iss      string
		Iat, Exp int64
	}
	raw, _ := base64.RawURLEncoding.DecodeString(parts[1])
	_ = json.Unmarshal(raw, &claims)
	now := time.Now().Unix()
	if claims.Iss != "Iv23test" || claims.Iat > now || claims.Exp <= now || claims.Exp-claims.Iat > 600 {
		t.Errorf("claims = %+v", claims)
		return false
	}
	return true
}

func newAppGitHub(t *testing.T, key *rsa.PublicKey) *appGitHub {
	g := &appGitHub{key: key}
	g.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if !g.signed(t, r) {
			w.WriteHeader(401)
			_, _ = io.WriteString(w, `{"message": "A JSON web token could not be decoded"}`)
			return
		}
		switch {
		case r.URL.Path == "/app":
			_, _ = io.WriteString(w, `{"name": "Fixer", "slug": "fixer", "owner": {"login": "acme"}, "permissions": {"contents": "write"}}`)
		case r.URL.Path == "/app/installations":
			_, _ = io.WriteString(w, `[{"account": {"login": "acme"}, "repository_selection": "selected"}]`)
		case r.URL.Path == "/repos/acme/app/installation":
			g.lookups++
			_, _ = io.WriteString(w, `{"id": 77}`)
		case r.URL.Path == "/app/installations/77/access_tokens" && r.Method == http.MethodPost:
			var in map[string]any
			_ = json.NewDecoder(r.Body).Decode(&in)
			g.asked = append(g.asked, in)
			w.WriteHeader(201)
			_, _ = io.WriteString(w, `{"token": "ghs_minted"}`)
		default:
			w.WriteHeader(404)
			_, _ = io.WriteString(w, `{"message": "Not Found"}`)
		}
	}))
	t.Cleanup(g.srv.Close)
	return g
}

func TestAGitHubAppSignsInWithATokenForOneRepository(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pemKey := string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
	gh := newAppGitHub(t, &key.PublicKey)
	reg := NewRegistry()
	env := map[string]string{"WEIR_SECRET__ACME__GITHUB_APP_KEY": pemKey}
	reg.Secrets = &secrets.Resolver{Getenv: func(k string) string { return env[k] }}
	to := engine.Endpoint{Connection: "github", URL: gh.srv.URL, Workspace: "acme",
		Auth: engine.Auth{Kind: secrets.GitHubApp, App: "Iv23test", Secret: "env:GITHUB_APP_KEY"}}
	ctx := context.Background()

	token, err := reg.GitHubToken(ctx, nil, to, "acme/app", GitHubRead)
	if err != nil || token != "ghs_minted" {
		t.Fatalf("GitHubToken = %q, %v", token, err)
	}
	asked := gh.asked[0]
	if repos, _ := json.Marshal(asked["repositories"]); string(repos) != `["app"]` {
		t.Errorf("the token was asked for %s, want one repository", repos)
	}
	if perms, _ := json.Marshal(asked["permissions"]); string(perms) != `{"contents":"read","metadata":"read"}` {
		t.Errorf("the token was asked for %s, want read only", perms)
	}
	// The same need again uses the token already held; a different need does not.
	if _, err := reg.GitHubToken(ctx, nil, to, "acme/app", GitHubRead); err != nil || len(gh.asked) != 1 {
		t.Errorf("a second read asked GitHub again: %d requests, %v", len(gh.asked), err)
	}
	if _, err := reg.GitHubToken(ctx, nil, to, "acme/app", GitHubCommit); err != nil || len(gh.asked) != 2 {
		t.Errorf("a write reused the read token: %d requests, %v", len(gh.asked), err)
	}
	// A pinned connection's address ends with the repository; the App's own
	// endpoints are still found.
	pinned := to
	pinned.Connection, pinned.URL = "github-app", gh.srv.URL+"/repos/acme/app"
	if _, err := reg.GitHubToken(ctx, nil, pinned, "acme/app", GitHubRead); err != nil {
		t.Errorf("a pinned connection: %v", err)
	}

	status, err := reg.GitHubApp(ctx, nil, to)
	if err != nil || status.Slug != "fixer" || len(status.Installations) != 1 || status.Installations[0].Repositories != "selected" {
		t.Errorf("GitHubApp = %+v, %v", status, err)
	}

	// What goes wrong says so, and never holds the key.
	if _, err := reg.GitHubToken(ctx, nil, to, "acme/other", GitHubRead); err == nil || !strings.Contains(err.Error(), "not installed on acme/other") {
		t.Errorf("a repository the App is not installed on = %v", err)
	}
	if _, err := reg.GitHubToken(ctx, nil, to, "../etc", GitHubRead); err == nil {
		t.Error("a repository that is not owner/name was accepted")
	}
	delete(env, "WEIR_SECRET__ACME__GITHUB_APP_KEY")
	fresh := NewRegistry()
	fresh.Secrets = reg.Secrets
	if _, err := fresh.GitHubToken(ctx, nil, to, "acme/app", GitHubRead); err == nil || !strings.Contains(err.Error(), "WEIR_SECRET__ACME__GITHUB_APP_KEY") {
		t.Errorf("a missing key = %v, want it to name the variable to set", err)
	}
	env["WEIR_SECRET__ACME__GITHUB_APP_KEY"] = "not a key"
	if _, err := fresh.GitHubToken(ctx, nil, to, "acme/app", GitHubRead); err == nil || strings.Contains(err.Error(), "not a key") {
		t.Errorf("a secret that is not a key = %v, want an error that does not repeat it", err)
	}
	other, _ := rsa.GenerateKey(rand.Reader, 2048)
	env["WEIR_SECRET__ACME__GITHUB_APP_KEY"] = string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(other)}))
	if _, err := fresh.GitHubApp(ctx, nil, to); err == nil || !strings.Contains(err.Error(), "did not accept") {
		t.Errorf("the wrong key = %v", err)
	}

	// Anything but the GitHub tools is refused the connection.
	req, _ := http.NewRequest(http.MethodGet, gh.srv.URL+"/anything", nil)
	if err := reg.sign(req, to); err == nil || req.Header.Get("Authorization") != "" {
		t.Errorf("a plain call was signed with a GitHub App connection: %v", err)
	}
}

func TestEachGitHubCallAsksForTheLeastItNeeds(t *testing.T) {
	for _, tt := range []struct {
		method, path string
		want         map[string]string
	}{
		{"GET", "/contents/README.md", GitHubRead},
		{"GET", "", GitHubRead},
		{"GET", "/pulls/4", GitHubReadPulls},
		{"GET", "/pulls/4/files", GitHubReadPulls},
		{"PUT", "/contents/a.txt", GitHubWrite},
		{"POST", "/git/refs", GitHubWrite},
		{"POST", "/pulls", GitHubWritePulls},
		{"POST", "/issues/4/comments", GitHubWritePulls},
	} {
		got, _ := json.Marshal(githubNeeds(tt.method, tt.path))
		want, _ := json.Marshal(tt.want)
		if string(got) != string(want) {
			t.Errorf("%s %s needs %s, want %s", tt.method, tt.path, got, want)
		}
	}
}

// TestGitHubAppLive signs in as a real GitHub App and reads one repository.
// It changes nothing. It runs only when asked:
//
//	WEIR_TEST_GITHUB_APP       the App's client id
//	WEIR_TEST_GITHUB_APP_KEY   the path of its private key
//	WEIR_TEST_GITHUB_APP_REPO  a repository it is installed on, as owner/name
func TestGitHubAppLive(t *testing.T) {
	app, keyPath, repo := os.Getenv("WEIR_TEST_GITHUB_APP"), os.Getenv("WEIR_TEST_GITHUB_APP_KEY"), os.Getenv("WEIR_TEST_GITHUB_APP_REPO")
	if app == "" || keyPath == "" {
		t.Skip("set WEIR_TEST_GITHUB_APP and WEIR_TEST_GITHUB_APP_KEY to sign in as a real GitHub App")
	}
	raw, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	reg := NewRegistry()
	reg.Secrets = &secrets.Resolver{Getenv: func(k string) string {
		if k == "WEIR_SECRET__TEST__GITHUB_APP_KEY" {
			return string(raw)
		}
		return ""
	}}
	to := engine.Endpoint{Connection: "github", URL: "https://api.github.com", Workspace: "test",
		Auth: engine.Auth{Kind: secrets.GitHubApp, App: app, Secret: "env:GITHUB_APP_KEY"}}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	status, err := reg.GitHubApp(ctx, nil, to)
	if err != nil {
		t.Fatalf("GitHubApp: %v", err)
	}
	t.Logf("signed in as %s (%s), owned by %s, permissions %v", status.Name, status.Slug, status.Owner, status.Permissions)
	for _, in := range status.Installations {
		t.Logf("  installed on %s (%s repositories)", in.Account, in.Repositories)
	}
	if repo == "" {
		return
	}
	token, err := reg.GitHubToken(ctx, nil, to, repo, GitHubRead)
	if err != nil {
		t.Fatalf("GitHubToken: %v", err)
	}
	var got struct {
		FullName      string `json:"full_name"`
		DefaultBranch string `json:"default_branch"`
		Private       bool
	}
	if err := githubCall(ctx, nil, http.MethodGet, "https://api.github.com/repos/"+repo, token, nil, &got); err != nil {
		t.Fatalf("reading %s with the token: %v", repo, err)
	}
	t.Logf("read %s: default branch %s, private %v", got.FullName, got.DefaultBranch, got.Private)
}
