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

// Tool classes.
const (
	ToolRead  = "read"
	ToolWrite = "write"
)

// ToolGrant says an attempt may call a tool.
type ToolGrant struct {
	AttemptID string
	RunID     string
	Step      string
	// Class is how the step listed the tool: read or write.
	Class string
}

// toolContext finds the running ai step a token belongs to.
func (e *Engine) toolContext(ctx context.Context, token string) (attemptID, runID string, st *spec.Step, err error) {
	var key, specJSON string
	err = e.pool.QueryRow(ctx, `
		SELECT a.id::text, r.id::text, s.step_key, v.spec::text
		FROM attempts a
		JOIN steps s ON s.id = a.step_id
		JOIN runs r ON r.id = s.run_id
		JOIN definition_versions v ON v.id = r.definition_version_id
		WHERE a.token_hash = $1 AND a.status = 'running' AND s.kind = 'ai' AND s.compensates IS NULL`,
		tokenHash(token)).Scan(&attemptID, &runID, &key, &specJSON)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", nil, ErrStaleToken
	}
	if err != nil {
		return "", "", nil, err
	}
	var def spec.Definition
	if err := json.Unmarshal([]byte(specJSON), &def); err != nil {
		return "", "", nil, fmt.Errorf("read stored definition: %w", err)
	}
	st, _ = def.Step(key)
	if st == nil {
		return "", "", nil, fmt.Errorf("step %s is not in the definition", key)
	}
	return attemptID, runID, st, nil
}

// AllowedTools lists the tools the running attempt behind token may call,
// and the run it belongs to.
func (e *Engine) AllowedTools(ctx context.Context, token string) (runID string, read, write []string, err error) {
	_, runID, st, err := e.toolContext(ctx, token)
	if err != nil {
		return "", nil, nil, err
	}
	if st.Tools == nil {
		return runID, nil, nil, nil
	}
	return runID, st.Tools.Read, st.Tools.Write, nil
}

// AuthorizeTool checks that the running attempt behind token may call tool.
// A tool the step did not list returns ErrNotAllowed, with the grant still
// filled in so that the refusal can be recorded.
func (e *Engine) AuthorizeTool(ctx context.Context, token, tool string) (ToolGrant, error) {
	attemptID, runID, st, err := e.toolContext(ctx, token)
	if err != nil {
		return ToolGrant{}, err
	}
	grant := ToolGrant{AttemptID: attemptID, RunID: runID, Step: st.ID}
	if st.Tools != nil {
		switch {
		case slices.Contains(st.Tools.Read, tool):
			grant.Class = ToolRead
			return grant, nil
		case slices.Contains(st.Tools.Write, tool):
			grant.Class = ToolWrite
			return grant, nil
		}
	}
	return grant, fmt.Errorf("step %s may not call %s: %w", st.ID, tool, ErrNotAllowed)
}

// ToolCall is the record of one tool call.
type ToolCall struct {
	Tool string
	// Class is read or write; empty when the call was refused.
	Class string
	// Status is succeeded, failed or denied.
	Status   string
	Args     json.RawMessage
	Result   json.RawMessage
	Error    string
	Duration time.Duration
}

// maxStoredResult is the most of a tool's result that is kept in the record.
const maxStoredResult = 16 << 10

// RecordToolCall stores what an agent asked a tool to do and what came back.
func (e *Engine) RecordToolCall(ctx context.Context, attemptID string, c ToolCall) error {
	args := c.Args
	if !json.Valid(args) {
		args = json.RawMessage(`{}`)
	}
	var result *string
	if json.Valid(c.Result) && len(c.Result) <= maxStoredResult {
		s := string(c.Result)
		result = &s
	} else if len(c.Result) > 0 {
		s := fmt.Sprintf(`{"truncated": true, "bytes": %d}`, len(c.Result))
		result = &s
	}
	_, err := e.pool.Exec(ctx, `
		INSERT INTO tool_calls (attempt_id, tool, class, status, args, result, error, duration_ms)
		VALUES ($1::uuid, $2, NULLIF($3, ''), $4, $5::jsonb, $6::jsonb, NULLIF($7, ''), $8)`,
		attemptID, c.Tool, c.Class, c.Status, string(args), result, c.Error, int(c.Duration.Milliseconds()))
	return err
}

// ToolCalls returns the tool calls made during a run, oldest first.
func (e *Engine) ToolCalls(ctx context.Context, runID string) ([]ToolCall, error) {
	rows, err := e.pool.Query(ctx, `
		SELECT c.tool, COALESCE(c.class, ''), c.status, c.args::text, c.result::text, COALESCE(c.error, ''), c.duration_ms
		FROM tool_calls c JOIN attempts a ON a.id = c.attempt_id JOIN steps s ON s.id = a.step_id
		WHERE s.run_id = $1::uuid ORDER BY c.created_at, c.id`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ToolCall
	for rows.Next() {
		var c ToolCall
		var args string
		var result *string
		var ms int
		if err := rows.Scan(&c.Tool, &c.Class, &c.Status, &args, &result, &c.Error, &ms); err != nil {
			return nil, err
		}
		c.Args, c.Result, c.Duration = json.RawMessage(args), rawOrNil(result), time.Duration(ms)*time.Millisecond
		out = append(out, c)
	}
	return out, rows.Err()
}

// Endpoint returns a named connection as the workspace that owns runID knows
// it: its address, and how to sign in. A run can use only its own workspace's
// connections.
func (e *Engine) Endpoint(ctx context.Context, runID, name string) (Endpoint, error) {
	var workspace string
	if err := e.pool.QueryRow(ctx, `SELECT workspace_id::text FROM runs WHERE id = $1::uuid`, runID).Scan(&workspace); err != nil {
		return Endpoint{}, fmt.Errorf("run %s: %w", runID, ErrNotFound)
	}
	return e.endpoint(ctx, e.pool, workspace, name)
}

func (e *Engine) checkTools(ctx context.Context, def *spec.Definition) error {
	if e.ToolClass == nil {
		return nil
	}
	for _, s := range def.Steps {
		if s.Tools == nil {
			continue
		}
		for _, name := range s.Tools.Read {
			class, ok := e.toolClass(ctx, name)
			if !ok {
				return fmt.Errorf("step %s: tool %s: %w", s.ID, name, ErrNotFound)
			}
			if class != ToolRead {
				return fmt.Errorf("step %s: tool %s can change things, so it must be listed under write, not read", s.ID, name)
			}
		}
		for _, name := range s.Tools.Write {
			if _, ok := e.toolClass(ctx, name); !ok {
				return fmt.Errorf("step %s: tool %s: %w", s.ID, name, ErrNotFound)
			}
		}
	}
	return nil
}
