package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/bobbydeveaux/weir/spec"
)

// setSchedules stores when each scheduled trigger of a workflow's new live
// version is next due, replacing the schedules of the version before.
func setSchedules(ctx context.Context, tx pgx.Tx, defID string, def *spec.Definition) error {
	if _, err := tx.Exec(ctx, `DELETE FROM schedules WHERE definition_id = $1::uuid`, defID); err != nil {
		return err
	}
	now := time.Now()
	for i, t := range def.Triggers {
		if t.Schedule == "" {
			continue
		}
		s, err := spec.ParseSchedule(t.Schedule)
		if err != nil {
			return err
		}
		next := s.Next(now)
		if next.IsZero() {
			return fmt.Errorf("triggers[%d]: the schedule %q never comes due", i, t.Schedule)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schedules (definition_id, trigger_index, cron, next_at) VALUES ($1::uuid, $2, $3, $4)`,
			defID, i, t.Schedule, next); err != nil {
			return err
		}
	}
	return nil
}

// fireSchedule starts a run for one schedule that is due, if there is one,
// and reports whether there was. Two workers sweeping at once cannot both
// take the same schedule, and a firing has its own key, so a schedule that is
// due starts one run however many times it is looked at.
//
// A schedule that fell due while nothing was sweeping fires once, when the
// sweep comes back. It does not catch up on every time it missed.
func (e *Engine) fireSchedule(ctx context.Context) (bool, error) {
	found := false
	err := e.inTx(ctx, func(tx pgx.Tx) error {
		var defID, workspace, slug, namespace, name, cron, triggers string
		var index int
		var due time.Time
		err := tx.QueryRow(ctx, `
			SELECT s.definition_id::text, s.trigger_index, s.cron, s.next_at, d.workspace_id::text, w.slug, d.namespace, d.name,
				COALESCE(v.spec->'triggers', '[]'::jsonb)::text
			FROM schedules s
			JOIN definitions d ON d.id = s.definition_id
			JOIN workspaces w ON w.id = d.workspace_id
			JOIN aliases a ON a.definition_id = d.id AND a.name = 'live'
			JOIN definition_versions v ON v.id = a.version_id
			WHERE s.next_at <= now()
			ORDER BY s.next_at FOR UPDATE OF s SKIP LOCKED LIMIT 1`).
			Scan(&defID, &index, &cron, &due, &workspace, &slug, &namespace, &name, &triggers)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		found = true

		// Start the run in the workflow's own workspace.
		scoped := *e
		scoped.workspace, scoped.slug = workspace, slug
		startErr := func() error {
			var list []spec.Trigger
			if err := json.Unmarshal([]byte(triggers), &list); err != nil || index >= len(list) {
				return errors.New("the workflow's live version no longer has this schedule")
			}
			input := json.RawMessage(`{}`)
			if list[index].Input != nil {
				at := due.UTC().Format(time.RFC3339)
				resolved, err := spec.Resolve(list[index].Input, spec.Scope{Event: map[string]any{
					"type": "schedule", "id": at, "source": "schedule", "data": map[string]any{"at": at}}})
				if err != nil {
					return fmt.Errorf("input: %w", err)
				}
				if input, err = json.Marshal(resolved); err != nil {
					return err
				}
			}
			_, _, err := scoped.StartRun(ctx, StartRequest{Namespace: namespace, Name: name, Input: input, Requester: "schedule",
				IdempotencyKey: fmt.Sprintf("schedule:%d:%d", index, due.Unix())})
			return err
		}()

		// Whether or not the run started, move on to the next time, so that a
		// schedule that cannot start is not tried again every second.
		var note *string
		if startErr != nil {
			text := startErr.Error()
			note = &text
		}
		next := time.Now().Add(time.Hour) // only used if the stored expression is unreadable
		if s, err := spec.ParseSchedule(cron); err == nil {
			if n := s.Next(time.Now()); !n.IsZero() {
				next = n
			}
		}
		_, err = tx.Exec(ctx, `UPDATE schedules SET next_at = $3, last_at = $4, last_error = $5 WHERE definition_id = $1::uuid AND trigger_index = $2`,
			defID, index, next, due, note)
		return err
	})
	return found, err
}
