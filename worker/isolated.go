package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/bobbydeveaux/weir/agent"
	"github.com/bobbydeveaux/weir/engine"
	"github.com/bobbydeveaux/weir/internal/archive"
	"github.com/bobbydeveaux/weir/runner"
)

// Launcher starts an agent in a container of its own and waits for it to end.
// The container is given Weir's address and the one-time token of one
// attempt, and must be given nothing else of Weir's.
type Launcher interface {
	// Launch returns when the container has ended. An error means it could
	// not be started or ended badly; whether the agent did its work is
	// decided by the result it sent, not by this.
	Launch(ctx context.Context, start AgentStart) error
	// Describe says where agents run, for the server's start-up message.
	Describe() string
}

// AgentStart is what a container needs to do one job.
type AgentStart struct {
	// URL is Weir's address as the container reaches it.
	URL string
	// Token is the one-time token of the attempt.
	Token string
}

// isolated runs an ai step's agent through the launcher: it prepares the
// directory the agent starts from, records the job, starts the container,
// and reads back the result. Everything the result holds is treated as the
// agent's word, and checked.
func (w *Worker) isolated(ctx context.Context, work *engine.Work, req runner.RunRequest) (runner.RunResult, error) {
	job := agent.Job{Step: req.Step, System: req.System, Prompt: req.Prompt, Schema: req.Schema, BudgetUSD: req.BudgetUSD,
		Tools: req.Tools, Writes: req.Writes, Model: req.Model, Effort: req.Effort}
	if w.Model != nil {
		job.Model = w.Model(req.Tier, req.Model)
	}
	var packed *bytes.Buffer
	if req.Workspace != nil || len(req.Files) > 0 {
		dir, err := os.MkdirTemp("", "weir-agent-")
		if err != nil {
			return runner.RunResult{}, err
		}
		defer os.RemoveAll(dir)
		if req.Workspace != nil {
			// The directory is prepared here, by trusted code with whatever
			// credential that takes. The agent's container gets the files
			// and never the credential.
			prepared, err := req.Workspace.Prepare(ctx, dir)
			if err != nil {
				return runner.RunResult{}, fmt.Errorf("prepare the working directory: %w", err)
			}
			job.Archive, job.Ignore, job.Discard = agent.Workspace, prepared.Ignore, req.Workspace.Discard
		} else {
			job.Archive = agent.Files
			for _, f := range req.Files {
				name, err := runner.CleanPath(f.Name)
				if err != nil {
					return runner.RunResult{}, err
				}
				target := filepath.Join(dir, filepath.FromSlash(name))
				if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
					return runner.RunResult{}, err
				}
				if err := os.WriteFile(target, f.Data, 0o600); err != nil {
					return runner.RunResult{}, err
				}
			}
		}
		packed = &bytes.Buffer{}
		if err := archive.Pack(dir, packed, agent.MaxArchive); err != nil {
			if errors.Is(err, archive.ErrTooLarge) {
				return runner.RunResult{}, fmt.Errorf("the working directory packs to more than %d MB, the most that can be sent to an agent", agent.MaxArchive>>20)
			}
			return runner.RunResult{}, err
		}
	}
	raw, err := json.Marshal(job)
	if err != nil {
		return runner.RunResult{}, err
	}
	// A typed nil would not be nil as an io.Reader.
	if packed == nil {
		err = w.Engine.PutAgentJob(ctx, work.Token, raw, nil)
	} else {
		err = w.Engine.PutAgentJob(ctx, work.Token, raw, packed)
	}
	if err != nil {
		return runner.RunResult{}, err
	}

	launchErr := w.Launcher.Launch(ctx, AgentStart{URL: strings.TrimRight(w.agentURL(), "/"), Token: work.Token})
	sent, found, err := w.Engine.TakeAgentResult(context.WithoutCancel(ctx), work.Token)
	if err != nil {
		return runner.RunResult{}, err
	}
	if !found {
		if launchErr != nil {
			return runner.RunResult{}, fmt.Errorf("the agent's container ended without sending a result: %w", launchErr)
		}
		return runner.RunResult{}, errors.New("the agent's container ended without sending a result")
	}
	var result agent.Result
	if err := json.Unmarshal(sent, &result); err != nil {
		return runner.RunResult{}, fmt.Errorf("the agent's result could not be read: %w", err)
	}
	res := runner.RunResult{Output: result.Output, Transcript: result.Transcript, Usage: result.Usage}
	if result.Error != "" {
		return res, errors.New(result.Error)
	}
	// The limits were applied in the container, by a program the agent could
	// have interfered with. Apply them again here.
	var total int64
	for _, f := range result.Files {
		name, err := runner.CleanPath(f.Path)
		if err != nil {
			return res, err
		}
		total += int64(len(f.Data))
		res.Files = append(res.Files, runner.File{Name: name, Data: f.Data, Deleted: f.Deleted})
	}
	if limit := req.Writes; limit != nil {
		if limit.MaxFiles > 0 && len(res.Files) > limit.MaxFiles {
			return res, fmt.Errorf("the agent sent back more than %d files", limit.MaxFiles)
		}
		if limit.MaxBytes > 0 && total > limit.MaxBytes {
			return res, fmt.Errorf("the agent sent back more than %d MB of files", limit.MaxBytes>>20)
		}
	} else if len(res.Files) > 0 {
		return res, errors.New("the agent sent back files, and its step does not let it write any")
	}
	if len(res.Output) == 0 {
		return res, errors.New("the agent sent back no output")
	}
	return res, nil
}

// agentURL is Weir's address as an agent's container reaches it.
func (w *Worker) agentURL() string {
	if w.AgentURL != "" {
		return w.AgentURL
	}
	return w.toolURL()
}

// Docker starts an agent's container with Docker on this machine. It is how
// the isolated path is run on a laptop, and proven.
type Docker struct {
	// Image holds the weir program and the agent's command line.
	Image string
	// Env names the variables passed to the container: the model's
	// credential, and anything that points the command line at a cloud. They
	// are passed by name, so no value is in a process listing.
	Env []string
	// Args are extra arguments for "docker run", such as a network or limits.
	Args []string
	// Command is the docker program. Empty means "docker".
	Command string
}

// Describe implements Launcher.
func (d *Docker) Describe() string { return "a Docker container each, from the image " + d.Image }

// Launch implements Launcher.
func (d *Docker) Launch(ctx context.Context, start AgentStart) error {
	command := d.Command
	if command == "" {
		command = "docker"
	}
	args := []string{"run", "--rm", "--init",
		"--add-host", "host.docker.internal:host-gateway", // so a container can reach a Weir on this machine
		"--env", "WEIR_URL", "--env", "WEIR_AGENT_TOKEN"}
	env := append(os.Environ(), "WEIR_URL="+start.URL, "WEIR_AGENT_TOKEN="+start.Token)
	for _, name := range d.Env {
		if _, ok := os.LookupEnv(name); ok {
			args = append(args, "--env", name)
		}
	}
	args = append(append(args, d.Args...), d.Image, "weir", "agent")
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Env = env
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		text := strings.ReplaceAll(strings.TrimSpace(out.String()), start.Token, "[token]")
		if len(text) > 600 {
			text = text[len(text)-600:]
		}
		if errors.Is(err, exec.ErrNotFound) {
			return errors.New("docker is not installed on this worker")
		}
		return fmt.Errorf("%w: %s", err, text)
	}
	return nil
}
