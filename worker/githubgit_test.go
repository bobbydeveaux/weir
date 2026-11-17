package worker

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	ignore "github.com/sabhiram/go-gitignore"

	"github.com/bobbydeveaux/weir/engine"
	"github.com/bobbydeveaux/weir/internal/secrets"
)

// origin makes a repository on disk to clone from, with two branches and
// nested ignore files.
func origin(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1",
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(name, body string) {
		t.Helper()
		p := filepath.Join(dir, filepath.FromSlash(name))
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("init", "--quiet", "--initial-branch", "main")
	write("main.go", "package main\n")
	write(".gitignore", "node_modules/\n*.log\n# a comment\n\n/dist\n")
	write("web/.gitignore", "build/\n/cache\n!keep.log\n")
	write("web/app.js", "x\n")
	run("add", "-A")
	run("commit", "--quiet", "-m", "first")
	run("checkout", "--quiet", "-b", "feature")
	write("feature.txt", "on the branch\n")
	run("add", "-A")
	run("commit", "--quiet", "-m", "second")
	run("checkout", "--quiet", "main")
	return dir
}

func TestACheckoutLeavesNoTokenBehind(t *testing.T) {
	remote := origin(t)
	ctx := context.Background()
	dir := t.TempDir()
	const token = "ghs_secret_token_value"

	prepared, err := checkout(ctx, token, remote, dir, "", 1)
	if err != nil {
		t.Fatalf("checkout: %v", err)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "main.go")); string(data) != "package main\n" {
		t.Errorf("main.go = %q", data)
	}
	if _, err := os.Stat(filepath.Join(dir, "feature.txt")); err == nil {
		t.Error("the default branch holds a file from another branch")
	}
	// The token is nowhere in the directory the agent will work in.
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if data, _ := os.ReadFile(p); strings.Contains(string(data), token) {
			t.Errorf("%s holds the token", p)
		}
		return nil
	})

	if config, _ := os.ReadFile(filepath.Join(dir, ".git", "config")); strings.Contains(strings.ToLower(string(config)), "extraheader") {
		t.Errorf("the repository's configuration holds a credential:\n%s", config)
	}

	// What the repository ignores, wherever it said so.
	skip := ignore.CompileIgnoreLines(prepared.Ignore...)
	for path, want := range map[string]bool{
		"node_modules/left-pad/index.js": true,
		"web/node_modules/x.js":          true,
		"test.log":                       true,
		"web/debug.log":                  true,
		"dist/app.js":                    true,
		"web/dist/app.js":                false, // "/dist" is tied to the root
		"web/build/out.js":               true,
		"build/out.js":                   false, // "build/" was said under web
		"web/cache/a":                    true,
		"web/src/cache/a":                false, // "/cache" is tied to web
		"main.go":                        false,
		"web/app.js":                     false,
	} {
		if got := skip.MatchesPath(path); got != want {
			t.Errorf("ignored(%s) = %v, want %v (patterns %q)", path, got, want, prepared.Ignore)
		}
	}

	// A named branch, and a ref that is not there.
	branch := t.TempDir()
	if _, err := checkout(ctx, "", remote, branch, "feature", 1); err != nil {
		t.Fatalf("checkout of a branch: %v", err)
	}
	if _, err := os.Stat(filepath.Join(branch, "feature.txt")); err != nil {
		t.Error("the branch's file is missing")
	}
	if _, err := checkout(ctx, token, remote, t.TempDir(), "no-such-branch", 1); err == nil || strings.Contains(err.Error(), token) {
		t.Errorf("a ref that does not exist = %v, want an error that does not hold the token", err)
	}
}

func TestWhereARepositoryIsClonedFrom(t *testing.T) {
	for api, want := range map[string]string{
		"https://api.github.com":             "https://github.com/acme/app.git",
		"https://github.example.com/api/v3":  "https://github.example.com/acme/app.git",
		"https://something-else.example.com": "",
	} {
		got, err := githubRemote(api, "acme/app")
		if got != want || (want == "") != (err != nil) {
			t.Errorf("githubRemote(%s) = %q, %v, want %q", api, got, err, want)
		}
	}
}

// TestGitHubCheckoutLive clones a real repository as a real GitHub App. It
// changes nothing. It runs only when asked; see TestGitHubAppLive.
func TestGitHubCheckoutLive(t *testing.T) {
	app, keyPath, repo := os.Getenv("WEIR_TEST_GITHUB_APP"), os.Getenv("WEIR_TEST_GITHUB_APP_KEY"), os.Getenv("WEIR_TEST_GITHUB_APP_REPO")
	if app == "" || keyPath == "" || repo == "" {
		t.Skip("set WEIR_TEST_GITHUB_APP, WEIR_TEST_GITHUB_APP_KEY and WEIR_TEST_GITHUB_APP_REPO to clone as a real GitHub App")
	}
	raw, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	reg := NewRegistry()
	reg.Secrets = &secrets.Resolver{Getenv: func(string) string { return string(raw) }}
	to := engine.Endpoint{Connection: "github", URL: "https://api.github.com", Workspace: "test",
		Auth: engine.Auth{Kind: secrets.GitHubApp, App: app, Secret: "env:GITHUB_APP_KEY"}}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	token, err := reg.gitToken(ctx, (*http.Client)(nil), to, repo, GitHubRead)
	if err != nil {
		t.Fatal(err)
	}
	remote, _ := githubRemote("https://api.github.com", repo)
	dir := t.TempDir()
	prepared, err := checkout(ctx, token, remote, dir, "", 1)
	if err != nil {
		t.Fatalf("checkout: %v", err)
	}
	files, leaked := 0, 0
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		files++
		if data, _ := os.ReadFile(p); strings.Contains(string(data), token) {
			leaked++
			t.Errorf("%s holds the token", strings.TrimPrefix(p, dir))
		}
		return nil
	})
	// Nor does git's own record of the repository say how to sign in.
	if config, _ := os.ReadFile(filepath.Join(dir, ".git", "config")); strings.Contains(strings.ToLower(string(config)), "extraheader") || strings.Contains(string(config), "@") {
		t.Errorf("the repository's configuration holds a credential:\n%s", config)
	}
	t.Logf("cloned %s: %d files, %d ignore patterns, token found in %d files", repo, files, len(prepared.Ignore), leaked)
	if files < 5 {
		t.Errorf("only %d files were cloned", files)
	}
	// Without the token, the same clone is refused: it is a private repository.
	if _, err := checkout(ctx, "", remote, t.TempDir(), "", 1); err == nil {
		t.Log("the repository is public, so a clone with no token also worked")
	}
}
