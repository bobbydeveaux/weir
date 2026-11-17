// Package runner runs decide and ai steps. A runner hides how a model is
// reached: a command line, an API, or scripted answers for tests. The engine
// and the audit record are the same whichever runner is used.
package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Usage is what a model call consumed.
type Usage struct {
	Provider  string
	Model     string
	TokensIn  int64
	TokensOut int64
	CostUSD   float64
	// CostEstimated is true when the provider did not report a cost and it was
	// worked out from tokens, or when the figure is not an amount charged.
	CostEstimated bool
	Duration      time.Duration
}

// DecideRequest asks for one label from a closed list.
type DecideRequest struct {
	// Step is the id of the step, for logs and for scripted runners.
	Step string
	// System and Prompt are the rendered template.
	System string
	Prompt string
	// Labels are the only acceptable answers.
	Labels []string
	// Tier names the model size to use. Empty means the runner's default.
	Tier string
	// Describe says what each label means, by label. It may be empty.
	Describe map[string]string
	// Provider names what should answer, as the workflow gave it. Empty
	// means the server's default.
	Provider string
}

// DecideResult is a runner's answer to a DecideRequest.
type DecideResult struct {
	Label string
	// Confidence is between 0 and 1.
	Confidence float64
	Usage      Usage
}

// RunRequest asks an agent to do a piece of work and return structured output.
type RunRequest struct {
	Step   string
	System string
	Prompt string
	// Schema is the JSON Schema the output must match.
	Schema json.RawMessage
	Tier   string
	// Model, when set, is the model the step's template asks for. It is used
	// when the step names no tier.
	Model string
	// BudgetUSD is the most this call may spend. Zero means no limit.
	BudgetUSD float64
	// Tools are the tools the agent may call. The agent reaches them only
	// through Weir, at ToolURL, presenting ToolToken.
	Tools     []Tool
	ToolURL   string
	ToolToken string
	// Files are documents the agent may read. They are the only files it can see.
	Files []File
	// Writes, when set, lets the agent produce files, up to these limits.
	Writes *WriteLimit
	// Shell, when set, gives the agent a sandboxed shell.
	Shell *Shell
	// Workspace, when set, prepares the agent's working directory before it
	// runs. The agent may change anything in it, and the result's files are
	// what it changed, within the limits in Writes.
	Workspace *Workspace
}

// Shell says what an agent's shell may reach.
type Shell struct {
	// Hosts are the only hosts it may connect to. None means no network.
	Hosts []string
}

// WriteLimit bounds what an agent may write.
type WriteLimit struct {
	MaxFiles int
	MaxBytes int64
}

// File is a document given to an agent.
type File struct {
	// Name is the file's path, with forward slashes and no leading slash.
	// A file fetched for an agent has a plain name with no directory.
	Name string
	Data []byte
	// Deleted says the agent removed this file from a prepared directory.
	Deleted bool
}

// Tool describes one tool an agent may call.
type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Class       string         `json:"class"`
	InputSchema map[string]any `json:"input_schema"`
}

// RunResult is what an agent produced.
type RunResult struct {
	Output json.RawMessage
	// Files are the files the agent wrote, when it was allowed to.
	Files      []File
	Transcript string
	Usage      Usage
}

// Runner runs decide and ai steps.
type Runner interface {
	// Name identifies the runner in the audit record: cli, api or fake.
	Name() string
	Decide(ctx context.Context, req DecideRequest) (DecideResult, error)
	Run(ctx context.Context, req RunRequest) (RunResult, error)
}

// Fake is a runner that returns scripted answers, keyed by step id. It is for
// tests, the conformance suite and the examples, and it costs nothing to run
// unless a cost is scripted.
type Fake struct {
	mu sync.Mutex
	// Decisions is the answer to give for each decide step.
	Decisions map[string]DecideResult
	// Outputs is the queue of outputs for each ai step. Each call takes the
	// next one; the last is repeated once the queue is used up.
	Outputs map[string][]json.RawMessage
	// Errors makes a step's call fail.
	Errors map[string]error
	// CostUSD is charged for every ai call.
	CostUSD float64
	// Calls records the step id of every call made, in order.
	Calls []string
}

// Name implements Runner.
func (f *Fake) Name() string { return "fake" }

// Decide implements Runner.
func (f *Fake) Decide(_ context.Context, req DecideRequest) (DecideResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, req.Step)
	if err := f.Errors[req.Step]; err != nil {
		return DecideResult{}, err
	}
	res, ok := f.Decisions[req.Step]
	if !ok {
		return DecideResult{}, fmt.Errorf("fake runner: no decision scripted for step %q", req.Step)
	}
	res.Usage = Usage{Provider: "fake", Model: "fake-small"}
	return res, nil
}

// Run implements Runner.
func (f *Fake) Run(_ context.Context, req RunRequest) (RunResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, req.Step)
	usage := Usage{Provider: "fake", Model: "fake-large", CostUSD: f.CostUSD, TokensIn: 100, TokensOut: 50}
	if err := f.Errors[req.Step]; err != nil {
		return RunResult{Usage: usage}, err
	}
	queue := f.Outputs[req.Step]
	if len(queue) == 0 {
		return RunResult{Usage: usage}, fmt.Errorf("fake runner: no output scripted for step %q", req.Step)
	}
	out := queue[0]
	if len(queue) > 1 {
		f.Outputs[req.Step] = queue[1:]
	}
	return RunResult{Output: out, Transcript: "scripted", Usage: usage}, nil
}

// ErrNotBuilt is returned by a runner that is selected but not implemented yet.
var ErrNotBuilt = errors.New("this runner is not built yet")

// describeLabels turns label descriptions into lines for a language model's
// instructions. It returns "" when there are none.
func describeLabels(req DecideRequest) string {
	var b strings.Builder
	for _, label := range req.Labels {
		if d := req.Describe[label]; d != "" {
			fmt.Fprintf(&b, "\n- %s: %s", label, d)
		}
	}
	if b.Len() == 0 {
		return ""
	}
	return "\n\nThe labels mean:" + b.String()
}
