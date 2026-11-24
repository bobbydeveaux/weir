package engine

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/bobbydeveaux/weir/spec"
)

// Resumed is what an event did for one step that was waiting for it.
type Resumed struct {
	RunID string `json:"run_id"`
	// Step is the step's id in its definition.
	Step string `json:"step"`
	// Error says why the step's rule could not be worked out for this event.
	// The step is still waiting.
	Error string `json:"error,omitempty"`
}

// inboundEvent is an event as it is stored.
type inboundEvent struct {
	id, typ, source, key, sentBy string
	data                         string
}

// scope is what an expression sees as `event`.
func (ev inboundEvent) scope() (map[string]any, error) {
	var data any
	if err := json.Unmarshal([]byte(ev.data), &data); err != nil {
		return nil, err
	}
	return map[string]any{"type": ev.typ, "id": ev.key, "source": ev.source, "data": data}, nil
}

// lookBack is how many events a step that starts waiting will consider.
const lookBack = 1000

// lockEvents makes a step starting to wait and an event arriving take turns,
// for one type of event in one workspace. Without it an event could arrive
// after the step had looked back and before the step's wait could be seen,
// and be missed by both. The lock lasts until the transaction ends.
func lockEvents(ctx context.Context, tx pgx.Tx, workspace, eventType string) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, "weir:event:"+workspace+":"+eventType)
	return err
}

// waitForEvent records that an attempt is waiting for an event, then looks
// back: an event that arrived after the run started and before the step
// began to wait is taken now. It reports whether one was, in which case the
// step is already complete.
func (e *Engine) waitForEvent(ctx context.Context, tx pgx.Tx, run *runRow, step *stepRow, att *attemptRow, st *spec.Step) (bool, error) {
	if err := lockEvents(ctx, tx, run.workspace, st.Event); err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO event_waits (attempt_id, workspace_id, run_id, event_type) VALUES ($1::uuid, $2::uuid, $3::uuid, $4)`,
		att.id, run.workspace, run.id, st.Event); err != nil {
		return false, err
	}
	if err := e.event(ctx, tx, run, &step.id, &att.id, "step.waiting", "worker", map[string]any{"for": "event", "event": st.Event}); err != nil {
		return false, err
	}

	// The newest are read, and tried oldest first. An event this run has
	// already taken is left out, so a step visited twice waits for a new one.
	rows, err := tx.Query(ctx, `
		SELECT ev.id::text, ev.type, ev.source, COALESCE(ev.key, ''), ev.sent_by, ev.data::text
		FROM inbound_events ev
		WHERE ev.workspace_id = $1::uuid AND ev.type = $2
			AND ev.received_at >= (SELECT created_at FROM runs WHERE id = $3::uuid)
			AND NOT EXISTS (SELECT 1 FROM event_waits w WHERE w.run_id = $3::uuid AND w.event_id = ev.id)
		ORDER BY ev.received_at DESC LIMIT $4`, run.workspace, st.Event, run.id, lookBack)
	if err != nil {
		return false, err
	}
	earlier, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (inboundEvent, error) {
		var ev inboundEvent
		return ev, row.Scan(&ev.id, &ev.typ, &ev.source, &ev.key, &ev.sentBy, &ev.data)
	})
	if err != nil || len(earlier) == 0 {
		return false, err
	}
	scope, err := e.scope(ctx, tx, run)
	if err != nil {
		return false, err
	}
	for i := len(earlier) - 1; i >= 0; i-- {
		ev := earlier[i]
		if scope.Event, err = ev.scope(); err != nil {
			continue
		}
		// An event the rule cannot be worked out for is not this step's.
		if ok, err := spec.EvalBool(st.When, scope); err != nil || !ok {
			continue
		}
		return true, e.takeEvent(ctx, tx, att, ev)
	}
	return false, nil
}

// takeEvent completes a waiting attempt with the event it was waiting for.
// The event becomes the step's output, and whoever sent it is the actor.
func (e *Engine) takeEvent(ctx context.Context, tx pgx.Tx, att *attemptRow, ev inboundEvent) error {
	if _, err := tx.Exec(ctx, `UPDATE event_waits SET event_id = $2::uuid WHERE attempt_id = $1::uuid`, att.id, ev.id); err != nil {
		return err
	}
	output, err := json.Marshal(map[string]any{"type": ev.typ, "id": ev.key, "source": ev.source,
		"sent_by": ev.sentBy, "data": json.RawMessage(ev.data)})
	if err != nil {
		return err
	}
	return e.completeTx(ctx, tx, att, Result{Output: output, Actor: ev.sentBy})
}

// resumeWaiting gives an event that has just arrived to every step in this
// workspace that is waiting for it. Each step is moved on in a transaction of
// its own.
func (e *Engine) resumeWaiting(ctx context.Context, ev inboundEvent) ([]Resumed, error) {
	out := []Resumed{}
	var attempts []string
	err := e.inTx(ctx, func(tx pgx.Tx) error {
		// A step that is part-way through starting to wait finishes first,
		// and is then in this list. One that starts later looks back and
		// finds the event for itself.
		if err := lockEvents(ctx, tx, e.workspace, ev.typ); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `
			SELECT w.attempt_id::text FROM event_waits w JOIN attempts a ON a.id = w.attempt_id
			WHERE w.workspace_id = $1::uuid AND w.event_type = $2 AND w.event_id IS NULL AND a.status = 'waiting'
			ORDER BY w.created_at`, e.workspace, ev.typ)
		if err != nil {
			return err
		}
		attempts, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err
	})
	if err != nil {
		return out, err
	}
	eventScope, err := ev.scope()
	if err != nil {
		return out, err
	}
	for _, id := range attempts {
		var r *Resumed
		err := e.inTx(ctx, func(tx pgx.Tx) error {
			att := &attemptRow{id: id}
			err := tx.QueryRow(ctx, `SELECT step_id::text, number FROM attempts WHERE id = $1::uuid AND status = 'waiting' FOR UPDATE`,
				id).Scan(&att.stepID, &att.number)
			if errors.Is(err, pgx.ErrNoRows) {
				return nil // it stopped waiting in the meantime
			}
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
			if err != nil || run.workspace != e.workspace || st.Resume != spec.ResumeEvent || st.Event != ev.typ {
				return nil
			}
			scope, err := e.scope(ctx, tx, run)
			if err != nil {
				return err
			}
			scope.Event = eventScope
			ok, err := spec.EvalBool(st.When, scope)
			if err != nil {
				r = &Resumed{RunID: run.id, Step: step.key, Error: "when: " + err.Error()}
				return nil
			}
			if !ok {
				return nil
			}
			r = &Resumed{RunID: run.id, Step: step.key}
			return e.takeEvent(ctx, tx, att, ev)
		})
		if err != nil {
			r = &Resumed{Error: "attempt " + id + ": " + err.Error()}
		}
		if r != nil {
			out = append(out, *r)
		}
	}
	return out, nil
}
