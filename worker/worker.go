// Package worker claims steps from the engine and does the work: it calls
// handlers, runs models through a runner, and calls third parties.
package worker

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/bobbydeveaux/weir/engine"
	"github.com/bobbydeveaux/weir/runner"
	"github.com/bobbydeveaux/weir/spec"
)

// Call is what a deterministic handler receives.
type Call struct {
	// Input is the step's resolved input as JSON.
	Input json.RawMessage
	// IdempotencyKey is the same for every attempt at this step, so a handler
	// that has side effects can make a retry safe.
	IdempotencyKey string
	RunID          string
	Step           string
}

// Handler is a piece of registered code that a deterministic step runs.
type Handler func(ctx context.Context, call Call) (json.RawMessage, error)

// permanent marks an error that retrying will not fix.
type permanent struct{ err error }

func (p permanent) Error() string { return p.err.Error() }
func (p permanent) Unwrap() error { return p.err }

// Permanent wraps an error to say that retrying will not help.
func Permanent(err error) error { return permanent{err} }

// Registry holds the handlers a worker can run.
type Registry struct {
	mu       sync.RWMutex
	handlers map[string]Handler
}

// NewRegistry returns a registry with the built-in handlers.
func NewRegistry() *Registry {
	r := &Registry{handlers: map[string]Handler{}}
	r.Register("core.noop", func(context.Context, Call) (json.RawMessage, error) {
		return json.RawMessage(`{}`), nil
	})
	r.Register("core.echo", func(_ context.Context, c Call) (json.RawMessage, error) {
		return c.Input, nil
	})
	return r
}

// Register adds or replaces a handler.
func (r *Registry) Register(name string, h Handler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handlers[name] = h
}

// Exists reports whether a handler is registered. It is what the engine uses
// to refuse a definition that names a handler nobody can run.
func (r *Registry) Exists(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.handlers[name]
	return ok
}

func (r *Registry) get(name string) (Handler, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	h, ok := r.handlers[name]
	return h, ok
}

// Worker claims and runs steps.
type Worker struct {
	Engine   *engine.Engine
	Runner   runner.Runner
	Handlers *Registry

	// ID names this worker in the audit record.
	ID string
	// Lease is how long the worker holds an attempt between heartbeats.
	Lease time.Duration
	// Idle is how long to wait when there is nothing to do.
	Idle time.Duration
	// CallbackURL is the public address of the callback endpoint, passed to
	// third parties so they know where to return a token.
	CallbackURL string
	// HTTP makes the calls for external steps.
	HTTP *http.Client
	// PollEvery is the default delay between polls of an external step.
	PollEvery time.Duration
}

// kinds a worker executes. A human step never reaches a worker.
var kinds = []string{spec.KindDecide, spec.KindAI, spec.KindDeterministic, spec.KindExternal}

func (w *Worker) defaults() {
	if w.ID == "" {
		w.ID = "worker"
	}
	if w.Lease == 0 {
		w.Lease = 30 * time.Second
	}
	if w.Idle == 0 {
		w.Idle = 250 * time.Millisecond
	}
	if w.HTTP == nil {
		w.HTTP = &http.Client{Timeout: 30 * time.Second}
	}
	if w.PollEvery == 0 {
		w.PollEvery = 5 * time.Second
	}
}

// Run claims and runs steps until ctx is cancelled, sweeping as it goes.
func (w *Worker) Run(ctx context.Context) error {
	w.defaults()
	lastSweep := time.Time{}
	for ctx.Err() == nil {
		if time.Since(lastSweep) > time.Second {
			if _, err := w.Engine.Sweep(ctx); err != nil && ctx.Err() == nil {
				return fmt.Errorf("sweep: %w", err)
			}
			lastSweep = time.Now()
		}
		found, err := w.RunOnce(ctx)
		if err != nil && ctx.Err() == nil {
			return err
		}
		if !found {
			select {
			case <-ctx.Done():
			case <-time.After(w.Idle):
			}
		}
	}
	return nil
}

// RunOnce claims one step and runs it. It reports whether there was a step to run.
func (w *Worker) RunOnce(ctx context.Context) (bool, error) {
	w.defaults()
	work, err := w.Engine.Claim(ctx, kinds, w.ID, w.Lease)
	if err != nil {
		return false, fmt.Errorf("claim: %w", err)
	}
	if work == nil {
		return false, nil
	}

	// Keep the lease alive while the step runs.
	beat, stop := context.WithCancel(ctx)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		tick := time.NewTicker(w.Lease / 3)
		defer tick.Stop()
		for {
			select {
			case <-beat.Done():
				return
			case <-tick.C:
				_ = w.Engine.Heartbeat(beat, work.Token, w.Lease)
			}
		}
	}()

	stepCtx := ctx
	cancel := func() {}
	if work.Step.Timeout != "" && work.Step.Kind != spec.KindExternal {
		if d, err := spec.ParseDuration(work.Step.Timeout); err == nil {
			stepCtx, cancel = context.WithTimeout(ctx, d)
		}
	}
	err = w.execute(stepCtx, work)
	cancel()
	stop()
	wg.Wait()

	// A stale token means the attempt was taken away, for example because the
	// lease ran out. The engine has already dealt with it.
	if errors.Is(err, engine.ErrStaleToken) {
		return true, nil
	}
	return true, err
}

func (w *Worker) execute(ctx context.Context, work *engine.Work) error {
	switch work.Step.Kind {
	case spec.KindDeterministic:
		return w.deterministic(ctx, work)
	case spec.KindDecide:
		return w.decide(ctx, work)
	case spec.KindAI:
		return w.ai(ctx, work)
	case spec.KindExternal:
		return w.external(ctx, work)
	}
	return w.Engine.Fail(ctx, work.Token, engine.Failure{Type: "kind_unsupported", Message: "no worker runs " + work.Step.Kind + " steps", Actor: w.ID})
}

func (w *Worker) deterministic(ctx context.Context, work *engine.Work) error {
	h, ok := w.Handlers.get(work.Step.Handler)
	if !ok {
		return w.Engine.Fail(ctx, work.Token, engine.Failure{Type: "handler_missing",
			Message: "handler " + work.Step.Handler + " is not registered on this worker", Actor: w.ID})
	}
	out, err := h(ctx, Call{Input: work.Input, IdempotencyKey: work.IdempotencyKey, RunID: work.RunID, Step: work.Step.ID})
	if err != nil {
		var perm permanent
		return w.Engine.Fail(context.WithoutCancel(ctx), work.Token, engine.Failure{Type: "handler_failed", Message: err.Error(),
			Retryable: !errors.As(err, &perm), Actor: w.ID})
	}
	return w.Engine.Complete(ctx, work.Token, engine.Result{Output: out, Actor: w.ID})
}

func (w *Worker) decide(ctx context.Context, work *engine.Work) error {
	st := work.Step
	system, prompt := render(work.Template, work.Input)
	res, err := w.Runner.Decide(ctx, runner.DecideRequest{Step: st.ID, System: system, Prompt: prompt, Labels: st.Labels, Tier: st.Tier})
	call := w.call(work, st.Template, system, prompt, res.Usage, err)

	// A decide step never fails for want of an answer: anything short of a
	// confident, valid label becomes the declared default.
	threshold := st.Confidence
	label, confident, reason := res.Label, true, ""
	switch {
	case err != nil:
		label, confident, reason = st.Default, false, "the classifier failed: "+err.Error()
	case !slices.Contains(st.Labels, res.Label):
		label, confident, reason = st.Default, false, fmt.Sprintf("the classifier answered %q, which is not a label", res.Label)
	case res.Confidence < threshold:
		label, confident, reason = st.Default, false, fmt.Sprintf("confidence %.2f is below %.2f", res.Confidence, threshold)
	}
	out, _ := json.Marshal(map[string]any{"label": label, "confidence": res.Confidence, "confident": confident, "reason": reason})
	return w.Engine.Complete(context.WithoutCancel(ctx), work.Token, engine.Result{Output: out, Label: label, Actor: w.ID, Calls: []engine.AICall{call}})
}

func (w *Worker) ai(ctx context.Context, work *engine.Work) error {
	st := work.Step
	system, prompt := render(work.Template, work.Input)
	req := runner.RunRequest{Step: st.ID, System: system, Prompt: prompt, Tier: st.Tier}
	if st.Output != nil {
		req.Schema, _ = json.Marshal(st.Output.Schema)
	}
	if st.Budget != nil {
		req.BudgetUSD = st.Budget.USD
		if st.Budget.Minutes > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, time.Duration(st.Budget.Minutes*float64(time.Minute)))
			defer cancel()
		}
	}
	if st.Tools != nil {
		req.ToolsRead, req.ToolsWrite = st.Tools.Read, st.Tools.Write
	}
	res, err := w.Runner.Run(ctx, req)
	call := w.call(work, st.Prompt, system, prompt, res.Usage, err)
	done := context.WithoutCancel(ctx)
	if err != nil {
		return w.Engine.Fail(done, work.Token, engine.Failure{Type: "runner_failed", Message: err.Error(),
			Retryable: true, Actor: w.ID, CostUSD: res.Usage.CostUSD}, call)
	}
	if st.Budget != nil && st.Budget.USD > 0 && res.Usage.CostUSD > st.Budget.USD {
		return w.Engine.Fail(done, work.Token, engine.Failure{Type: "budget_exceeded",
			Message: fmt.Sprintf("the step spent $%.2f of a $%.2f budget", res.Usage.CostUSD, st.Budget.USD),
			Actor:   w.ID, CostUSD: res.Usage.CostUSD}, call)
	}
	return w.Engine.Complete(done, work.Token, engine.Result{Output: res.Output, Actor: w.ID, Calls: []engine.AICall{call}})
}

// call builds the audit record for one model call.
func (w *Worker) call(work *engine.Work, ref, system, prompt string, u runner.Usage, err error) engine.AICall {
	key, version, _ := spec.TemplateRef(ref)
	sum := sha256.Sum256([]byte(system + "\n" + prompt))
	c := engine.AICall{Runner: w.Runner.Name(), Provider: u.Provider, Model: u.Model, TemplateKey: key, TemplateVersion: version,
		PromptHash: hex.EncodeToString(sum[:]), TokensIn: u.TokensIn, TokensOut: u.TokensOut,
		CostUSD: u.CostUSD, CostEstimated: u.CostEstimated, Duration: u.Duration}
	if c.Provider == "" {
		c.Provider = "unknown"
	}
	if c.Model == "" {
		c.Model = "unknown"
	}
	if err != nil {
		c.Error = err.Error()
	}
	return c
}

func (w *Worker) external(ctx context.Context, work *engine.Work) error {
	st := work.Step
	method := st.Call.Method
	if method == "" {
		method = http.MethodPost
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(work.ConnectionURL, "/")+st.Call.Path, bytes.NewReader(work.Input))
	if err != nil {
		return w.Engine.Fail(ctx, work.Token, engine.Failure{Type: "call_invalid", Message: err.Error(), Actor: w.ID})
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", work.IdempotencyKey)
	if st.Resume == "callback" {
		req.Header.Set("Weir-Callback-Token", work.Token)
		req.Header.Set("Weir-Callback-URL", strings.TrimRight(w.CallbackURL, "/")+"/v1/callbacks/"+work.Token)
	}
	resp, err := w.HTTP.Do(req)
	if err != nil {
		return w.Engine.Fail(context.WithoutCancel(ctx), work.Token, engine.Failure{Type: "call_failed", Message: err.Error(), Retryable: true, Actor: w.ID})
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return w.Engine.Fail(ctx, work.Token, engine.Failure{Type: "call_failed",
			Message:   fmt.Sprintf("%s %s returned %d", method, st.Call.Path, resp.StatusCode),
			Retryable: resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests, Actor: w.ID})
	}

	if st.Resume == "callback" {
		// The step now waits for the third party to return the token. If it
		// already has, the token is stale and there is nothing left to do.
		return w.Engine.Park(ctx, work.Token)
	}

	var response any
	if err := json.Unmarshal(body, &response); err != nil {
		return w.Engine.Fail(ctx, work.Token, engine.Failure{Type: "response_invalid", Message: "the reply was not JSON", Retryable: true, Actor: w.ID})
	}
	done, err := spec.EvalBool(st.Poll.Until, spec.Scope{Response: response})
	if err != nil {
		return w.Engine.Fail(ctx, work.Token, engine.Failure{Type: "expression_failed", Message: err.Error(), Actor: w.ID})
	}
	if done {
		return w.Engine.Complete(ctx, work.Token, engine.Result{Output: body, Actor: "connection:" + st.Connection})
	}
	every := w.PollEvery
	if st.Poll.Every != "" {
		if d, err := spec.ParseDuration(st.Poll.Every); err == nil {
			every = d
		}
	}
	return w.Engine.Defer(ctx, work.Token, every)
}

// render fills a template's {{name}} placeholders from the step's input.
// {{input}} stands for the whole input as JSON.
func render(t *engine.Template, input json.RawMessage) (system, prompt string) {
	if t == nil {
		return "", string(input)
	}
	values := map[string]any{}
	_ = json.Unmarshal(input, &values)
	fill := func(s string) string {
		s = strings.ReplaceAll(s, "{{input}}", string(input))
		for k, v := range values {
			text, ok := v.(string)
			if !ok {
				raw, _ := json.Marshal(v)
				text = string(raw)
			}
			s = strings.ReplaceAll(s, "{{"+k+"}}", text)
		}
		return s
	}
	return fill(t.System), fill(t.User)
}
