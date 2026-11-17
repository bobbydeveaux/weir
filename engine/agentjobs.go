package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/jackc/pgx/v5"
)

// An agent that runs in a container of its own talks to Weir with the token
// of its own attempt, and nothing else. It fetches its job and the directory
// it starts from, and sends back one result.

// ErrResultSent is returned when an agent sends a second result.
var ErrResultSent = errors.New("this agent has already sent its result")

func agentKey(workspace, attempt, part string) string {
	return workspace + "/agents/" + attempt + "/" + part
}

// agentJob finds the job behind a token, for an attempt that is still running.
func (e *Engine) agentJob(ctx context.Context, token string) (attempt, workspace string, job json.RawMessage, hasArchive, finished bool, err error) {
	attempt, _, _, err = e.toolContext(ctx, token)
	if err != nil {
		return "", "", nil, false, false, err
	}
	var raw string
	err = e.pool.QueryRow(ctx, `
		SELECT workspace_id::text, job::text, has_archive, finished_at IS NOT NULL FROM agent_jobs WHERE attempt_id = $1::uuid`, attempt).
		Scan(&workspace, &raw, &hasArchive, &finished)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", nil, false, false, fmt.Errorf("a job for this agent: %w", ErrNotFound)
	}
	return attempt, workspace, json.RawMessage(raw), hasArchive, finished, err
}

// PutAgentJob records the job for the attempt behind token, and the archive
// of the directory the agent starts from, which may be nil.
func (e *Engine) PutAgentJob(ctx context.Context, token string, job json.RawMessage, archive io.Reader) error {
	attempt, runID, _, err := e.toolContext(ctx, token)
	if err != nil {
		return err
	}
	var workspace string
	if err := e.pool.QueryRow(ctx, `SELECT workspace_id::text FROM runs WHERE id = $1::uuid`, runID).Scan(&workspace); err != nil {
		return err
	}
	if archive != nil {
		if e.Files == nil {
			return ErrNoFileStore
		}
		if _, err := e.Files.Put(ctx, agentKey(workspace, attempt, "workspace"), archive, "application/gzip"); err != nil {
			return fmt.Errorf("keep the agent's working directory: %w", err)
		}
	}
	_, err = e.pool.Exec(ctx, `
		INSERT INTO agent_jobs (attempt_id, workspace_id, job, has_archive) VALUES ($1::uuid, $2::uuid, $3::jsonb, $4)
		ON CONFLICT (attempt_id) DO UPDATE SET job = EXCLUDED.job, has_archive = EXCLUDED.has_archive, finished_at = NULL`,
		attempt, workspace, string(job), archive != nil)
	return err
}

// AgentJob returns the job for the agent holding token.
func (e *Engine) AgentJob(ctx context.Context, token string) (json.RawMessage, error) {
	_, _, job, _, _, err := e.agentJob(ctx, token)
	return job, err
}

// AgentArchive opens the directory the agent holding token starts from.
func (e *Engine) AgentArchive(ctx context.Context, token string) (io.ReadCloser, error) {
	attempt, workspace, _, has, _, err := e.agentJob(ctx, token)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, fmt.Errorf("a working directory for this agent: %w", ErrNotFound)
	}
	return e.Files.Get(ctx, agentKey(workspace, attempt, "workspace"))
}

// PutAgentResult keeps the one result an agent sends.
func (e *Engine) PutAgentResult(ctx context.Context, token string, result io.Reader) error {
	attempt, workspace, _, _, finished, err := e.agentJob(ctx, token)
	if err != nil {
		return err
	}
	if finished {
		return ErrResultSent
	}
	if e.Files == nil {
		return ErrNoFileStore
	}
	if _, err := e.Files.Put(ctx, agentKey(workspace, attempt, "result"), result, "application/json"); err != nil {
		return fmt.Errorf("keep the agent's result: %w", err)
	}
	tag, err := e.pool.Exec(ctx, `UPDATE agent_jobs SET finished_at = now() WHERE attempt_id = $1::uuid AND finished_at IS NULL`, attempt)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrResultSent
	}
	return err
}

// TakeAgentResult returns the result the agent behind token sent, and
// removes the job and everything kept for it. found is false when the agent
// sent nothing.
func (e *Engine) TakeAgentResult(ctx context.Context, token string) (result []byte, found bool, err error) {
	attempt, workspace, _, has, finished, err := e.agentJob(ctx, token)
	if err != nil {
		return nil, false, err
	}
	defer func() {
		done := context.WithoutCancel(ctx)
		_, _ = e.pool.Exec(done, `DELETE FROM agent_jobs WHERE attempt_id = $1::uuid`, attempt)
		if e.Files != nil {
			if has {
				_ = e.Files.Delete(done, agentKey(workspace, attempt, "workspace"))
			}
			if finished {
				_ = e.Files.Delete(done, agentKey(workspace, attempt, "result"))
			}
		}
	}()
	if !finished {
		return nil, false, nil
	}
	body, err := e.Files.Get(ctx, agentKey(workspace, attempt, "result"))
	if err != nil {
		return nil, false, err
	}
	defer body.Close()
	result, err = io.ReadAll(body)
	return result, err == nil, err
}
