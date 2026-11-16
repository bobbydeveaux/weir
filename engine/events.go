package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/bobbydeveaux/weir/spec"
)

// Event is something an application says has happened.
type Event struct {
	// Type names what happened, such as "treaty.uploaded".
	Type string `json:"type"`
	// ID is the sender's own id for the event. Sending the same type and id
	// again starts nothing new.
	ID string `json:"id,omitempty"`
	// Source says where the event came from. It is optional.
	Source string `json:"source,omitempty"`
	// Data is the event's content.
	Data json.RawMessage `json:"data,omitempty"`
}

// Triggered is what an event did for one workflow whose trigger matched.
type Triggered struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	// RunID is the run that was started.
	RunID string `json:"run_id,omitempty"`
	// Error says why a run could not be started.
	Error string `json:"error,omitempty"`
}

// EventResult is what came of an event.
type EventResult struct {
	// ID is Weir's id for the event.
	ID string `json:"id"`
	// Duplicate is true when this event had been received before; Runs are
	// then the runs it started that first time.
	Duplicate bool        `json:"duplicate"`
	Runs      []Triggered `json:"runs"`
}

// Emit records an event and starts a run of every workflow in this workspace
// whose live version has a trigger that matches it. An event nothing is
// waiting for is recorded and starts nothing; that is not an error.
func (e *Engine) Emit(ctx context.Context, ev Event, actor string) (*EventResult, error) {
	if ev.Type == "" {
		return nil, errors.New("an event needs a type")
	}
	if len(ev.Data) == 0 {
		ev.Data = json.RawMessage(`{}`)
	}
	var data any
	if err := json.Unmarshal(ev.Data, &data); err != nil {
		return nil, fmt.Errorf("the event's data is not valid JSON: %w", err)
	}

	out := &EventResult{Runs: []Triggered{}}
	var started string
	err := e.pool.QueryRow(ctx, `
		INSERT INTO inbound_events (workspace_id, type, source, key, data, sent_by)
		VALUES ($1::uuid, $2, $3, NULLIF($4, ''), $5::jsonb, $6)
		ON CONFLICT (workspace_id, type, key) WHERE key IS NOT NULL DO NOTHING
		RETURNING id::text`, e.workspace, ev.Type, ev.Source, ev.ID, string(ev.Data), actor).Scan(&out.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		// Seen before: report what it did then.
		err = e.pool.QueryRow(ctx, `
			SELECT id::text, started::text FROM inbound_events
			WHERE workspace_id = $1::uuid AND type = $2 AND key = $3`, e.workspace, ev.Type, ev.ID).Scan(&out.ID, &started)
		if err != nil {
			return nil, err
		}
		out.Duplicate = true
		return out, json.Unmarshal([]byte(started), &out.Runs)
	}
	if err != nil {
		return nil, err
	}

	// Every workflow whose live version names this type of event.
	match, _ := json.Marshal([]map[string]string{{"event": ev.Type}})
	rows, err := e.pool.Query(ctx, `
		SELECT d.namespace, d.name, v.spec->'triggers'
		FROM definitions d
		JOIN aliases a ON a.definition_id = d.id AND a.name = 'live'
		JOIN definition_versions v ON v.id = a.version_id
		WHERE d.workspace_id = $1::uuid AND v.spec->'triggers' @> $2::jsonb
		ORDER BY d.namespace, d.name`, e.workspace, string(match))
	if err != nil {
		return nil, err
	}
	type candidate struct {
		namespace, name string
		triggers        []spec.Trigger
	}
	candidates, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (candidate, error) {
		var c candidate
		var raw string
		if err := row.Scan(&c.namespace, &c.name, &raw); err != nil {
			return c, err
		}
		return c, json.Unmarshal([]byte(raw), &c.triggers)
	})
	if err != nil {
		return nil, err
	}

	scope := spec.Scope{Event: map[string]any{"type": ev.Type, "id": ev.ID, "source": ev.Source, "data": data}}
	for _, c := range candidates {
		input, matched, err := matchTrigger(c.triggers, ev, scope)
		if err == nil && !matched {
			continue
		}
		t := Triggered{Namespace: c.namespace, Name: c.name}
		if err == nil {
			// The event's own id makes the run safe to ask for twice.
			t.RunID, _, err = e.StartRun(ctx, StartRequest{Namespace: c.namespace, Name: c.name, Input: input,
				Requester: actor, IdempotencyKey: "event:" + out.ID})
		}
		if err != nil {
			t.Error = err.Error()
		}
		out.Runs = append(out.Runs, t)
	}
	raw, _ := json.Marshal(out.Runs)
	_, err = e.pool.Exec(ctx, `UPDATE inbound_events SET started = $2::jsonb WHERE id = $1::uuid`, out.ID, string(raw))
	return out, err
}

// matchTrigger returns the run input from the first trigger that matches.
func matchTrigger(triggers []spec.Trigger, ev Event, scope spec.Scope) (json.RawMessage, bool, error) {
	for _, t := range triggers {
		if t.Event != ev.Type {
			continue
		}
		if t.When != "" {
			ok, err := spec.EvalBool(t.When, scope)
			if err != nil {
				return nil, false, fmt.Errorf("trigger: when: %w", err)
			}
			if !ok {
				continue
			}
		}
		if t.Input == nil {
			return ev.Data, true, nil
		}
		resolved, err := spec.Resolve(t.Input, scope)
		if err != nil {
			return nil, false, fmt.Errorf("trigger: input: %w", err)
		}
		input, err := json.Marshal(resolved)
		return input, err == nil, err
	}
	return nil, false, nil
}

// Events lists the most recent events, newest first, with what each started.
func (e *Engine) Events(ctx context.Context, limit int) (json.RawMessage, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	return e.list(ctx, `
		SELECT id, type, source, key, data, sent_by, received_at, started AS runs
		FROM inbound_events WHERE workspace_id = $1::uuid ORDER BY received_at DESC LIMIT $2`, e.workspace, limit)
}

// Triggers lists which events start which workflows, from their live versions.
func (e *Engine) Triggers(ctx context.Context) (json.RawMessage, error) {
	return e.list(ctx, `
		SELECT t->>'event' AS event, d.namespace, d.name, v.version, t->>'when' AS "when"
		FROM definitions d
		JOIN aliases a ON a.definition_id = d.id AND a.name = 'live'
		JOIN definition_versions v ON v.id = a.version_id
		CROSS JOIN LATERAL jsonb_array_elements(COALESCE(v.spec->'triggers', '[]'::jsonb)) t
		WHERE d.workspace_id = $1::uuid ORDER BY 1, 2, 3`, e.workspace)
}
