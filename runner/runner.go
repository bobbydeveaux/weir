// Package runner runs decide and ai steps. A runner hides how a model is
// reached: a command line, an API, or scripted answers for tests. The engine
// and the audit record are the same whichever runner is used.
package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	// BudgetUSD is the most this call may spend. Zero means no limit.
	BudgetUSD float64
	// ToolsRead and ToolsWrite are the tools the agent may call.
	ToolsRead  []string
	ToolsWrite []string
}

// RunResult is what an agent produced.
type RunResult struct {
	Output     json.RawMessage
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
