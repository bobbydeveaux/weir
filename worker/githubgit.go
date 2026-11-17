package worker

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/bobbydeveaux/weir/engine"
	"github.com/bobbydeveaux/weir/internal/secrets"
	"github.com/bobbydeveaux/weir/runner"
)

// The GitHub Pipe's git half. It is deterministic code that runs on the
// worker, outside any agent's sandbox, and it follows three rules:
//
//   - A token is handed to git through its environment for one command. It is
//     never in the remote's address, in the repository's configuration, or in
//     a process listing, so an agent that later works in the directory cannot
//     find it.
//   - Git runs with no configuration but what Weir gives it, and with hooks
//     switched off, so nothing in a repository runs because it was cloned.
//   - Git is never run in a directory after an agent has been in it.

// githubRemote returns the address git clones a repository from, given the
// API's address. Tests replace it.
var githubRemote = func(api, repo string) (string, error) {
	switch {
	case api == "https://api.github.com":
		return "https://github.com/" + repo + ".git", nil
	case strings.HasSuffix(api, "/api/v3"):
		// GitHub Enterprise Server serves git from the same host as its API.
		return strings.TrimSuffix(api, "/api/v3") + "/" + repo + ".git", nil
	}
	return "", fmt.Errorf("cannot tell where to clone from for the API address %s", api)
}

// gitToken returns a token for one repository: minted for a GitHub App, or
// the connection's own for one that signs in with a token.
func (r *Registry) gitToken(ctx context.Context, client *http.Client, to engine.Endpoint, repo string, perms map[string]string) (string, error) {
	switch to.Auth.Kind {
	case secrets.GitHubApp:
		return r.GitHubToken(ctx, client, to, repo, perms)
	case secrets.Bearer:
		token, err := r.ResolveSecret(to.Workspace, to.Auth.Secret)
		if err != nil {
			return "", fmt.Errorf("connection %s: %w", to.Connection, err)
		}
		return token, nil
	case "", secrets.None:
		return "", nil // a public repository needs no token to read
	}
	return "", fmt.Errorf("connection %s signs in with %s, which git cannot use: use a GitHub App or a bearer token", to.Connection, to.Auth.Kind)
}

// git runs one git command in dir. token, when set, is sent to remote and
// nowhere else.
func git(ctx context.Context, token, remote, dir string, args ...string) (string, error) {
	config := [][2]string{
		{"core.hooksPath", os.DevNull},  // no hook in any repository runs
		{"safe.directory", "*"},         // the directory may belong to another user
		{"protocol.file.allow", "user"}, // a local remote is for tests only, and must be asked for
		{"credential.helper", ""},       // nothing is asked for or remembered
		{"core.fsmonitor", "false"},     // no program named by a repository runs
		{"advice.detachedHead", "false"},
	}
	if token != "" {
		// Limited to the remote's own address, so a redirect or a submodule
		// elsewhere is never sent it.
		basic := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + token))
		config = append(config, [2]string{"http." + strings.TrimSuffix(remote, ".git") + ".git.extraheader", "AUTHORIZATION: basic " + basic})
	}
	env := []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.TempDir(), "LANG=C", "GIT_TERMINAL_PROMPT=0",
		"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_CONFIG_COUNT=" + strconv.Itoa(len(config))}
	for i, kv := range config {
		env = append(env, fmt.Sprintf("GIT_CONFIG_KEY_%d=%s", i, kv[0]), fmt.Sprintf("GIT_CONFIG_VALUE_%d=%s", i, kv[1]))
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir, cmd.Env = dir, env
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		text := strings.TrimSpace(out.String())
		if token != "" {
			text = strings.ReplaceAll(text, token, "[token]")
		}
		if len(text) > 400 {
			text = text[len(text)-400:]
		}
		if errors.Is(err, exec.ErrNotFound) {
			return "", errors.New("git is not installed on this worker")
		}
		return "", fmt.Errorf("git %s: %s", args[0], text)
	}
	return out.String(), nil
}

// maxCheckoutDepth is the most history a checkout may fetch.
const maxCheckoutDepth = 1000

// RegisterGitHubCheckout adds "github.checkout", which fills an agent's
// working directory with one repository at one ref.
//
// It reads "repo" (as "owner/name") and "ref" (a branch or tag; the default
// branch when left out) from the step's input, and "connection" (default
// "github") and "depth" (default 1) from the step's "with". A connection
// pinned to one repository needs no "repo".
func RegisterGitHubCheckout(reg *Registry, eng *engine.Engine, client *http.Client) {
	reg.RegisterWorkspace("github.checkout", func(ctx context.Context, w Workspace) (runner.Prepared, error) {
		var in struct {
			Repo string `json:"repo"`
			Ref  string `json:"ref"`
		}
		_ = json.Unmarshal(w.Input, &in)
		connection, _ := w.With["connection"].(string)
		depth := 1
		switch d := w.With["depth"].(type) {
		case int:
			depth = d
		case float64:
			depth = int(d)
		}
		if depth < 1 || depth > maxCheckoutDepth {
			return runner.Prepared{}, Permanent(fmt.Errorf("depth must be between 1 and %d", maxCheckoutDepth))
		}
		if in.Ref != "" && !githubBranch.MatchString(in.Ref) {
			return runner.Prepared{}, Permanent(fmt.Errorf("ref %q is not a branch or tag name", in.Ref))
		}
		g, err := githubFor(ctx, reg, eng, client, w.RunID, connection, in.Repo)
		if err != nil {
			return runner.Prepared{}, err
		}
		remote, err := githubRemote(g.api, g.repo)
		if err != nil {
			return runner.Prepared{}, err
		}
		token, err := reg.gitToken(ctx, client, g.to, g.repo, GitHubRead)
		if err != nil {
			return runner.Prepared{}, err
		}
		prepared, err := checkout(ctx, token, remote, w.Dir, in.Ref, depth)
		if err != nil {
			return runner.Prepared{}, fmt.Errorf("check out %s: %w", g.repo, err)
		}
		return prepared, nil
	})
}

// checkout clones remote at ref into dir, which must be empty, and returns
// what the repository says to ignore.
func checkout(ctx context.Context, token, remote, dir, ref string, depth int) (runner.Prepared, error) {
	args := []string{"clone", "--quiet", "--depth", strconv.Itoa(depth), "--no-recurse-submodules"}
	if ref != "" {
		args = append(args, "--branch", ref)
	}
	if _, err := git(ctx, token, remote, dir, append(args, "--", remote, ".")...); err != nil {
		return runner.Prepared{}, err
	}
	patterns, err := ignoredPaths(dir)
	if err != nil {
		return runner.Prepared{}, err
	}
	return runner.Prepared{Ignore: patterns}, nil
}

// ignoredPaths gathers every .gitignore in a checkout into one list, as
// patterns relative to the checkout's root. A pattern in a directory's own
// .gitignore applies only under that directory.
func ignoredPaths(root string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() != ".gitignore" || !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(root, filepath.Dir(p))
		if err != nil {
			return err
		}
		file, err := os.Open(p)
		if err != nil {
			return err
		}
		defer file.Close()
		lines := bufio.NewScanner(file)
		for lines.Scan() {
			if pattern := underDirectory(filepath.ToSlash(rel), lines.Text()); pattern != "" {
				out = append(out, pattern)
			}
		}
		return lines.Err()
	})
	return out, err
}

// underDirectory rewrites one line of a .gitignore found in dir so that it
// means the same when read from the root. It returns "" for a line that is
// blank or a comment.
func underDirectory(dir, line string) string {
	line = strings.TrimRight(line, " \t\r")
	if line == "" || strings.HasPrefix(line, "#") {
		return ""
	}
	if dir == "." || dir == "" {
		return line
	}
	not := ""
	if strings.HasPrefix(line, "!") {
		not, line = "!", line[1:]
	}
	// A pattern with a slash before its end is tied to its directory. One
	// without matches at any depth below it.
	if strings.Contains(strings.TrimSuffix(line, "/"), "/") {
		return not + "/" + path.Join(dir, strings.TrimPrefix(line, "/")) + trailingSlash(line)
	}
	return not + "/" + dir + "/**/" + line
}

func trailingSlash(s string) string {
	if strings.HasSuffix(s, "/") {
		return "/"
	}
	return ""
}
