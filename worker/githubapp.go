package worker

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bobbydeveaux/weir/engine"
	"github.com/bobbydeveaux/weir/internal/secrets"
)

// What a token for one repository may do. A token is asked for with the least
// that the call needs.
var (
	// GitHubRead is enough to read files and clone.
	GitHubRead = map[string]string{"contents": "read", "metadata": "read"}
	// GitHubReadPulls is enough to read a pull request.
	GitHubReadPulls = map[string]string{"pull_requests": "read", "metadata": "read"}
	// GitHubWrite is enough to push a branch.
	GitHubWrite = map[string]string{"contents": "write", "metadata": "read"}
	// GitHubWritePulls is enough to open a pull request or comment on one.
	// Opening one needs to read the branches it joins, or GitHub refuses with
	// "not all refs are readable".
	GitHubWritePulls = map[string]string{"pull_requests": "write", "contents": "read", "metadata": "read"}
	// GitHubCommit is enough to push a branch and open its pull request.
	GitHubCommit = map[string]string{"contents": "write", "pull_requests": "write", "metadata": "read"}
)

// githubTokens remembers tokens for a few minutes, in memory only, so that an
// agent making many calls does not ask GitHub for a token each time. A token
// is never written anywhere.
type githubTokens struct {
	mu   sync.Mutex
	held map[string]heldToken
}

type heldToken struct {
	token string
	until time.Time
}

// tokenLife is how long a token is reused. GitHub's tokens last an hour.
const tokenLife = 10 * time.Minute

// githubAPI is the API's address for a connection: its own, without the
// repository a pinned connection ends with.
func githubAPI(to engine.Endpoint) string {
	api := strings.TrimRight(to.URL, "/")
	if m := githubPinned.FindStringSubmatch(api); m != nil {
		return m[1]
	}
	return api
}

// appKey reads and parses the private key a GitHub App connection names.
func (r *Registry) appKey(to engine.Endpoint) (*rsa.PrivateKey, error) {
	if to.Auth.Kind != secrets.GitHubApp {
		return nil, fmt.Errorf("connection %s does not sign in as a GitHub App", to.Connection)
	}
	raw, err := r.ResolveSecret(to.Workspace, to.Auth.Secret)
	if err != nil {
		return nil, fmt.Errorf("connection %s: %w", to.Connection, err)
	}
	block, _ := pem.Decode([]byte(raw))
	if block == nil {
		return nil, fmt.Errorf("connection %s: its secret is not a private key in PEM form, as GitHub gives one", to.Connection)
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("connection %s: its private key could not be read", to.Connection)
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("connection %s: its private key is not an RSA key", to.Connection)
	}
	return key, nil
}

// appJWT is what GitHub expects from an App: signed with its key, dated half
// a minute back in case clocks disagree, and good for nine minutes.
func appJWT(app string, key *rsa.PrivateKey, now time.Time) (string, error) {
	part := func(v any) string {
		raw, _ := json.Marshal(v)
		return base64.RawURLEncoding.EncodeToString(raw)
	}
	signing := part(map[string]string{"alg": "RS256", "typ": "JWT"}) + "." +
		part(map[string]any{"iat": now.Unix() - 30, "exp": now.Unix() + 540, "iss": app})
	sum := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		return "", err
	}
	return signing + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

// githubCall makes one call to GitHub's API as bearer and decodes the reply.
func githubCall(ctx context.Context, client *http.Client, method, url, bearer string, body, out any) error {
	var payload io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		payload = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, payload)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "weir")
	req.Header.Set("Authorization", "Bearer "+bearer)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second, CheckRedirect: secrets.SameHost}
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("GitHub could not be reached: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode >= 300 {
		var e struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(raw, &e)
		if e.Message == "" {
			e.Message = http.StatusText(resp.StatusCode)
		}
		return fmt.Errorf("GitHub said %d: %s", resp.StatusCode, e.Message)
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}

// GitHubToken returns a short-lived token for one repository ("owner/name"),
// limited to perms, for a connection that signs in as a GitHub App. The token
// must not be stored, logged, or given to an agent.
func (r *Registry) GitHubToken(ctx context.Context, client *http.Client, to engine.Endpoint, repo string, perms map[string]string) (string, error) {
	if !githubRepo.MatchString(repo) {
		return "", fmt.Errorf(`a token is for one repository, named as "owner/name", not %q`, repo)
	}
	names := make([]string, 0, len(perms))
	for k, v := range perms {
		names = append(names, k+"="+v)
	}
	// The order of a map is not fixed, so sort before using it as a key.
	sort.Strings(names)
	cacheKey := strings.Join([]string{to.Workspace, to.Connection, to.Auth.App, to.Auth.Secret, githubAPI(to), strings.ToLower(repo), strings.Join(names, ",")}, "|")

	r.github.mu.Lock()
	held, ok := r.github.held[cacheKey]
	r.github.mu.Unlock()
	if ok && time.Now().Before(held.until) {
		return held.token, nil
	}

	key, err := r.appKey(to)
	if err != nil {
		return "", err
	}
	jwt, err := appJWT(to.Auth.App, key, time.Now())
	if err != nil {
		return "", err
	}
	api := githubAPI(to)
	var installation struct {
		ID int64 `json:"id"`
	}
	if err := githubCall(ctx, client, http.MethodGet, api+"/repos/"+repo+"/installation", jwt, nil, &installation); err != nil {
		return "", fmt.Errorf("connection %s: its GitHub App is not installed on %s, or could not be asked: %w", to.Connection, repo, err)
	}
	var minted struct {
		Token string `json:"token"`
	}
	_, name, _ := strings.Cut(repo, "/")
	request := map[string]any{"repositories": []string{name}, "permissions": perms}
	if err := githubCall(ctx, client, http.MethodPost, fmt.Sprintf("%s/app/installations/%d/access_tokens", api, installation.ID), jwt, request, &minted); err != nil {
		return "", fmt.Errorf("connection %s: GitHub would not give its App a token for %s: %w", to.Connection, repo, err)
	}
	if minted.Token == "" {
		return "", errors.New("GitHub's reply held no token")
	}
	r.github.mu.Lock()
	if r.github.held == nil {
		r.github.held = map[string]heldToken{}
	}
	r.github.held[cacheKey] = heldToken{token: minted.Token, until: time.Now().Add(tokenLife)}
	r.github.mu.Unlock()
	return minted.Token, nil
}

// GitHubAppStatus is what a GitHub App is and where it is installed.
type GitHubAppStatus struct {
	Name          string               `json:"name"`
	Slug          string               `json:"slug"`
	Owner         string               `json:"owner"`
	Permissions   map[string]string    `json:"permissions"`
	Installations []GitHubInstallation `json:"installations"`
}

// GitHubInstallation is one place a GitHub App is installed.
type GitHubInstallation struct {
	Account string `json:"account"`
	// Repositories is "all" or "selected".
	Repositories string `json:"repositories"`
}

// GitHubApp asks GitHub who a connection's App is and where it is installed.
// It proves that Weir can sign in, without doing anything.
func (r *Registry) GitHubApp(ctx context.Context, client *http.Client, to engine.Endpoint) (GitHubAppStatus, error) {
	var out GitHubAppStatus
	key, err := r.appKey(to)
	if err != nil {
		return out, err
	}
	jwt, err := appJWT(to.Auth.App, key, time.Now())
	if err != nil {
		return out, err
	}
	api := githubAPI(to)
	var app struct {
		Name        string            `json:"name"`
		Slug        string            `json:"slug"`
		Permissions map[string]string `json:"permissions"`
		Owner       struct {
			Login string `json:"login"`
		} `json:"owner"`
	}
	if err := githubCall(ctx, client, http.MethodGet, api+"/app", jwt, nil, &app); err != nil {
		return out, fmt.Errorf("connection %s: GitHub did not accept its App's key: %w", to.Connection, err)
	}
	out.Name, out.Slug, out.Owner, out.Permissions = app.Name, app.Slug, app.Owner.Login, app.Permissions
	var installs []struct {
		Account struct {
			Login string `json:"login"`
		} `json:"account"`
		Selection string `json:"repository_selection"`
	}
	if err := githubCall(ctx, client, http.MethodGet, api+"/app/installations?per_page=100", jwt, nil, &installs); err != nil {
		return out, err
	}
	out.Installations = []GitHubInstallation{}
	for _, in := range installs {
		out.Installations = append(out.Installations, GitHubInstallation{Account: in.Account.Login, Repositories: in.Selection})
	}
	return out, nil
}
