package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/bobbydeveaux/weir/internal/secrets"
)

// ConnectionTool is one tool an MCP connection offers.
type ConnectionTool struct {
	// Name is the tool's name on the server. In a workflow it is written
	// with the connection in front: "search" on "knowledge" is "knowledge.search".
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Class       string         `json:"class"`
	InputSchema map[string]any `json:"input_schema"`
}

// WorkspaceTool is a tool as one workspace sees it, with where to reach it.
type WorkspaceTool struct {
	// Name is the name used in workflows, with the connection in front.
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Class       string         `json:"class"`
	InputSchema map[string]any `json:"input_schema"`
	Connection  string         `json:"connection"`
	// Remote is the tool's name on its server, and Endpoint is where the
	// server is and how to sign in to it.
	Remote   string   `json:"-"`
	Endpoint Endpoint `json:"-"`
}

// PutMCPConnection creates or updates a connection to an MCP server, with the
// tools the server offered. The tools are this workspace's alone.
func (e *Engine) PutMCPConnection(ctx context.Context, name, url string, readOnly bool, auth Auth, tools []ConnectionTool) error {
	if err := auth.Check(); err != nil {
		return fmt.Errorf("connection %s: %w", name, err)
	}
	kind := auth.Kind
	if kind == "" {
		kind = secrets.None
	}
	if strings.Contains(name, ".") {
		return errors.New("a connection's name cannot contain a dot")
	}
	if tools == nil {
		tools = []ConnectionTool{}
	}
	for i := range tools {
		if readOnly || tools[i].Class == ToolRead {
			tools[i].Class = ToolRead
		} else {
			tools[i].Class = ToolWrite // an unlabelled tool is not trusted to be harmless
		}
	}
	raw, err := json.Marshal(tools)
	if err != nil {
		return err
	}
	_, err = e.pool.Exec(ctx, `
		INSERT INTO connections (workspace_id, name, base_url, kind, read_only, tools, synced_at,
			auth_kind, secret_ref, auth_header, auth_username)
		VALUES ($1::uuid, $2, $3, 'mcp', $4, $5::jsonb, now(), $6, NULLIF($7, ''), NULLIF($8, ''), NULLIF($9, ''))
		ON CONFLICT (workspace_id, name) DO UPDATE
		SET base_url = EXCLUDED.base_url, kind = 'mcp', read_only = EXCLUDED.read_only, tools = EXCLUDED.tools, synced_at = now(),
			auth_kind = EXCLUDED.auth_kind, secret_ref = EXCLUDED.secret_ref, auth_header = EXCLUDED.auth_header,
			auth_username = EXCLUDED.auth_username`,
		e.workspace, name, url, readOnly, string(raw), kind, auth.Secret, auth.Header, auth.Username)
	return err
}

// workspaceTool finds a tool among one workspace's MCP connections.
func (e *Engine) workspaceTool(ctx context.Context, workspace, name string) (*WorkspaceTool, error) {
	connection, remote, ok := strings.Cut(name, ".")
	if !ok {
		return nil, fmt.Errorf("tool %s: %w", name, ErrNotFound)
	}
	var raw string
	err := e.pool.QueryRow(ctx, `
		SELECT tools::text FROM connections
		WHERE workspace_id = $1::uuid AND name = $2 AND kind = 'mcp'`, workspace, connection).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("tool %s: %w", name, ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	ep, err := e.endpoint(ctx, e.pool, workspace, connection)
	if err != nil {
		return nil, err
	}
	var tools []ConnectionTool
	if err := json.Unmarshal([]byte(raw), &tools); err != nil {
		return nil, err
	}
	for _, t := range tools {
		if t.Name == remote {
			return &WorkspaceTool{Name: name, Description: t.Description, Class: t.Class, InputSchema: t.InputSchema,
				Connection: connection, Remote: remote, Endpoint: ep}, nil
		}
	}
	return nil, fmt.Errorf("tool %s: %w", name, ErrNotFound)
}

// ToolForRun finds a tool among the MCP connections of the workspace that
// owns runID. A run can never reach another workspace's tools.
func (e *Engine) ToolForRun(ctx context.Context, runID, name string) (*WorkspaceTool, error) {
	var workspace string
	if err := e.pool.QueryRow(ctx, `SELECT workspace_id::text FROM runs WHERE id = $1::uuid`, runID).Scan(&workspace); err != nil {
		return nil, fmt.Errorf("run %s: %w", runID, ErrNotFound)
	}
	return e.workspaceTool(ctx, workspace, name)
}

// WorkspaceTools lists every tool this workspace's MCP connections offer.
func (e *Engine) WorkspaceTools(ctx context.Context) ([]WorkspaceTool, error) {
	rows, err := e.pool.Query(ctx, `
		SELECT name, tools::text FROM connections WHERE workspace_id = $1::uuid AND kind = 'mcp' ORDER BY name`, e.workspace)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []WorkspaceTool{}
	for rows.Next() {
		var connection, raw string
		if err := rows.Scan(&connection, &raw); err != nil {
			return nil, err
		}
		var tools []ConnectionTool
		if err := json.Unmarshal([]byte(raw), &tools); err != nil {
			return nil, err
		}
		for _, t := range tools {
			out = append(out, WorkspaceTool{Name: connection + "." + t.Name, Description: t.Description, Class: t.Class,
				InputSchema: t.InputSchema, Connection: connection})
		}
	}
	return out, rows.Err()
}

// toolClass reports a tool's class: from the server's own registry first, so
// that a workspace cannot stand in for a built-in tool, then from the
// workspace's MCP connections.
func (e *Engine) toolClass(ctx context.Context, name string) (string, bool) {
	if e.ToolClass != nil {
		if class, ok := e.ToolClass(name); ok {
			return class, true
		}
	}
	if t, err := e.workspaceTool(ctx, e.workspace, name); err == nil {
		return t.Class, true
	}
	return "", false
}
