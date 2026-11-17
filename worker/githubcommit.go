package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/bobbydeveaux/weir/engine"
	"github.com/bobbydeveaux/weir/internal/secrets"
	"github.com/bobbydeveaux/weir/runner"
)

// commitInput is what "github.commit" reads from its step's input.
type commitInput struct {
	// Connection is the connection to use. Empty means "github".
	Connection string `json:"connection"`
	// Repo is the repository, as "owner/name". Not needed if the connection
	// is for one repository.
	Repo string `json:"repo"`
	// From is the step whose files are committed.
	From string `json:"from"`
	// Branch is the new branch to push.
	Branch string `json:"branch"`
	// Base is the branch to start from and merge into. Empty means the
	// repository's default branch.
	Base string `json:"base"`
	// Title and Body are the pull request's. Message is the commit's, and
	// defaults to the title.
	Title   string `json:"title"`
	Body    string `json:"body"`
	Message string `json:"message"`
	// Author is who the commit is by. Empty means "weir".
	Author struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	} `json:"author"`
}

// RegisterGitHubCommit adds "github.commit", a deterministic handler that
// turns the files a step changed into a branch and a pull request.
//
// It clones the repository afresh, so nothing an agent left behind is used or
// run, applies the files, commits, pushes a new branch and opens a pull
// request. The token that can write is asked for only now. Running it twice
// is safe: a branch that is already there is not pushed again, and a pull
// request that is already open is returned.
func RegisterGitHubCommit(reg *Registry, eng *engine.Engine, client *http.Client) {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second, CheckRedirect: secrets.SameHost}
	}
	reg.Register("github.commit", func(ctx context.Context, call Call) (json.RawMessage, error) {
		var in commitInput
		if err := json.Unmarshal(call.Input, &in); err != nil {
			return nil, Permanent(fmt.Errorf("the step's input could not be read: %w", err))
		}
		switch {
		case in.From == "":
			return nil, Permanent(errors.New(`say which step's files to commit, as "from"`))
		case !githubBranch.MatchString(in.Branch):
			return nil, Permanent(fmt.Errorf("branch %q is not a branch name", in.Branch))
		case in.Base != "" && !githubBranch.MatchString(in.Base):
			return nil, Permanent(fmt.Errorf("base %q is not a branch name", in.Base))
		case strings.TrimSpace(in.Title) == "":
			return nil, Permanent(errors.New("a pull request needs a title"))
		}
		g, err := githubFor(ctx, reg, eng, client, call.RunID, in.Connection, in.Repo)
		if err != nil {
			return nil, err
		}
		files, err := eng.StepFiles(ctx, call.RunID, in.From)
		if err != nil {
			return nil, err
		}
		if len(files) == 0 {
			// Nothing was changed, so there is nothing to propose.
			return json.Marshal(map[string]any{"changed": false, "reason": "step " + in.From + " changed no files"})
		}
		base := in.Base
		if base == "" {
			if base, err = g.defaultBranch(ctx); err != nil {
				return nil, err
			}
		}
		if in.Branch == base {
			return nil, Permanent(fmt.Errorf("the changes must go on a new branch, not on %s", base))
		}
		out := map[string]any{"changed": true, "branch": in.Branch, "base": base, "files": len(files)}

		// A branch that is already there was pushed by an earlier try at this
		// step. It is left as it is, and only the pull request is seen to.
		status, err := g.do(ctx, http.MethodGet, "/git/ref/heads/"+in.Branch, nil, nil)
		switch {
		case err == nil:
			out["pushed"] = false
		case status == http.StatusNotFound:
			remote, err := reg.remote(g.api, g.repo)
			if err != nil {
				return nil, err
			}
			// The token that can write is asked for here and no earlier:
			// after the agent has exited, for this one repository.
			token, err := reg.gitToken(ctx, client, g.to, g.repo, GitHubCommit)
			if err != nil {
				return nil, err
			}
			message := in.Message
			if strings.TrimSpace(message) == "" {
				message = in.Title
			}
			sha, changed, err := commit(ctx, token, remote, base, in.Branch, message, in.Author.Name, in.Author.Email, files)
			if err != nil {
				return nil, fmt.Errorf("commit to %s: %w", g.repo, err)
			}
			if !changed {
				return json.Marshal(map[string]any{"changed": false, "reason": "the files are the same as on " + base})
			}
			out["pushed"], out["commit"] = true, sha
		default:
			return nil, err
		}

		number, link, opened, err := g.openPull(ctx, in.Branch, base, in.Title, in.Body)
		if err != nil {
			return nil, err
		}
		out["pull_request"] = map[string]any{"number": number, "url": link, "opened": opened}
		return json.Marshal(out)
	})
}

// commit clones remote at base into a new directory, applies files, commits
// them on branch and pushes it. It returns the commit, or changed false when
// the files made no difference.
//
// Git is run only in the clone made here. It never touches a directory an
// agent has been in.
func commit(ctx context.Context, token, remote, base, branch, message, name, email string, files []engine.NewFile) (sha string, changed bool, err error) {
	dir, err := os.MkdirTemp("", "weir-commit-")
	if err != nil {
		return "", false, err
	}
	defer os.RemoveAll(dir)
	if _, err := git(ctx, token, remote, dir, "clone", "--quiet", "--depth", "1", "--no-recurse-submodules", "--branch", base, "--", remote, "."); err != nil {
		return "", false, err
	}
	if _, err := git(ctx, "", remote, dir, "checkout", "--quiet", "-b", branch); err != nil {
		return "", false, err
	}
	for _, f := range files {
		clean, err := runner.CleanPath(f.Path)
		if err != nil {
			return "", false, err
		}
		// A repository's own records are never written from a file an agent
		// produced, whatever rules its step had.
		for _, part := range strings.Split(clean, "/") {
			if part == ".git" {
				return "", false, fmt.Errorf("%s is inside a repository's own records and cannot be committed", clean)
			}
		}
		target := filepath.Join(dir, filepath.FromSlash(clean))
		if f.Deleted {
			// The file itself may be a link, and removing one is harmless.
			// A link on the way to it is not.
			if parent := path.Dir(clean); parent != "." {
				if err := noLinks(dir, parent); err != nil {
					return "", false, err
				}
			}
			if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
				return "", false, err
			}
			continue
		}
		// A link in the way would send the write somewhere else.
		if err := noLinks(dir, clean); err != nil {
			return "", false, err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return "", false, err
		}
		if err := os.WriteFile(target, f.Data, 0o644); err != nil {
			return "", false, err
		}
	}
	if _, err := git(ctx, "", remote, dir, "add", "--all"); err != nil {
		return "", false, err
	}
	status, err := git(ctx, "", remote, dir, "status", "--porcelain")
	if err != nil {
		return "", false, err
	}
	if strings.TrimSpace(status) == "" {
		return "", false, nil
	}
	if name == "" {
		name = "weir"
	}
	if email == "" {
		email = "weir@users.noreply.github.com"
	}
	identity := []string{"-c", "user.name=" + name, "-c", "user.email=" + email, "-c", "commit.gpgsign=false"}
	if _, err := git(ctx, "", remote, dir, append(identity, "commit", "--quiet", "--message", message)...); err != nil {
		return "", false, err
	}
	head, err := git(ctx, "", remote, dir, "rev-parse", "HEAD")
	if err != nil {
		return "", false, err
	}
	if _, err := git(ctx, token, remote, dir, "push", "--quiet", "origin", "HEAD:refs/heads/"+branch); err != nil {
		return "", false, err
	}
	return strings.TrimSpace(head), true, nil
}

// noLinks refuses a path if any directory on the way to it, or the path
// itself, is a link in the clone.
func noLinks(root, clean string) error {
	at := root
	for _, part := range strings.Split(clean, "/") {
		at = filepath.Join(at, part)
		info, err := os.Lstat(at)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%s goes through a link in the repository and cannot be written", clean)
		}
	}
	return nil
}
