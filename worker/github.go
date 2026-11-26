package worker

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/bobbydeveaux/weir/engine"
	"github.com/bobbydeveaux/weir/internal/secrets"
)

// The GitHub tools let an agent read a repository and propose a change to it.
// They go through a named connection, so Weir adds the token, checks each
// call against the step's list, and records it. The agent never sees the token.
//
// Two rules keep an agent that has been talked into something from doing harm:
//
//   - A connection can be pinned to one repository, by giving it the address
//     https://api.github.com/repos/<owner>/<name>. The tools then work on that
//     repository only, and take no repository argument.
//   - A file cannot be written to the repository's default branch. The way in
//     is a branch and a pull request, which a person or a check can refuse.

const (
	// maxGitHubFile is the largest file an agent is given.
	maxGitHubFile = 200 << 10
	// maxGitHubPatch is how much of one file's changes is given.
	maxGitHubPatch = 20 << 10
	// maxGitHubFiles is how many changed files of a pull request are listed.
	maxGitHubFiles = 100
)

var (
	githubRepo   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*/[A-Za-z0-9._-]+$`)
	githubBranch = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)
	githubPinned = regexp.MustCompile(`^(.*)/repos/([A-Za-z0-9][A-Za-z0-9-]*/[A-Za-z0-9._-]+)$`)
)

// github makes calls to GitHub's API for one tool call.
type github struct {
	reg    *Registry
	client *http.Client
	to     engine.Endpoint
	// api is the API's base address, and repo is "owner/name".
	api, repo string
	// needs, when set, is the permission every call asks for, in place of
	// what the call's method and path would say.
	needs map[string]string
}

// githubFor finds the connection and the repository for a tool call.
func githubFor(ctx context.Context, reg *Registry, eng *engine.Engine, client *http.Client, runID, connection, repo string) (*github, error) {
	if connection == "" {
		connection = "github"
	}
	to, err := eng.Endpoint(ctx, runID, connection)
	if err != nil {
		return nil, err
	}
	g := &github{reg: reg, client: client, to: to, api: strings.TrimRight(to.URL, "/"), repo: repo}
	if m := githubPinned.FindStringSubmatch(g.api); m != nil {
		// The connection is for one repository. Naming another is refused,
		// not quietly corrected.
		if repo != "" && !strings.EqualFold(repo, m[2]) {
			return nil, fmt.Errorf("connection %s is for the repository %s and cannot be used on %s", connection, m[2], repo)
		}
		g.api, g.repo = m[1], m[2]
		return g, nil
	}
	if !githubRepo.MatchString(repo) {
		return nil, fmt.Errorf(`say which repository, as "owner/name"; connection %s is not pinned to one`, connection)
	}
	return g, nil
}

// githubNeeds says what a call to a repository needs permission for. Pull
// requests and their comments are one permission; everything else under a
// repository is its contents.
func githubNeeds(method, path string) map[string]string {
	pulls := strings.HasPrefix(path, "/pulls") || strings.HasPrefix(path, "/issues")
	read := method == http.MethodGet || method == http.MethodHead
	switch {
	case pulls && read:
		return GitHubReadPulls
	case pulls:
		return GitHubWritePulls
	case read:
		return GitHubRead
	}
	return GitHubWrite
}

// do calls the API. out, when not nil, receives the decoded reply. It returns
// the status code, and an error for anything that is not a success.
func (g *github) do(ctx context.Context, method, path string, body, out any) (int, error) {
	var payload io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}
		payload = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, g.api+"/repos/"+g.repo+path, payload)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "weir")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if g.to.Auth.Kind == secrets.GitHubApp {
		// A token for this one repository, with the least this call needs.
		needs := g.needs
		if needs == nil {
			needs = githubNeeds(method, path)
		}
		token, err := g.reg.GitHubToken(ctx, g.client, g.to, g.repo, needs)
		if err != nil {
			return 0, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
	} else if err := g.reg.sign(req, g.to); err != nil {
		return 0, err
	}
	resp, err := g.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("GitHub could not be reached: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode >= 300 {
		var e struct {
			Message string            `json:"message"`
			Errors  []json.RawMessage `json:"errors"`
		}
		_ = json.Unmarshal(raw, &e)
		if e.Message == "" {
			e.Message = http.StatusText(resp.StatusCode)
		}
		// "Validation Failed" says nothing on its own; the reasons are beside it.
		for _, reason := range e.Errors {
			var detail struct {
				Message, Field, Code string
			}
			if json.Unmarshal(reason, &detail) != nil || (detail.Message == "" && detail.Field == "") {
				var text string
				_ = json.Unmarshal(reason, &text)
				detail.Message = text
			}
			e.Message += "; " + strings.TrimSpace(detail.Field+" "+detail.Code+" "+detail.Message)
		}
		return resp.StatusCode, fmt.Errorf("GitHub said %d: %s", resp.StatusCode, e.Message)
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return resp.StatusCode, fmt.Errorf("GitHub's reply was not understood: %w", err)
		}
	}
	return resp.StatusCode, nil
}

// contentPath escapes a file path for the API, keeping its slashes.
func contentPath(path string) (string, error) {
	path = strings.Trim(path, "/")
	for _, part := range strings.Split(path, "/") {
		if part == ".." || part == "." {
			return "", errors.New("the path must not contain . or .. parts")
		}
	}
	parts := strings.Split(path, "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	return strings.Join(parts, "/"), nil
}

// openPull opens a pull request from head into base. If one is already open
// from that branch it is returned instead, with opened false, so that asking
// twice is safe.
func (g *github) openPull(ctx context.Context, head, base, title, body string) (number int, link string, opened bool, err error) {
	type pull struct {
		Number int    `json:"number"`
		URL    string `json:"html_url"`
	}
	var pr pull
	status, err := g.do(ctx, http.MethodPost, "/pulls", map[string]string{"head": head, "base": base, "title": title, "body": body}, &pr)
	if status == http.StatusUnprocessableEntity {
		// Most often: one is already open from this branch. Find it.
		owner, _, _ := strings.Cut(g.repo, "/")
		var open []pull
		q := "/pulls?state=open&head=" + url.QueryEscape(owner+":"+head)
		if _, findErr := g.do(ctx, http.MethodGet, q, nil, &open); findErr == nil && len(open) > 0 {
			return open[0].Number, open[0].URL, false, nil
		}
	}
	if err != nil {
		return 0, "", false, err
	}
	return pr.Number, pr.URL, true, nil
}

func (g *github) defaultBranch(ctx context.Context) (string, error) {
	var repo struct {
		DefaultBranch string `json:"default_branch"`
	}
	if _, err := g.do(ctx, http.MethodGet, "", nil, &repo); err != nil {
		return "", err
	}
	if repo.DefaultBranch == "" {
		return "", errors.New("GitHub did not say what the repository's default branch is")
	}
	return repo.DefaultBranch, nil
}

// RegisterGitHubTools adds the GitHub tools to the registry. A step lists the
// ones it may use; the read ones under read, the others under write.
func RegisterGitHubTools(reg *Registry, eng *engine.Engine, client *http.Client) {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second, CheckRedirect: secrets.SameHost}
	}
	str := func(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
	common := map[string]any{
		"connection": str(`The connection to use. Defaults to "github".`),
		"repo":       str(`The repository, as "owner/name". Leave out if the connection is for one repository.`),
	}
	schema := func(required []string, props map[string]any) map[string]any {
		for k, v := range common {
			props[k] = v
		}
		return map[string]any{"type": "object", "required": required, "properties": props, "additionalProperties": false}
	}
	type base struct {
		Connection string `json:"connection"`
		Repo       string `json:"repo"`
	}
	tool := func(name, class, desc string, input map[string]any, run func(ctx context.Context, g *github, raw json.RawMessage) (any, error)) {
		reg.RegisterTool(name, Tool{Class: class, Description: desc, InputSchema: input,
			Handler: func(ctx context.Context, c Call) (json.RawMessage, error) {
				var b base
				if err := json.Unmarshal(c.Input, &b); err != nil {
					return nil, fmt.Errorf("arguments: %w", err)
				}
				g, err := githubFor(ctx, reg, eng, client, c.RunID, b.Connection, b.Repo)
				if err != nil {
					return nil, err
				}
				out, err := run(ctx, g, c.Input)
				if err != nil {
					return nil, err
				}
				return json.Marshal(out)
			}})
	}

	tool("github.get_file", engine.ToolRead, "Read one file from a repository. Returns its text.",
		schema([]string{"path"}, map[string]any{
			"path": str("The file's path in the repository."),
			"ref":  str("A branch, tag or commit. Defaults to the default branch."),
		}),
		func(ctx context.Context, g *github, raw json.RawMessage) (any, error) {
			var in struct{ Path, Ref string }
			_ = json.Unmarshal(raw, &in)
			path, err := contentPath(in.Path)
			if err != nil {
				return nil, err
			}
			if path == "" {
				return nil, errors.New("say which file to read")
			}
			text, sha, err := g.file(ctx, path, in.Ref)
			if err != nil {
				return nil, err
			}
			if sha == "" {
				return nil, fmt.Errorf("%s is not in the repository", in.Path)
			}
			return map[string]any{"path": in.Path, "content": text}, nil
		})

	tool("github.list_files", engine.ToolRead, "List the files and directories in one directory of a repository.",
		schema(nil, map[string]any{
			"path": str("The directory's path. Leave out for the top of the repository."),
			"ref":  str("A branch, tag or commit. Defaults to the default branch."),
		}),
		func(ctx context.Context, g *github, raw json.RawMessage) (any, error) {
			var in struct{ Path, Ref string }
			_ = json.Unmarshal(raw, &in)
			path, err := contentPath(in.Path)
			if err != nil {
				return nil, err
			}
			var entries []struct {
				Name string `json:"name"`
				Path string `json:"path"`
				Type string `json:"type"`
				Size int64  `json:"size"`
			}
			if _, err := g.do(ctx, http.MethodGet, "/contents/"+path+refQuery(in.Ref), nil, &entries); err != nil {
				return nil, err
			}
			return map[string]any{"entries": entries}, nil
		})

	tool("github.get_pull_request", engine.ToolRead, "Read a pull request: its title, description, branches and the files it changes.",
		schema([]string{"number"}, map[string]any{"number": map[string]any{"type": "integer", "description": "The pull request's number."}}),
		func(ctx context.Context, g *github, raw json.RawMessage) (any, error) {
			var in struct{ Number int }
			_ = json.Unmarshal(raw, &in)
			if in.Number <= 0 {
				return nil, errors.New("say which pull request, by its number")
			}
			var pr struct {
				Number int    `json:"number"`
				Title  string `json:"title"`
				Body   string `json:"body"`
				State  string `json:"state"`
				Draft  bool   `json:"draft"`
				User   struct {
					Login string `json:"login"`
				} `json:"user"`
				Head struct {
					Ref string `json:"ref"`
					SHA string `json:"sha"`
				} `json:"head"`
				Base struct {
					Ref string `json:"ref"`
				} `json:"base"`
			}
			if _, err := g.do(ctx, http.MethodGet, fmt.Sprintf("/pulls/%d", in.Number), nil, &pr); err != nil {
				return nil, err
			}
			var files []struct {
				Filename  string `json:"filename"`
				Status    string `json:"status"`
				Additions int    `json:"additions"`
				Deletions int    `json:"deletions"`
				Patch     string `json:"patch"`
			}
			if _, err := g.do(ctx, http.MethodGet, fmt.Sprintf("/pulls/%d/files?per_page=%d", in.Number, maxGitHubFiles), nil, &files); err != nil {
				return nil, err
			}
			for i := range files {
				if len(files[i].Patch) > maxGitHubPatch {
					files[i].Patch = files[i].Patch[:maxGitHubPatch] + "\n… cut short"
				}
			}
			return map[string]any{"number": pr.Number, "title": pr.Title, "description": pr.Body, "state": pr.State, "draft": pr.Draft,
				"author": pr.User.Login, "head": pr.Head.Ref, "head_sha": pr.Head.SHA, "base": pr.Base.Ref, "files": files}, nil
		})

	tool("github.create_branch", engine.ToolWrite, "Create a branch. If it already exists, that is not an error.",
		schema([]string{"branch"}, map[string]any{
			"branch": str("The new branch's name."),
			"from":   str("The branch to start from. Defaults to the default branch."),
		}),
		func(ctx context.Context, g *github, raw json.RawMessage) (any, error) {
			var in struct{ Branch, From string }
			_ = json.Unmarshal(raw, &in)
			if !githubBranch.MatchString(in.Branch) || strings.Contains(in.Branch, "..") {
				return nil, errors.New("the branch name must be letters, digits, dots, hyphens, underscores and slashes")
			}
			from := in.From
			if from == "" {
				var err error
				if from, err = g.defaultBranch(ctx); err != nil {
					return nil, err
				}
			}
			var ref struct {
				Object struct {
					SHA string `json:"sha"`
				} `json:"object"`
			}
			if _, err := g.do(ctx, http.MethodGet, "/git/ref/heads/"+from, nil, &ref); err != nil {
				return nil, fmt.Errorf("the branch %s to start from: %w", from, err)
			}
			status, err := g.do(ctx, http.MethodPost, "/git/refs", map[string]string{"ref": "refs/heads/" + in.Branch, "sha": ref.Object.SHA}, nil)
			if status == http.StatusUnprocessableEntity {
				// GitHub answers 422 when the reference exists. A retry of this
				// step must not fail for having succeeded the first time.
				return map[string]any{"branch": in.Branch, "created": false, "from": from}, nil
			}
			if err != nil {
				return nil, err
			}
			return map[string]any{"branch": in.Branch, "created": true, "from": from}, nil
		})

	tool("github.put_file", engine.ToolWrite,
		"Create or replace one file on a branch, as a commit. It cannot write to the default branch: write to a branch and open a pull request.",
		schema([]string{"branch", "path", "content", "message"}, map[string]any{
			"branch":  str("The branch to commit to. Not the default branch."),
			"path":    str("The file's path in the repository."),
			"content": str("The file's whole new content, as text."),
			"message": str("The commit message."),
		}),
		func(ctx context.Context, g *github, raw json.RawMessage) (any, error) {
			var in struct{ Branch, Path, Content, Message string }
			_ = json.Unmarshal(raw, &in)
			path, err := contentPath(in.Path)
			if err != nil {
				return nil, err
			}
			if path == "" {
				return nil, errors.New("say which file to write")
			}
			if in.Branch == "" || strings.TrimSpace(in.Message) == "" {
				return nil, errors.New("a branch and a commit message are both needed")
			}
			main, err := g.defaultBranch(ctx)
			if err != nil {
				return nil, err
			}
			if in.Branch == main {
				return nil, fmt.Errorf("%s is the repository's default branch and cannot be written to: create a branch, write there, and open a pull request", main)
			}
			current, sha, err := g.file(ctx, path, in.Branch)
			if err != nil {
				return nil, err
			}
			if sha != "" && current == in.Content {
				return map[string]any{"path": in.Path, "branch": in.Branch, "changed": false}, nil // a retry, or nothing to do
			}
			body := map[string]string{"message": in.Message, "branch": in.Branch, "content": base64.StdEncoding.EncodeToString([]byte(in.Content))}
			if sha != "" {
				body["sha"] = sha
			}
			var done struct {
				Commit struct {
					SHA string `json:"sha"`
				} `json:"commit"`
			}
			if _, err := g.do(ctx, http.MethodPut, "/contents/"+path, body, &done); err != nil {
				return nil, err
			}
			return map[string]any{"path": in.Path, "branch": in.Branch, "changed": true, "commit": done.Commit.SHA}, nil
		})

	tool("github.open_pull_request", engine.ToolWrite, "Open a pull request from a branch. If one is already open from that branch, it is returned.",
		schema([]string{"head", "title"}, map[string]any{
			"head":  str("The branch with the changes."),
			"base":  str("The branch to merge into. Defaults to the default branch."),
			"title": str("The pull request's title."),
			"body":  str("The pull request's description."),
		}),
		func(ctx context.Context, g *github, raw json.RawMessage) (any, error) {
			var in struct{ Head, Base, Title, Body string }
			_ = json.Unmarshal(raw, &in)
			if in.Head == "" || strings.TrimSpace(in.Title) == "" {
				return nil, errors.New("a branch and a title are both needed")
			}
			base := in.Base
			if base == "" {
				var err error
				if base, err = g.defaultBranch(ctx); err != nil {
					return nil, err
				}
			}
			number, link, opened, err := g.openPull(ctx, in.Head, base, in.Title, in.Body)
			if err != nil {
				return nil, err
			}
			return map[string]any{"number": number, "url": link, "opened": opened}, nil
		})

	tool("github.comment", engine.ToolWrite, "Add a comment to an issue or a pull request.",
		schema([]string{"number", "body"}, map[string]any{
			"number": map[string]any{"type": "integer", "description": "The issue's or pull request's number."},
			"body":   str("The comment, in Markdown."),
		}),
		func(ctx context.Context, g *github, raw json.RawMessage) (any, error) {
			var in struct {
				Number int
				Body   string
			}
			_ = json.Unmarshal(raw, &in)
			if in.Number <= 0 || strings.TrimSpace(in.Body) == "" {
				return nil, errors.New("a number and the comment's text are both needed")
			}
			var done struct {
				URL string `json:"html_url"`
			}
			if _, err := g.do(ctx, http.MethodPost, fmt.Sprintf("/issues/%d/comments", in.Number), map[string]string{"body": in.Body}, &done); err != nil {
				return nil, err
			}
			return map[string]any{"url": done.URL}, nil
		})
}

func refQuery(ref string) string {
	if ref == "" {
		return ""
	}
	return "?ref=" + url.QueryEscape(ref)
}

// file reads one file. It returns its text and its blob id; the id is empty
// when the file does not exist on that branch.
func (g *github) file(ctx context.Context, path, ref string) (text, sha string, err error) {
	var f struct {
		Type     string `json:"type"`
		Encoding string `json:"encoding"`
		Content  string `json:"content"`
		SHA      string `json:"sha"`
		Size     int64  `json:"size"`
	}
	status, err := g.do(ctx, http.MethodGet, "/contents/"+path+refQuery(ref), nil, &f)
	if status == http.StatusNotFound {
		return "", "", nil
	}
	if err != nil {
		// A directory comes back as a list, which does not decode as a file.
		return "", "", fmt.Errorf("%s: %w", path, err)
	}
	if f.Type != "file" {
		return "", "", fmt.Errorf("%s is a %s, not a file", path, f.Type)
	}
	if f.Size > maxGitHubFile {
		return "", "", fmt.Errorf("%s is %d KB, more than the %d KB an agent is given", path, f.Size>>10, maxGitHubFile>>10)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(f.Content, "\n", ""))
	if err != nil || f.Encoding != "base64" {
		return "", "", fmt.Errorf("%s could not be decoded", path)
	}
	if !utf8.Valid(raw) {
		return "", "", fmt.Errorf("%s is not a text file", path)
	}
	return string(raw), f.SHA, nil
}
