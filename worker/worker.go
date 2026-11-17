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
	"os"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bobbydeveaux/weir/engine"
	"github.com/bobbydeveaux/weir/internal/secrets"
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
	tools    map[string]Tool

	// Secrets finds the value of a connection's secret when a call is made.
	// NewRegistry sets it to read this process's environment.
	Secrets *secrets.Resolver
}

// SecretStatus reports whether a workspace's secret can be found, and where
// it is read from. It never returns the value.
func (r *Registry) SecretStatus(workspace, ref string) (found bool, readFrom string) {
	resolver := r.Secrets
	if resolver == nil {
		resolver = secrets.FromEnv(nil)
	}
	readFrom, _ = resolver.Where(workspace, ref)
	_, err := resolver.Resolve(workspace, ref)
	return err == nil, readFrom
}

// sign adds a connection's credential to a request. If the secret cannot be
// found it returns an error, and the request must not be sent.
func (r *Registry) sign(req *http.Request, to engine.Endpoint) error {
	resolver := r.Secrets
	if resolver == nil {
		resolver = secrets.FromEnv(nil)
	}
	if err := resolver.Apply(req, to.Workspace, to.Auth); err != nil {
		return fmt.Errorf("connection %s: %w", to.Connection, err)
	}
	return nil
}

// NewRegistry returns a registry with the built-in handlers.
func NewRegistry() *Registry {
	r := &Registry{handlers: map[string]Handler{}, Secrets: secrets.FromEnv(nil)}
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

// HandlerNames lists the registered handlers, in order.
func (r *Registry) HandlerNames() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.handlers))
	for name := range r.handlers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
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
	// ToolURL is where an agent's tool calls are sent: the address of Weir's
	// own API as the agent's machine sees it. Empty means CallbackURL.
	ToolURL string
}

func (w *Worker) toolURL() string {
	if w.ToolURL != "" {
		return w.ToolURL
	}
	return w.CallbackURL
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
		// Calls may carry a credential, so a redirect to another host is not followed.
		w.HTTP = &http.Client{Timeout: 30 * time.Second, CheckRedirect: secrets.SameHost}
	}
	if w.PollEvery == 0 {
		w.PollEvery = 5 * time.Second
	}
}

// Run claims and runs steps until ctx is cancelled, sweeping as it goes.
func (w *Worker) Run(ctx context.Context) error {
	w.defaults()
	// Hooks are sent from their own loop, so that an application hears about
	// a run promptly even while this worker is busy with a long step.
	go func() {
		for ctx.Err() == nil {
			if _, err := w.DeliverHooks(ctx); err != nil && ctx.Err() == nil {
				fmt.Fprintf(os.Stderr, "weir: delivering hooks: %v\n", err)
			}
			select {
			case <-ctx.Done():
			case <-time.After(500 * time.Millisecond):
			}
		}
	}()
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

// DeliverHooks sends every hook that is due and returns how many arrived.
func (w *Worker) DeliverHooks(ctx context.Context) (int, error) {
	w.defaults()
	return w.Engine.DeliverHooks(ctx, func(ctx context.Context, d engine.HookDelivery) error {
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.URL, bytes.NewReader(d.Body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Weir-Hook-Id", d.ID)
		req.Header.Set("Idempotency-Key", d.ID)
		if err := w.Handlers.sign(req, d.To); err != nil {
			return err // not sent; tried again later, once the secret is there
		}
		resp, err := w.HTTP.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 300 {
			return fmt.Errorf("the receiver returned %d", resp.StatusCode)
		}
		return nil
	})
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
	res, err := w.Runner.Decide(ctx, runner.DecideRequest{Step: st.ID, System: system, Prompt: prompt, Labels: st.Labels, Tier: st.Tier, Provider: st.Provider, Describe: st.Describe})
	call := w.call(work, st.Template, system, prompt, fmt.Sprintf("%s (confidence %.2f)", res.Label, res.Confidence), res.Usage, err)

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
	if work.Template != nil {
		req.Model = work.Template.Model
	}
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
		// The agent is told about only the tools this step lists, and reaches
		// them only through Weir, presenting this attempt's token.
		names := append(append([]string{}, st.Tools.Read...), st.Tools.Write...)
		req.Tools = DescribeFor(ctx, w.Engine, w.Handlers, work.RunID, names...)
		req.ToolURL, req.ToolToken = strings.TrimRight(w.toolURL(), "/"), work.Token
	}
	for _, ref := range st.Files {
		file, err := w.fetch(ctx, work, ref)
		if err != nil {
			return w.Engine.Fail(context.WithoutCancel(ctx), work.Token, engine.Failure{Type: "file_unavailable",
				Message: err.Error(), Retryable: true, Actor: w.ID})
		}
		req.Files = append(req.Files, file)
	}
	res, err := w.Runner.Run(ctx, req)
	response := res.Transcript
	if response == "" {
		response = string(res.Output)
	}
	call := w.call(work, st.Prompt, system, prompt, response, res.Usage, err)
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
func (w *Worker) call(work *engine.Work, ref, system, prompt, response string, u runner.Usage, err error) engine.AICall {
	key, version, _ := spec.TemplateRef(ref)
	sum := sha256.Sum256([]byte(system + "\n" + prompt))
	c := engine.AICall{Runner: w.Runner.Name(), Provider: u.Provider, Model: u.Model, TemplateKey: key, TemplateVersion: version,
		PromptHash: hex.EncodeToString(sum[:]), TokensIn: u.TokensIn, TokensOut: u.TokensOut,
		CostUSD: u.CostUSD, CostEstimated: u.CostEstimated, Duration: u.Duration,
		Prompt: strings.TrimSpace("SYSTEM:\n" + system + "\n\nUSER:\n" + prompt), Response: response}
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
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(work.Connection.URL, "/")+st.Call.Path, bytes.NewReader(work.Input))
	if err != nil {
		return w.Engine.Fail(ctx, work.Token, engine.Failure{Type: "call_invalid", Message: err.Error(), Actor: w.ID})
	}
	// Without its credential the call is not made at all. Trying again will
	// not help until someone sets the secret, so the run stops for a person.
	if err := w.Handlers.sign(req, work.Connection); err != nil {
		return w.Engine.Fail(ctx, work.Token, engine.Failure{Type: "secret_unavailable", Message: err.Error(), Actor: w.ID})
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
			Message:   fmt.Sprintf("%s %s returned %d%s", method, st.Call.Path, resp.StatusCode, reason(body)),
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

// maxFile is the largest document fetched for an agent.
const maxFile = 20 << 20

// fetch downloads one document an ai step lists, from the named connection.
func (w *Worker) fetch(ctx context.Context, work *engine.Work, ref spec.FileRef) (runner.File, error) {
	fill := filler(work.Input)
	name, path := fill(ref.Name), fill(ref.Path)
	to, err := w.Engine.Endpoint(ctx, work.RunID, ref.Connection)
	if err != nil {
		return runner.File{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(to.URL, "/")+path, nil)
	if err != nil {
		return runner.File{}, err
	}
	if err := w.Handlers.sign(req, to); err != nil {
		return runner.File{}, err
	}
	client := w.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return runner.File{}, fmt.Errorf("fetch %s: %w", name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return runner.File{}, fmt.Errorf("fetch %s: %s returned %d", name, ref.Connection, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxFile+1))
	if err != nil {
		return runner.File{}, fmt.Errorf("fetch %s: %w", name, err)
	}
	if len(data) > maxFile {
		return runner.File{}, fmt.Errorf("%s is larger than %d MB", name, maxFile>>20)
	}
	return runner.File{Name: name, Data: data}, nil
}

// filler returns a function that fills {{name}} placeholders from a step's
// input. {{input}} stands for the whole input as JSON.
func filler(input json.RawMessage) func(string) string {
	values := map[string]any{}
	_ = json.Unmarshal(input, &values)
	return func(s string) string {
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
}

// render fills a template's placeholders from the step's input.
func render(t *engine.Template, input json.RawMessage) (system, prompt string) {
	if t == nil {
		return "", string(input)
	}
	fill := filler(input)
	return fill(t.System), fill(t.User)
}

// reason picks out why a third party refused a call, when its reply says, so
// that the person looking at a failed step is not left with a bare status code.
func reason(body []byte) string {
	var reply struct {
		Error   string `json:"error"`
		Detail  string `json:"detail"`
		Message string `json:"message"`
	}
	text := ""
	if json.Unmarshal(body, &reply) == nil {
		for _, candidate := range []string{reply.Error, reply.Detail, reply.Message} {
			if candidate != "" {
				text = candidate
				break
			}
		}
	}
	if text == "" {
		return ""
	}
	if len(text) > 300 {
		text = text[:300] + "…"
	}
	return ": " + text
}
