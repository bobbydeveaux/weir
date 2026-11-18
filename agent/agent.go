// Package agent is the contract between Weir and an agent that runs in a
// container of its own, and the program that runs there.
//
// The container talks to one thing: Weir's API, with the one-time token of
// its own attempt. It fetches its job and the directory it starts from, runs
// the agent with every tool, and sends back one result. It holds no Weir
// secret, no database address and no third party's credential.
package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bobbydeveaux/weir/internal/archive"
	"github.com/bobbydeveaux/weir/runner"
)

// The kinds of archive a job can come with.
const (
	// Files are documents for the agent to read.
	Files = "files"
	// Workspace is a directory for the agent to change in place.
	Workspace = "workspace"
)

// MaxArchive is the largest working directory, packed, that is sent to an agent.
const MaxArchive = 30 << 20

// MaxResult is the largest result an agent may send back.
const MaxResult = 30 << 20

// Job is what an agent is asked to do.
type Job struct {
	Step   string          `json:"step"`
	System string          `json:"system"`
	Prompt string          `json:"prompt"`
	Schema json.RawMessage `json:"schema,omitempty"`
	// Model is the model to use, already chosen by Weir.
	Model string `json:"model,omitempty"`
	// Effort is how hard the model is asked to think. Empty is its default.
	Effort    string        `json:"effort,omitempty"`
	BudgetUSD float64       `json:"budget_usd,omitempty"`
	Tools     []runner.Tool `json:"tools,omitempty"`
	// Writes are the limits on what the agent may write or change.
	Writes *runner.WriteLimit `json:"writes,omitempty"`
	// Archive says what the archive holds: "", Files or Workspace.
	Archive string `json:"archive,omitempty"`
	// Ignore lists paths in a workspace that are never kept, as a .gitignore does.
	Ignore []string `json:"ignore,omitempty"`
	// Discard says that nothing changed in a workspace is kept.
	Discard bool `json:"discard,omitempty"`
}

// Result is what an agent sends back.
type Result struct {
	Output     json.RawMessage `json:"output,omitempty"`
	Transcript string          `json:"transcript,omitempty"`
	Usage      runner.Usage    `json:"usage"`
	// Error is why the agent failed, when it did.
	Error string `json:"error,omitempty"`
	Files []File `json:"files,omitempty"`
}

// File is one file an agent wrote, changed or deleted.
type File struct {
	Path    string `json:"path"`
	Deleted bool   `json:"deleted,omitempty"`
	Data    []byte `json:"data,omitempty"`
}

// Client talks to Weir on an agent's behalf.
type Client struct {
	// BaseURL is Weir's address, and Token the one-time token of the attempt.
	BaseURL, Token string
	HTTP           *http.Client
}

func (c *Client) do(ctx context.Context, method, part string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.BaseURL, "/")+"/v1/agent/"+c.Token+"/"+part, body)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Minute}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Weir could not be reached at %s: %w", c.BaseURL, err)
	}
	if resp.StatusCode >= 300 {
		defer resp.Body.Close()
		var e struct {
			Error string `json:"error"`
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		_ = json.Unmarshal(raw, &e)
		if e.Error == "" {
			e.Error = http.StatusText(resp.StatusCode)
		}
		return nil, fmt.Errorf("Weir said %d: %s", resp.StatusCode, e.Error)
	}
	return resp, nil
}

// unpack fetches the job's archive into dir.
func (c *Client) unpack(ctx context.Context, dir string) error {
	resp, err := c.do(ctx, http.MethodGet, "workspace", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return archive.Unpack(io.LimitReader(resp.Body, MaxArchive+1), dir)
}

// Run does one job: it fetches it, runs the agent with r, and sends the
// result. An agent that fails is still a result that was sent, so Run returns
// an error only when it could not fetch the job or send the result.
func Run(ctx context.Context, c *Client, r runner.Runner) error {
	resp, err := c.do(ctx, http.MethodGet, "job", nil)
	if err != nil {
		return err
	}
	var job Job
	err = json.NewDecoder(resp.Body).Decode(&job)
	resp.Body.Close()
	if err != nil {
		return fmt.Errorf("the job could not be read: %w", err)
	}

	req := runner.RunRequest{Step: job.Step, System: job.System, Prompt: job.Prompt, Schema: job.Schema, Model: job.Model, Effort: job.Effort,
		BudgetUSD: job.BudgetUSD, Tools: job.Tools, Writes: job.Writes}
	if len(job.Tools) > 0 {
		req.ToolURL, req.ToolToken = strings.TrimRight(c.BaseURL, "/"), c.Token
	}
	var result Result
	switch job.Archive {
	case Workspace:
		req.Workspace = &runner.Workspace{Discard: job.Discard, Prepare: func(ctx context.Context, dir string) (runner.Prepared, error) {
			return runner.Prepared{Ignore: job.Ignore}, c.unpack(ctx, dir)
		}}
	case Files:
		if req.Files, err = c.files(ctx); err != nil {
			result.Error = err.Error()
		}
	}
	if result.Error == "" {
		res, err := r.Run(ctx, req)
		result.Output, result.Transcript, result.Usage = res.Output, res.Transcript, res.Usage
		if err != nil {
			result.Error = err.Error()
		}
		for _, f := range res.Files {
			result.Files = append(result.Files, File{Path: f.Name, Deleted: f.Deleted, Data: f.Data})
		}
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return err
	}
	if len(raw) > MaxResult {
		// Too much to send. Say so, rather than leave Weir with nothing.
		result.Files, result.Output = nil, nil
		result.Error = fmt.Sprintf("the agent's result is %d MB, more than the %d MB that can be sent back", len(raw)>>20, MaxResult>>20)
		raw, _ = json.Marshal(result)
	}
	// The step's own time may be up; the result is still worth sending.
	send, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
	defer cancel()
	sent, err := c.do(send, http.MethodPost, "result", bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("the result could not be sent: %w", err)
	}
	sent.Body.Close()
	return nil
}

// files fetches the documents a job comes with.
func (c *Client) files(ctx context.Context) ([]runner.File, error) {
	dir, err := os.MkdirTemp("", "weir-given-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	if err := c.unpack(ctx, dir); err != nil {
		return nil, err
	}
	var out []runner.File
	err = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		out = append(out, runner.File{Name: filepath.ToSlash(rel), Data: data})
		return nil
	})
	if err != nil {
		return nil, errors.Join(errors.New("the files for this step could not be read"), err)
	}
	return out, nil
}
