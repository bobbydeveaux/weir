package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/bobbydeveaux/weir/spec"
)

// --- rows ------------------------------------------------------------------------

type stepRow struct {
	id           string
	runID        string
	key          string
	kind         string
	status       string
	compensates  *string
	input        string
	attemptsMade int
	maxAttempts  int
}

func (e *Engine) loadStep(ctx context.Context, tx pgx.Tx, stepID string) (*stepRow, error) {
	s := &stepRow{id: stepID}
	err := tx.QueryRow(ctx, `
		SELECT run_id::text, step_key, kind, status, compensates::text, input::text, attempts_made, max_attempts
		FROM steps WHERE id = $1::uuid FOR UPDATE`, stepID).
		Scan(&s.runID, &s.key, &s.kind, &s.status, &s.compensates, &s.input, &s.attemptsMade, &s.maxAttempts)
	if err != nil {
		return nil, fmt.Errorf("load step %s: %w", stepID, err)
	}
	return s, nil
}

type attemptRow struct {
	id     string
	stepID string
	number int
}

// liveAttempt finds the live attempt a token belongs to and locks it.
func (e *Engine) liveAttempt(ctx context.Context, tx pgx.Tx, token string, statuses ...string) (*attemptRow, error) {
	a := &attemptRow{}
	err := tx.QueryRow(ctx, `
		SELECT id::text, step_id::text, number FROM attempts
		WHERE token_hash = $1 AND status = ANY($2) FOR UPDATE`, tokenHash(token), statuses).
		Scan(&a.id, &a.stepID, &a.number)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrStaleToken
	}
	return a, err
}

// --- creating steps and moving between them -----------------------------------------

// createStep adds the next step of a run. A human step is parked at once,
// because nothing has to run before a person can act on it.
func (e *Engine) createStep(ctx context.Context, tx pgx.Tx, run *runRow, st *spec.Step, scope spec.Scope) error {
	// What a step is given is its input. An external step may say it as
	// call.body instead, which is the older way of writing the same thing.
	mapping := st.Input
	if st.Kind == spec.KindExternal && st.Call != nil && st.Call.Body != nil {
		mapping = st.Call.Body
	}
	var input any = map[string]any{}
	if mapping != nil {
		resolved, err := spec.Resolve(mapping, scope)
		if err != nil {
			return e.failRun(ctx, tx, run, failure("input_invalid", fmt.Sprintf("step %s: %v", st.ID, err)))
		}
		input = resolved
	} else if st.Kind == spec.KindDecide || st.Kind == spec.KindAI {
		input = scope.Input // with no mapping, a model step sees the run's input
	}
	inputJSON, err := json.Marshal(input)
	if err != nil {
		return err
	}

	status := "runnable"
	if st.Kind == spec.KindHuman {
		status = "waiting"
	}
	var stepID string
	err = tx.QueryRow(ctx, `
		INSERT INTO steps (run_id, step_key, visit, kind, status, input, max_attempts)
		SELECT $1::uuid, $2, COUNT(*) + 1, $3, $4, $5::jsonb, $6 FROM steps WHERE run_id = $1::uuid AND step_key = $2
		RETURNING id::text`, run.id, st.ID, st.Kind, status, string(inputJSON), st.MaxAttempts()).Scan(&stepID)
	if err != nil {
		return fmt.Errorf("create step %s: %w", st.ID, err)
	}

	if st.Kind != spec.KindHuman {
		if err := e.setRunStatus(ctx, tx, run, "running"); err != nil {
			return err
		}
		if err := e.event(ctx, tx, run, &stepID, nil, "step.ready", "engine", nil); err != nil {
			return err
		}
		return e.wake(ctx, tx, run.id)
	}
	return e.openTask(ctx, tx, run, st, stepID)
}

// openTask parks a human step under a token and puts it in the inbox.
func (e *Engine) openTask(ctx context.Context, tx pgx.Tx, run *runRow, st *spec.Step, stepID string) error {
	_, hash, err := newToken()
	if err != nil {
		return err
	}
	var attemptID string
	err = tx.QueryRow(ctx, `
		INSERT INTO attempts (step_id, number, status, token_hash) VALUES ($1::uuid, 1, 'waiting', $2)
		RETURNING id::text`, stepID, hash).Scan(&attemptID)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE steps SET attempts_made = 1 WHERE id = $1::uuid`, stepID); err != nil {
		return err
	}
	var excluded *string
	if st.Assign.Not == "run.requester" {
		excluded = &run.requester
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO human_tasks (attempt_id, run_id, step_id, assignee_role, assignee_user, excluded_actor, outcomes, status)
		VALUES ($1::uuid, $2::uuid, $3::uuid, NULLIF($4, ''), NULLIF($5, ''), $6, $7, 'pending')`,
		attemptID, run.id, stepID, st.Assign.Role, st.Assign.User, excluded, st.HumanOutcomes())
	if err != nil {
		return err
	}
	if err := e.addTimers(ctx, tx, run.id, stepID, attemptID, st); err != nil {
		return err
	}
	if err := e.setRunStatus(ctx, tx, run, "waiting"); err != nil {
		return err
	}
	return e.event(ctx, tx, run, &stepID, &attemptID, "step.waiting", "engine",
		map[string]any{"for": "person", "role": st.Assign.Role, "user": st.Assign.User})
}

// addTimers schedules a waiting step's timeout and any escalations.
func (e *Engine) addTimers(ctx context.Context, tx pgx.Tx, runID, stepID, attemptID string, st *spec.Step) error {
	add := func(kind string, after time.Duration, data any) error {
		raw, _ := json.Marshal(data)
		_, err := tx.Exec(ctx, `
			INSERT INTO timers (run_id, step_id, attempt_id, kind, fire_at, data)
			VALUES ($1::uuid, $2::uuid, $3::uuid, $4, now() + make_interval(secs => $5::float8), $6::jsonb)`,
			runID, stepID, attemptID, kind, after.Seconds(), string(raw))
		return err
	}
	if st.Timeout != "" {
		d, err := spec.ParseDuration(st.Timeout)
		if err != nil {
			return err
		}
		if err := add("timeout", d, map[string]any{}); err != nil {
			return err
		}
	}
	for i, esc := range st.Escalate {
		d, err := spec.ParseDuration(esc.After)
		if err != nil {
			return err
		}
		if err := add("escalate", d, map[string]any{"role": esc.Role, "level": i + 1}); err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) setRunStatus(ctx context.Context, tx pgx.Tx, run *runRow, status string) error {
	if run.status == status {
		return nil
	}
	run.status = status
	_, err := tx.Exec(ctx, `UPDATE runs SET status = $2 WHERE id = $1::uuid`, run.id, status)
	return err
}

// advance moves a run on from a step that has finished: to the step a next
// rule names, else to the following step, else to the end.
func (e *Engine) advance(ctx context.Context, tx pgx.Tx, run *runRow, st *spec.Step, scope spec.Scope) error {
	target := ""
	for _, rule := range st.Next {
		if rule.When == "" {
			target = rule.Go
			break
		}
		ok, err := spec.EvalBool(rule.When, scope)
		if err != nil {
			return e.failRun(ctx, tx, run, failure("expression_failed", fmt.Sprintf("step %s: %v", st.ID, err)))
		}
		if ok {
			target = rule.Go
			break
		}
	}
	if target == "" && !st.End {
		if _, i := run.def.Step(st.ID); i >= 0 && i+1 < len(run.def.Steps) {
			target = run.def.Steps[i+1].ID
		}
	}
	if target == "" {
		return e.finishRun(ctx, tx, run, scope.Output)
	}
	next, _ := run.def.Step(target)
	if next == nil {
		return e.failRun(ctx, tx, run, failure("definition_invalid", "next step "+target+" does not exist"))
	}
	scope, err := e.scope(ctx, tx, run)
	if err != nil {
		return err
	}
	return e.createStep(ctx, tx, run, next, scope)
}

func (e *Engine) finishRun(ctx context.Context, tx pgx.Tx, run *runRow, output any) error {
	raw, err := json.Marshal(output)
	if err != nil {
		return err
	}
	run.status = "succeeded"
	if _, err := tx.Exec(ctx, `UPDATE runs SET status = 'succeeded', output = $2::jsonb, finished_at = now() WHERE id = $1::uuid`,
		run.id, string(raw)); err != nil {
		return err
	}
	return e.event(ctx, tx, run, nil, nil, "run.succeeded", "engine", nil)
}

// Failure describes why an attempt, a step or a run failed.
type Failure struct {
	Type    string `json:"type"`
	Message string `json:"message"`
	// Retryable says whether trying again could help.
	Retryable bool `json:"-"`
	// Actor is who reported the failure.
	Actor string `json:"-"`
	// CostUSD is what the attempt cost before it failed.
	CostUSD float64 `json:"-"`
}

func failure(typ, message string) Failure { return Failure{Type: typ, Message: message} }

// failRun ends a run as failed, with no compensation.
func (e *Engine) failRun(ctx context.Context, tx pgx.Tx, run *runRow, f Failure) error {
	raw, _ := json.Marshal(f)
	run.status = "failed"
	if _, err := tx.Exec(ctx, `UPDATE runs SET status = 'failed', error = $2::jsonb, finished_at = now() WHERE id = $1::uuid`,
		run.id, string(raw)); err != nil {
		return err
	}
	return e.event(ctx, tx, run, nil, nil, "run.failed", "engine", f)
}

// --- claiming work -------------------------------------------------------------------

// Work is one attempt handed to a worker.
type Work struct {
	// Token completes or fails this attempt. It is shown once and never stored.
	Token     string
	RunID     string
	StepID    string
	AttemptID string
	Attempt   int
	// Step is the step's definition. For a compensation it is a synthetic
	// deterministic step that names the undo handler.
	Step spec.Step
	// Input is the step's resolved input as JSON.
	Input json.RawMessage
	// Requester is who started the run.
	Requester string
	// IdempotencyKey is stable across retries of the same step visit.
	IdempotencyKey string
	// Template is set for decide and ai steps.
	Template *Template
	// Connection is set for external steps: where to call, and how to sign in.
	Connection Endpoint
}

// Claim takes the next runnable step of one of the given kinds and returns it
// as Work, or nil when there is nothing to do. The attempt must be completed,
// failed, parked or deferred before lease runs out, or it is given to someone else.
func (e *Engine) Claim(ctx context.Context, kinds []string, workerID string, lease time.Duration) (*Work, error) {
	var work *Work
	err := e.inTx(ctx, func(tx pgx.Tx) error {
		var stepID string
		err := tx.QueryRow(ctx, `
			SELECT id::text FROM steps
			WHERE status = 'runnable' AND not_before <= now() AND kind = ANY($1)
			ORDER BY not_before FOR UPDATE SKIP LOCKED LIMIT 1`, kinds).Scan(&stepID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		step, err := e.loadStep(ctx, tx, stepID)
		if err != nil {
			return err
		}
		run, err := e.loadRun(ctx, tx, step.runID)
		if err != nil {
			return err
		}
		if run.budget != nil && run.spent >= *run.budget {
			return e.stepFailed(ctx, tx, run, step, failure("budget_exceeded", "the run's budget is spent"))
		}

		st, err := stepSpec(run, step)
		if err != nil {
			return e.failRun(ctx, tx, run, failure("definition_invalid", err.Error()))
		}
		w := &Work{RunID: run.id, StepID: step.id, Attempt: step.attemptsMade + 1, Step: *st,
			Input: json.RawMessage(step.input), Requester: run.requester, IdempotencyKey: step.id}
		switch st.Kind {
		case spec.KindDecide:
			w.Template, err = e.loadTemplate(ctx, tx, run.workspace, st.Template)
		case spec.KindAI:
			w.Template, err = e.loadTemplate(ctx, tx, run.workspace, st.Prompt)
		case spec.KindExternal:
			// A step that only waits for an event calls nobody.
			if st.Connection != "" {
				w.Connection, err = e.endpoint(ctx, tx, run.workspace, st.Connection)
			}
		}
		if err != nil {
			return e.stepFailed(ctx, tx, run, step, failure("reference_missing", err.Error()))
		}

		token, hash, err := newToken()
		if err != nil {
			return err
		}
		w.Token = token
		if _, err := tx.Exec(ctx, `UPDATE steps SET status = 'running', attempts_made = $2 WHERE id = $1::uuid`,
			step.id, w.Attempt); err != nil {
			return err
		}
		err = tx.QueryRow(ctx, `
			INSERT INTO attempts (step_id, number, status, token_hash, worker_id, lease_until)
			SELECT $1::uuid, COALESCE(MAX(number), 0) + 1, 'running', $2, $3, now() + make_interval(secs => $4::float8)
			FROM attempts WHERE step_id = $1::uuid
			RETURNING id::text`, step.id, hash, workerID, lease.Seconds()).Scan(&w.AttemptID)
		if err != nil {
			return err
		}
		if err := e.event(ctx, tx, run, &step.id, &w.AttemptID, "attempt.started", workerID,
			map[string]any{"attempt": w.Attempt}); err != nil {
			return err
		}
		work = w
		return nil
	})
	return work, err
}

// stepSpec finds the definition of a step row. A compensation step gets a
// synthetic definition that runs the undo handler.
func stepSpec(run *runRow, step *stepRow) (*spec.Step, error) {
	if step.compensates != nil {
		return &spec.Step{ID: step.key, Kind: spec.KindDeterministic, Handler: step.key, OnFailure: spec.OnFailureBlock}, nil
	}
	st, _ := run.def.Step(step.key)
	if st == nil {
		return nil, fmt.Errorf("step %s is not in the definition", step.key)
	}
	return st, nil
}

// Heartbeat extends the lease on an attempt a worker still holds.
func (e *Engine) Heartbeat(ctx context.Context, token string, lease time.Duration) error {
	tag, err := e.pool.Exec(ctx, `
		UPDATE attempts SET lease_until = now() + make_interval(secs => $2::float8)
		WHERE token_hash = $1 AND status = 'running'`, tokenHash(token), lease.Seconds())
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrStaleToken
	}
	return nil
}

// --- completing and failing ------------------------------------------------------------

// AICall records one model call made during an attempt.
type AICall struct {
	Runner          string
	Provider        string
	Model           string
	TemplateKey     string
	TemplateVersion int
	PromptHash      string
	TokensIn        int64
	TokensOut       int64
	CostUSD         float64
	CostEstimated   bool
	Duration        time.Duration
	Error           string
	// Prompt is the text sent and Response the text that came back. They are
	// kept so that a run can be audited; long ones are cut short.
	Prompt   string
	Response string
}

// Result is what an attempt produced.
type Result struct {
	// Output is the step's output as JSON.
	Output json.RawMessage
	// Label is the label a decide step chose.
	Label string
	// Outcome is the outcome a person chose.
	Outcome string
	// Actor is who completed the attempt.
	Actor string
	// Calls are the model calls made. Their cost is added to the step and the run.
	Calls []AICall
	// Files are the files the step produced, already kept with KeepFiles.
	Files []FileRecord
}

// Complete finishes the live attempt that token belongs to. A token that is
// stale, already used or unknown returns ErrStaleToken and changes nothing.
func (e *Engine) Complete(ctx context.Context, token string, res Result) error {
	return e.inTx(ctx, func(tx pgx.Tx) error {
		att, err := e.liveAttempt(ctx, tx, token, "running", "waiting")
		if err != nil {
			return err
		}
		return e.completeTx(ctx, tx, att, res)
	})
}

func (e *Engine) completeTx(ctx context.Context, tx pgx.Tx, att *attemptRow, res Result) error {
	step, err := e.loadStep(ctx, tx, att.stepID)
	if err != nil {
		return err
	}
	run, err := e.loadRun(ctx, tx, step.runID)
	if err != nil {
		return err
	}
	st, err := stepSpec(run, step)
	if err != nil {
		return e.failRun(ctx, tx, run, failure("definition_invalid", err.Error()))
	}
	if res.Actor == "" {
		res.Actor = "worker"
	}
	if len(res.Output) == 0 {
		res.Output = json.RawMessage(`null`)
	}
	cost := 0.0
	for _, c := range res.Calls {
		cost += c.CostUSD
	}

	// The engine checks the output itself, whatever the worker or model claims.
	if st.Output != nil && len(st.Output.Schema) > 0 {
		if err := validateJSON(st.Output.Schema, res.Output); err != nil {
			f := Failure{Type: "output_invalid", Message: err.Error(), Retryable: true, Actor: res.Actor, CostUSD: cost}
			return e.failTx(ctx, tx, att, run, step, st, f, "failed", res.Calls)
		}
	}
	if st.Kind == spec.KindDecide && !slices.Contains(st.Labels, res.Label) {
		f := Failure{Type: "label_invalid", Message: fmt.Sprintf("%q is not one of the step's labels", res.Label), Actor: res.Actor, CostUSD: cost}
		return e.failTx(ctx, tx, att, run, step, st, f, "failed", res.Calls)
	}

	if _, err := tx.Exec(ctx, `
		UPDATE attempts SET status = 'succeeded', finished_at = now(), actor = $2, cost_usd = $3::numeric, lease_until = NULL
		WHERE id = $1::uuid`, att.id, res.Actor, cost); err != nil {
		return err
	}
	if err := e.recordCalls(ctx, tx, att.id, res.Calls); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE timers SET fired_at = now() WHERE attempt_id = $1::uuid AND fired_at IS NULL`, att.id); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE steps SET status = 'succeeded', output = $2::jsonb, label = NULLIF($3, ''), outcome = NULLIF($4, ''),
			spent_usd = spent_usd + $5::numeric, finished_at = now()
		WHERE id = $1::uuid`, step.id, string(res.Output), res.Label, res.Outcome, cost); err != nil {
		return err
	}
	if err := tx.QueryRow(ctx, `UPDATE runs SET spent_usd = spent_usd + $2::numeric WHERE id = $1::uuid RETURNING spent_usd::float8`,
		run.id, cost).Scan(&run.spent); err != nil {
		return err
	}
	if err := e.recordFiles(ctx, tx, run.id, step.id, res.Files); err != nil {
		return err
	}
	data := map[string]any{"attempt": att.number}
	if len(res.Files) > 0 {
		data["files"] = len(res.Files)
	}
	if res.Label != "" {
		data["label"] = res.Label
	}
	if res.Outcome != "" {
		data["outcome"] = res.Outcome
	}
	if cost > 0 {
		data["cost_usd"] = cost
	}
	if err := e.event(ctx, tx, run, &step.id, &att.id, "step.succeeded", res.Actor, data); err != nil {
		return err
	}

	if step.compensates != nil {
		if _, err := tx.Exec(ctx, `UPDATE steps SET status = 'compensated' WHERE id = $1::uuid`, *step.compensates); err != nil {
			return err
		}
		return e.compensateNext(ctx, tx, run)
	}
	if run.budget != nil && run.spent > *run.budget {
		return e.blockRun(ctx, tx, run, failure("budget_exceeded",
			fmt.Sprintf("spent $%.2f of a $%.2f budget", run.spent, *run.budget)))
	}

	scope, err := e.scope(ctx, tx, run)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(res.Output, &scope.Output); err != nil {
		return err
	}
	scope.Label, scope.Outcome = res.Label, res.Outcome
	return e.advance(ctx, tx, run, st, scope)
}

func (e *Engine) recordCalls(ctx context.Context, tx pgx.Tx, attemptID string, calls []AICall) error {
	for _, c := range calls {
		_, err := tx.Exec(ctx, `
			INSERT INTO ai_calls (attempt_id, runner, provider, model, template_key, template_version, prompt_hash,
				tokens_in, tokens_out, cost_usd, cost_estimated, duration_ms, error, prompt, response)
			VALUES ($1::uuid, $2, $3, $4, NULLIF($5, ''), NULLIF($6, 0), NULLIF($7, ''), $8, $9, $10::numeric, $11, $12, NULLIF($13, ''),
				NULLIF($14, ''), NULLIF($15, ''))`,
			attemptID, c.Runner, c.Provider, c.Model, c.TemplateKey, c.TemplateVersion, c.PromptHash,
			c.TokensIn, c.TokensOut, c.CostUSD, c.CostEstimated, int(c.Duration.Milliseconds()), c.Error, clip(c.Prompt), clip(c.Response))
		if err != nil {
			return err
		}
	}
	return nil
}

// Fail reports that the live attempt behind token did not succeed.
func (e *Engine) Fail(ctx context.Context, token string, f Failure, calls ...AICall) error {
	return e.inTx(ctx, func(tx pgx.Tx) error {
		att, err := e.liveAttempt(ctx, tx, token, "running", "waiting")
		if err != nil {
			return err
		}
		step, err := e.loadStep(ctx, tx, att.stepID)
		if err != nil {
			return err
		}
		run, err := e.loadRun(ctx, tx, step.runID)
		if err != nil {
			return err
		}
		st, err := stepSpec(run, step)
		if err != nil {
			return e.failRun(ctx, tx, run, failure("definition_invalid", err.Error()))
		}
		return e.failTx(ctx, tx, att, run, step, st, f, "failed", calls)
	})
}

// failTx closes an attempt as unsuccessful, then either retries the step or
// fails it for good.
func (e *Engine) failTx(ctx context.Context, tx pgx.Tx, att *attemptRow, run *runRow, step *stepRow, st *spec.Step,
	f Failure, attemptStatus string, calls []AICall) error {
	if f.Actor == "" {
		f.Actor = "worker"
	}
	raw, _ := json.Marshal(f)
	if _, err := tx.Exec(ctx, `
		UPDATE attempts SET status = $2, finished_at = now(), actor = $3, error = $4::jsonb, cost_usd = $5::numeric, lease_until = NULL
		WHERE id = $1::uuid`, att.id, attemptStatus, f.Actor, string(raw), f.CostUSD); err != nil {
		return err
	}
	if err := e.recordCalls(ctx, tx, att.id, calls); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE timers SET fired_at = now() WHERE attempt_id = $1::uuid AND fired_at IS NULL`, att.id); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE human_tasks SET status = 'expired' WHERE attempt_id = $1::uuid AND status = 'pending'`, att.id); err != nil {
		return err
	}
	if f.CostUSD > 0 {
		if _, err := tx.Exec(ctx, `UPDATE steps SET spent_usd = spent_usd + $2::numeric WHERE id = $1::uuid`, step.id, f.CostUSD); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `UPDATE runs SET spent_usd = spent_usd + $2::numeric WHERE id = $1::uuid RETURNING spent_usd::float8`,
			run.id, f.CostUSD).Scan(&run.spent); err != nil {
			return err
		}
	}
	if err := e.event(ctx, tx, run, &step.id, &att.id, "attempt."+attemptStatus, f.Actor, f); err != nil {
		return err
	}

	// A worker that vanished is not the step's fault: run it again without
	// using up a retry, a limited number of times.
	if attemptStatus == "abandoned" {
		var abandoned int
		if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM attempts WHERE step_id = $1::uuid AND status = 'abandoned'`, step.id).Scan(&abandoned); err != nil {
			return err
		}
		if abandoned <= e.MaxAbandoned {
			return e.requeue(ctx, tx, run, step, 0, true, "step.requeued")
		}
		f = failure("worker_lost", fmt.Sprintf("the worker disappeared %d times", abandoned))
	} else if f.Retryable && step.attemptsMade < step.maxAttempts {
		backoff := e.BackoffBase
		if st.Retry != nil && st.Retry.Backoff != "" {
			if d, err := spec.ParseDuration(st.Retry.Backoff); err == nil {
				backoff = d
			}
		}
		return e.requeue(ctx, tx, run, step, backoff<<(step.attemptsMade-1), false, "step.retrying")
	}
	return e.stepFailed(ctx, tx, run, step, f)
}

// requeue makes a step runnable again after a delay. With refund set, the
// attempt just made does not count towards the step's retries.
func (e *Engine) requeue(ctx context.Context, tx pgx.Tx, run *runRow, step *stepRow, after time.Duration, refund bool, eventType string) error {
	made := step.attemptsMade
	if refund && made > 0 {
		made--
	}
	if _, err := tx.Exec(ctx, `
		UPDATE steps SET status = 'runnable', attempts_made = $2, not_before = now() + make_interval(secs => $3::float8) WHERE id = $1::uuid`,
		step.id, made, after.Seconds()); err != nil {
		return err
	}
	if err := e.setRunStatus(ctx, tx, run, "running"); err != nil {
		return err
	}
	if err := e.event(ctx, tx, run, &step.id, nil, eventType, "engine", map[string]any{"after": after.String()}); err != nil {
		return err
	}
	return e.wake(ctx, tx, run.id)
}

// stepFailed marks a step as failed for good and applies its failure policy.
func (e *Engine) stepFailed(ctx context.Context, tx pgx.Tx, run *runRow, step *stepRow, f Failure) error {
	raw, _ := json.Marshal(f)
	if _, err := tx.Exec(ctx, `UPDATE steps SET status = 'failed', error = $2::jsonb, finished_at = now() WHERE id = $1::uuid`,
		step.id, string(raw)); err != nil {
		return err
	}
	if err := e.event(ctx, tx, run, &step.id, nil, "step.failed", "engine", f); err != nil {
		return err
	}
	if step.compensates != nil {
		return e.failRun(ctx, tx, run, failure("compensation_failed", "undoing "+step.key+" failed: "+f.Message))
	}
	st, err := stepSpec(run, step)
	if err != nil {
		return e.failRun(ctx, tx, run, failure("definition_invalid", err.Error()))
	}
	switch st.FailurePolicy() {
	case spec.OnFailureContinue:
		scope, err := e.scope(ctx, tx, run)
		if err != nil {
			return err
		}
		return e.advance(ctx, tx, run, st, scope)
	case spec.OnFailureCancel:
		if _, err := tx.Exec(ctx, `UPDATE runs SET error = $2::jsonb WHERE id = $1::uuid`, run.id, string(raw)); err != nil {
			return err
		}
		if err := e.setRunStatus(ctx, tx, run, "compensating"); err != nil {
			return err
		}
		return e.compensateNext(ctx, tx, run)
	default:
		return e.blockRun(ctx, tx, run, f)
	}
}

func (e *Engine) blockRun(ctx context.Context, tx pgx.Tx, run *runRow, f Failure) error {
	raw, _ := json.Marshal(f)
	run.status = "blocked"
	if _, err := tx.Exec(ctx, `UPDATE runs SET status = 'blocked', error = $2::jsonb WHERE id = $1::uuid`, run.id, string(raw)); err != nil {
		return err
	}
	return e.event(ctx, tx, run, nil, nil, "run.blocked", "engine", f)
}

// compensateNext undoes the most recent finished step that declared an undo
// handler and has not been undone. When none is left, the run has failed.
func (e *Engine) compensateNext(ctx context.Context, tx pgx.Tx, run *runRow) error {
	rows, err := tx.Query(ctx, `
		SELECT s.id::text, s.step_key, s.input::text, s.output::text FROM steps s
		WHERE s.run_id = $1::uuid AND s.status = 'succeeded' AND s.compensates IS NULL
		  AND NOT EXISTS (SELECT 1 FROM steps c WHERE c.compensates = s.id)
		ORDER BY s.finished_at DESC`, run.id)
	if err != nil {
		return err
	}
	type done struct{ id, key, input, output string }
	var finished []done
	for rows.Next() {
		var d done
		var out *string
		if err := rows.Scan(&d.id, &d.key, &d.input, &out); err != nil {
			rows.Close()
			return err
		}
		d.output = "null"
		if out != nil {
			d.output = *out
		}
		finished = append(finished, d)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	for _, d := range finished {
		st, _ := run.def.Step(d.key)
		if st == nil || st.Compensate == "" {
			continue
		}
		input := fmt.Sprintf(`{"input": %s, "output": %s}`, d.input, d.output)
		var stepID string
		err := tx.QueryRow(ctx, `
			INSERT INTO steps (run_id, step_key, visit, kind, status, compensates, input)
			SELECT $1::uuid, $2, COUNT(*) + 1, 'deterministic', 'runnable', $3::uuid, $4::jsonb
			FROM steps WHERE run_id = $1::uuid AND step_key = $2
			RETURNING id::text`, run.id, st.Compensate, d.id, input).Scan(&stepID)
		if err != nil {
			return err
		}
		if err := e.event(ctx, tx, run, &stepID, nil, "step.compensating", "engine", map[string]any{"undoes": d.key}); err != nil {
			return err
		}
		return e.wake(ctx, tx, run.id)
	}

	// Nothing left to undo: the run has failed with the error recorded when it was cancelled.
	run.status = "failed"
	if _, err := tx.Exec(ctx, `UPDATE runs SET status = 'failed', finished_at = now() WHERE id = $1::uuid`, run.id); err != nil {
		return err
	}
	return e.event(ctx, tx, run, nil, nil, "run.failed", "engine", nil)
}

// --- parking and deferring ---------------------------------------------------------------

// Park moves a running attempt to waiting: the call has been made, and the
// step now waits for whoever holds the token to return it, or for the event
// its definition names.
func (e *Engine) Park(ctx context.Context, token string) error {
	return e.inTx(ctx, func(tx pgx.Tx) error {
		att, err := e.liveAttempt(ctx, tx, token, "running")
		if err != nil {
			return err
		}
		step, err := e.loadStep(ctx, tx, att.stepID)
		if err != nil {
			return err
		}
		run, err := e.loadRun(ctx, tx, step.runID)
		if err != nil {
			return err
		}
		st, err := stepSpec(run, step)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE attempts SET status = 'waiting', lease_until = NULL WHERE id = $1::uuid`, att.id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE steps SET status = 'waiting' WHERE id = $1::uuid`, step.id); err != nil {
			return err
		}
		if err := e.addTimers(ctx, tx, run.id, step.id, att.id, st); err != nil {
			return err
		}
		if err := e.setRunStatus(ctx, tx, run, "waiting"); err != nil {
			return err
		}
		if st.Kind == spec.KindExternal && st.Resume == spec.ResumeEvent {
			_, err := e.waitForEvent(ctx, tx, run, step, att, st)
			return err
		}
		return e.event(ctx, tx, run, &step.id, &att.id, "step.waiting", "worker", map[string]any{"for": "callback"})
	})
}

// Defer puts a running step back in the queue to be tried again after a
// delay, without using up a retry. It is how a polling step waits.
//
// A polling step never parks, so no timer watches it. Its timeout is checked
// here instead, each time a poll comes back: once it has been polling for
// longer than its timeout it fails as a waiting step does when its timer
// fires. A step can therefore run over its timeout by up to one interval.
// The time is counted from the first poll since the step last failed, so a
// step that is retried or resumed gets its whole timeout again, as a step
// that parks does.
func (e *Engine) Defer(ctx context.Context, token string, after time.Duration) error {
	return e.inTx(ctx, func(tx pgx.Tx) error {
		att, err := e.liveAttempt(ctx, tx, token, "running")
		if err != nil {
			return err
		}
		step, err := e.loadStep(ctx, tx, att.stepID)
		if err != nil {
			return err
		}
		run, err := e.loadRun(ctx, tx, step.runID)
		if err != nil {
			return err
		}
		st, err := stepSpec(run, step)
		if err != nil {
			return e.failRun(ctx, tx, run, failure("definition_invalid", err.Error()))
		}
		if st.Timeout != "" {
			if limit, err := spec.ParseDuration(st.Timeout); err == nil {
				// The database's clock, because it also set started_at. Only
				// the polls since the last attempt that ended in failure count.
				var over bool
				if err := tx.QueryRow(ctx, `
					SELECT now() - MIN(started_at) >= make_interval(secs => $2::float8) FROM attempts
					WHERE step_id = $1::uuid AND number > COALESCE((
						SELECT MAX(number) FROM attempts WHERE step_id = $1::uuid AND status NOT IN ('running', 'deferred')), 0)`,
					step.id, limit.Seconds()).Scan(&over); err != nil {
					return err
				}
				if over {
					return e.failTx(ctx, tx, att, run, step, st,
						Failure{Type: "timeout", Message: "the step was not completed in time", Actor: "engine"}, "timed_out", nil)
				}
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE attempts SET status = 'deferred', finished_at = now(), lease_until = NULL WHERE id = $1::uuid`, att.id); err != nil {
			return err
		}
		return e.requeue(ctx, tx, run, step, after, true, "step.deferred")
	})
}

// --- human tasks ---------------------------------------------------------------------------

// Task is a human step waiting for a decision.
type Task struct {
	ID       string   `json:"id"`
	RunID    string   `json:"run_id"`
	Step     string   `json:"step"`
	Role     string   `json:"role,omitempty"`
	User     string   `json:"user,omitempty"`
	Outcomes []string `json:"outcomes"`
	Level    int      `json:"escalation_level"`
	// Namespace and Name say which workflow the task belongs to.
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	// Input is what the step was given: what the person is being asked about.
	Input     json.RawMessage `json:"input"`
	CreatedAt time.Time       `json:"created_at"`
}

// Tasks lists the human steps that are waiting.
func (e *Engine) Tasks(ctx context.Context) ([]Task, error) {
	rows, err := e.pool.Query(ctx, `
		SELECT t.id::text, t.run_id::text, s.step_key, COALESCE(t.assignee_role, ''), COALESCE(t.assignee_user, ''),
			t.outcomes, t.escalation_level, d.namespace, d.name, s.input::text, t.created_at
		FROM human_tasks t JOIN steps s ON s.id = t.step_id JOIN runs r ON r.id = t.run_id
			JOIN definitions d ON d.id = r.definition_id
		WHERE t.status = 'pending' AND r.workspace_id = $1::uuid ORDER BY t.created_at`, e.workspace)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Task
	for rows.Next() {
		var t Task
		var input string
		if err := rows.Scan(&t.ID, &t.RunID, &t.Step, &t.Role, &t.User, &t.Outcomes, &t.Level, &t.Namespace, &t.Name, &input, &t.CreatedAt); err != nil {
			return nil, err
		}
		t.Input = json.RawMessage(input)
		out = append(out, t)
	}
	return out, rows.Err()
}

// Decide records a person's decision on a waiting human step and moves the
// run on. The engine, not the caller, enforces who may decide.
func (e *Engine) Decide(ctx context.Context, taskID, actor, outcome, note string, data ...json.RawMessage) error {
	var extra any
	if len(data) > 0 && len(data[0]) > 0 {
		if err := json.Unmarshal(data[0], &extra); err != nil {
			return fmt.Errorf("the decision's data is not valid JSON: %w", err)
		}
	}
	if actor == "" {
		return fmt.Errorf("a decision needs an actor: %w", ErrNotAllowed)
	}
	return e.inTx(ctx, func(tx pgx.Tx) error {
		var attemptID, status string
		var excluded, user *string
		var outcomes []string
		err := tx.QueryRow(ctx, `
			SELECT t.attempt_id::text, t.status, t.excluded_actor, t.assignee_user, t.outcomes
			FROM human_tasks t JOIN runs r ON r.id = t.run_id
			WHERE t.id = $1::uuid AND r.workspace_id = $2::uuid FOR UPDATE OF t`, taskID, e.workspace).Scan(&attemptID, &status, &excluded, &user, &outcomes)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("task %s: %w", taskID, ErrNotFound)
		}
		if err != nil {
			return err
		}
		if status != "pending" {
			return ErrTaskClosed
		}
		if excluded != nil && *excluded == actor {
			return fmt.Errorf("%s started this run and may not decide it: %w", actor, ErrNotAllowed)
		}
		if user != nil && *user != actor {
			return fmt.Errorf("this task is assigned to %s: %w", *user, ErrNotAllowed)
		}
		if !slices.Contains(outcomes, outcome) {
			return fmt.Errorf("outcome %q is not one of %v", outcome, outcomes)
		}
		att := &attemptRow{id: attemptID}
		err = tx.QueryRow(ctx, `SELECT step_id::text, number FROM attempts WHERE id = $1::uuid AND status = 'waiting' FOR UPDATE`,
			attemptID).Scan(&att.stepID, &att.number)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrTaskClosed
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE human_tasks SET status = 'done', outcome = $2, decided_by = $3, decided_at = now(), note = NULLIF($4, '')
			WHERE id = $1::uuid`, taskID, outcome, actor, note); err != nil {
			return err
		}
		// data is whatever the person supplied with the decision, such as the
		// terms they edited before approving. Later steps read it as
		// steps.<id>.output.data.
		output, _ := json.Marshal(map[string]any{"outcome": outcome, "decided_by": actor, "note": note, "data": extra})
		return e.completeTx(ctx, tx, att, Result{Output: output, Outcome: outcome, Actor: actor})
	})
}

// --- the sweep -------------------------------------------------------------------------------

// Swept counts what one sweep found.
type Swept struct {
	ExpiredLeases int
	Timeouts      int
	Escalations   int
	// Scheduled is how many schedules came due.
	Scheduled int
}

// Sweep deals with everything time has caught up with: attempts whose worker
// stopped reporting, and timers that have come due. It is safe to run from
// several places at once.
func (e *Engine) Sweep(ctx context.Context) (Swept, error) {
	var out Swept
	for {
		fired, err := e.fireSchedule(ctx)
		if err != nil {
			return out, err
		}
		if !fired {
			break
		}
		out.Scheduled++
	}
	for {
		found := false
		err := e.inTx(ctx, func(tx pgx.Tx) error {
			att := &attemptRow{}
			err := tx.QueryRow(ctx, `
				SELECT id::text, step_id::text, number FROM attempts
				WHERE status = 'running' AND lease_until < now()
				ORDER BY lease_until FOR UPDATE SKIP LOCKED LIMIT 1`).Scan(&att.id, &att.stepID, &att.number)
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			}
			if err != nil {
				return err
			}
			found = true
			out.ExpiredLeases++
			return e.expire(ctx, tx, att, Failure{Type: "lease_expired", Message: "the worker stopped reporting", Actor: "engine"}, "abandoned")
		})
		if err != nil {
			return out, err
		}
		if !found {
			break
		}
	}
	for {
		found := false
		err := e.inTx(ctx, func(tx pgx.Tx) error {
			var timerID, kind, attemptID, data string
			err := tx.QueryRow(ctx, `
				SELECT id::text, kind, attempt_id::text, data::text FROM timers
				WHERE fired_at IS NULL AND fire_at <= now()
				ORDER BY fire_at FOR UPDATE SKIP LOCKED LIMIT 1`).Scan(&timerID, &kind, &attemptID, &data)
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			}
			if err != nil {
				return err
			}
			found = true
			if _, err := tx.Exec(ctx, `UPDATE timers SET fired_at = now() WHERE id = $1::uuid`, timerID); err != nil {
				return err
			}
			att := &attemptRow{id: attemptID}
			err = tx.QueryRow(ctx, `SELECT step_id::text, number FROM attempts WHERE id = $1::uuid AND status IN ('running', 'waiting') FOR UPDATE`,
				attemptID).Scan(&att.stepID, &att.number)
			if errors.Is(err, pgx.ErrNoRows) {
				return nil // the attempt finished before its timer fired
			}
			if err != nil {
				return err
			}
			if kind == "timeout" {
				out.Timeouts++
				return e.expire(ctx, tx, att, Failure{Type: "timeout", Message: "the step was not completed in time", Actor: "engine"}, "timed_out")
			}
			out.Escalations++
			return e.escalate(ctx, tx, att, data)
		})
		if err != nil {
			return out, err
		}
		if !found {
			break
		}
	}
	return out, nil
}

func (e *Engine) expire(ctx context.Context, tx pgx.Tx, att *attemptRow, f Failure, status string) error {
	step, err := e.loadStep(ctx, tx, att.stepID)
	if err != nil {
		return err
	}
	run, err := e.loadRun(ctx, tx, step.runID)
	if err != nil {
		return err
	}
	st, err := stepSpec(run, step)
	if err != nil {
		return e.failRun(ctx, tx, run, failure("definition_invalid", err.Error()))
	}
	return e.failTx(ctx, tx, att, run, step, st, f, status, nil)
}

func (e *Engine) escalate(ctx context.Context, tx pgx.Tx, att *attemptRow, data string) error {
	var d struct {
		Role  string `json:"role"`
		Level int    `json:"level"`
	}
	if err := json.Unmarshal([]byte(data), &d); err != nil {
		return err
	}
	step, err := e.loadStep(ctx, tx, att.stepID)
	if err != nil {
		return err
	}
	run, err := e.loadRun(ctx, tx, step.runID)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE human_tasks SET assignee_role = $2, assignee_user = NULL, escalation_level = $3
		WHERE attempt_id = $1::uuid AND status = 'pending'`, att.id, d.Role, d.Level); err != nil {
		return err
	}
	return e.event(ctx, tx, run, &step.id, &att.id, "task.escalated", "engine", d)
}

// --- resuming --------------------------------------------------------------------------------

// Resume restarts a blocked run from where it stopped, keeping the results of
// every step that finished. budgetUSD, when above zero, replaces the run's budget.
func (e *Engine) Resume(ctx context.Context, runID, actor string, budgetUSD float64) error {
	return e.inTx(ctx, func(tx pgx.Tx) error {
		run, err := e.loadRun(ctx, tx, runID)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("run %s: %w", runID, ErrNotFound)
		}
		if err != nil {
			return err
		}
		if run.workspace != e.workspace {
			return fmt.Errorf("run %s: %w", runID, ErrNotFound)
		}
		if run.status != "blocked" {
			return ErrNotBlocked
		}
		if budgetUSD > 0 {
			run.budget = &budgetUSD
			if _, err := tx.Exec(ctx, `UPDATE runs SET budget_usd = $2::numeric WHERE id = $1::uuid`, run.id, budgetUSD); err != nil {
				return err
			}
		}
		if run.budget != nil && run.spent >= *run.budget {
			return fmt.Errorf("the run has spent its budget; resume it with a larger one: %w", ErrNotAllowed)
		}
		if _, err := tx.Exec(ctx, `UPDATE runs SET error = NULL WHERE id = $1::uuid`, run.id); err != nil {
			return err
		}
		if err := e.event(ctx, tx, run, nil, nil, "run.resumed", actor, nil); err != nil {
			return err
		}

		// Retry the step that failed, if one did.
		var stepID, key string
		var made int
		err = tx.QueryRow(ctx, `
			SELECT id::text, step_key, attempts_made FROM steps
			WHERE run_id = $1::uuid AND status = 'failed' AND compensates IS NULL
			ORDER BY finished_at DESC LIMIT 1`, run.id).Scan(&stepID, &key, &made)
		if err == nil {
			st, _ := run.def.Step(key)
			extra := 1
			if st != nil {
				extra = st.MaxAttempts()
			}
			if _, err := tx.Exec(ctx, `
				UPDATE steps SET status = 'runnable', error = NULL, finished_at = NULL, not_before = now(), max_attempts = $2
				WHERE id = $1::uuid`, stepID, made+extra); err != nil {
				return err
			}
			if err := e.setRunStatus(ctx, tx, run, "running"); err != nil {
				return err
			}
			return e.wake(ctx, tx, run.id)
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}

		// Otherwise the run stopped after a step that succeeded, such as on its
		// budget. Carry on from that step.
		var output *string
		var label, outcome string
		err = tx.QueryRow(ctx, `
			SELECT step_key, output::text, COALESCE(label, ''), COALESCE(outcome, '') FROM steps
			WHERE run_id = $1::uuid AND status = 'succeeded' AND compensates IS NULL
			ORDER BY finished_at DESC LIMIT 1`, run.id).Scan(&key, &output, &label, &outcome)
		if err != nil {
			return fmt.Errorf("find where to resume from: %w", err)
		}
		st, _ := run.def.Step(key)
		if st == nil {
			return e.failRun(ctx, tx, run, failure("definition_invalid", "step "+key+" is not in the definition"))
		}
		scope, err := e.scope(ctx, tx, run)
		if err != nil {
			return err
		}
		if output != nil {
			if err := json.Unmarshal([]byte(*output), &scope.Output); err != nil {
				return err
			}
		}
		scope.Label, scope.Outcome = label, outcome
		if err := e.setRunStatus(ctx, tx, run, "running"); err != nil {
			return err
		}
		return e.advance(ctx, tx, run, st, scope)
	})
}

// clip cuts a stored prompt or reply to a size that is safe to keep.
func clip(text string) string {
	const most = 200_000
	if len(text) <= most {
		return text
	}
	return text[:most] + fmt.Sprintf("\n… cut short; %d bytes in all", len(text))
}

// Cancel ends a run that has not finished. Nothing more of it will run: its
// live attempts are given up, its open human tasks are closed, and a worker
// still holding one of its tokens will find the token stale. It is how a
// person closes a run that is blocked and cannot be put right.
//
// Steps that already succeeded are not undone.
func (e *Engine) Cancel(ctx context.Context, runID, actor, reason string) error {
	return e.inTx(ctx, func(tx pgx.Tx) error {
		run, err := e.loadRun(ctx, tx, runID)
		if err != nil || run.workspace != e.workspace {
			return fmt.Errorf("run %s: %w", runID, ErrNotFound)
		}
		switch run.status {
		case "succeeded", "failed", "cancelled":
			return fmt.Errorf("the run has already %s: %w", run.status, ErrNotAllowed)
		}
		for _, q := range []string{
			`UPDATE attempts SET status = 'abandoned', finished_at = now(), lease_until = NULL
				WHERE status IN ('running', 'waiting') AND step_id IN (SELECT id FROM steps WHERE run_id = $1::uuid)`,
			`UPDATE human_tasks SET status = 'expired' WHERE run_id = $1::uuid AND status = 'pending'`,
			`UPDATE timers SET fired_at = now() WHERE run_id = $1::uuid AND fired_at IS NULL`,
			`UPDATE steps SET status = 'cancelled', finished_at = now()
				WHERE run_id = $1::uuid AND status IN ('runnable', 'running', 'waiting')`,
		} {
			if _, err := tx.Exec(ctx, q, run.id); err != nil {
				return err
			}
		}
		f := Failure{Type: "cancelled", Message: reason, Actor: actor}
		raw, _ := json.Marshal(f)
		run.status = "cancelled"
		if _, err := tx.Exec(ctx, `UPDATE runs SET status = 'cancelled', error = $2::jsonb, finished_at = now() WHERE id = $1::uuid`,
			run.id, string(raw)); err != nil {
			return err
		}
		return e.event(ctx, tx, run, nil, nil, "run.cancelled", actor, f)
	})
}
