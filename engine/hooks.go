package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// maxHookAttempts is how many times a hook is tried before Weir gives up.
const maxHookAttempts = 8

// enqueueHooks puts a hook in the outbox for every hook of the run's workflow
// that wants to hear about this event. It runs in the transaction that wrote
// the event, so a hook is queued if and only if the thing it reports happened.
func (e *Engine) enqueueHooks(ctx context.Context, tx pgx.Tx, run *runRow, stepID *string, seq int, typ, actor string, data []byte) error {
	if run.def == nil || len(run.def.Hooks) == 0 {
		return nil
	}
	var step string
	for _, h := range run.def.Hooks {
		if !h.Matches(typ) {
			continue
		}
		if stepID != nil && step == "" {
			if err := tx.QueryRow(ctx, `SELECT step_key FROM steps WHERE id = $1::uuid`, *stepID).Scan(&step); err != nil {
				return err
			}
		}
		payload, err := json.Marshal(map[string]any{
			"connection": h.Connection, "path": h.Path,
			"body": map[string]any{
				"type": typ, "run_id": run.id, "seq": seq, "step": step, "actor": actor,
				"workflow":  map[string]any{"namespace": run.def.Namespace, "name": run.def.Name, "version": run.def.Version},
				"run_input": json.RawMessage(run.input), "data": json.RawMessage(data), "at": time.Now().UTC(),
			},
		})
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO outbox (kind, run_id, payload) VALUES ('hook', $1::uuid, $2::jsonb)`, run.id, string(payload)); err != nil {
			return err
		}
	}
	return nil
}

// HookDelivery is one hook to send.
type HookDelivery struct {
	// ID identifies the delivery. A receiver that sees the same id twice has
	// been sent the same hook twice and should act once.
	ID string
	// URL is where to post Body.
	URL  string
	Body json.RawMessage
	// To is the connection the hook goes to, for signing the request.
	To Endpoint
}

// DeliverHooks sends every hook that is due, one at a time, using send. A
// hook that fails is tried again later, waiting longer each time, and is
// given up on after a limited number of tries. It returns how many were
// delivered. It is safe to run from several places at once.
func (e *Engine) DeliverHooks(ctx context.Context, send func(context.Context, HookDelivery) error) (int, error) {
	delivered := 0
	for ctx.Err() == nil {
		found := false
		err := e.inTx(ctx, func(tx pgx.Tx) error {
			var id int64
			var attempts int
			var workspace, payload string
			err := tx.QueryRow(ctx, `
				SELECT o.id, o.attempts, r.workspace_id::text, o.payload::text
				FROM outbox o JOIN runs r ON r.id = o.run_id
				WHERE o.kind = 'hook' AND o.delivered_at IS NULL AND o.failed_at IS NULL AND o.next_at <= now()
				ORDER BY o.id FOR UPDATE OF o SKIP LOCKED LIMIT 1`).Scan(&id, &attempts, &workspace, &payload)
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			}
			if err != nil {
				return err
			}
			found = true
			var p struct {
				Connection string          `json:"connection"`
				Path       string          `json:"path"`
				Body       json.RawMessage `json:"body"`
			}
			if err := json.Unmarshal([]byte(payload), &p); err != nil {
				return err
			}
			to, sendErr := e.endpoint(ctx, tx, workspace, p.Connection)
			if sendErr == nil {
				sendErr = send(ctx, HookDelivery{ID: fmt.Sprintf("hook-%d", id), URL: to.URL + p.Path, Body: p.Body, To: to})
			}
			if sendErr == nil {
				delivered++
				_, err = tx.Exec(ctx, `UPDATE outbox SET delivered_at = now(), attempts = attempts + 1, last_error = NULL WHERE id = $1`, id)
				return err
			}
			if attempts+1 >= maxHookAttempts {
				_, err = tx.Exec(ctx, `UPDATE outbox SET failed_at = now(), attempts = attempts + 1, last_error = $2 WHERE id = $1`, id, sendErr.Error())
				return err
			}
			// Wait 5s, 10s, 20s and so on before the next try.
			wait := 5 * (1 << attempts)
			_, err = tx.Exec(ctx, `
				UPDATE outbox SET attempts = attempts + 1, last_error = $2, next_at = now() + make_interval(secs => $3::float8)
				WHERE id = $1`, id, sendErr.Error(), float64(wait))
			return err
		})
		if err != nil {
			return delivered, err
		}
		if !found {
			break
		}
	}
	return delivered, nil
}
