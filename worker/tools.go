package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/bobbydeveaux/weir/engine"
	"github.com/bobbydeveaux/weir/internal/secrets"
	"github.com/bobbydeveaux/weir/runner"
)

// Tool is something an agent may call. It is a handler with a description,
// an input schema and a class.
type Tool struct {
	Description string
	// InputSchema is the JSON Schema of the tool's arguments.
	InputSchema map[string]any
	// Class is engine.ToolRead or engine.ToolWrite. A tool that can change
	// anything outside Weir is a write.
	Class   string
	Handler Handler
}

// RegisterTool adds or replaces a tool. A tool with no class is a write,
// because an unlabelled tool must not be trusted to be harmless.
func (r *Registry) RegisterTool(name string, t Tool) {
	if t.Class != engine.ToolRead {
		t.Class = engine.ToolWrite
	}
	if t.InputSchema == nil {
		t.InputSchema = map[string]any{"type": "object"}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.tools == nil {
		r.tools = map[string]Tool{}
	}
	r.tools[name] = t
}

// ToolClass reports whether a tool is registered and whether it reads or
// writes. The engine uses it to check the tools a step lists.
func (r *Registry) ToolClass(name string) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t.Class, ok
}

func (r *Registry) tool(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

// ToolNames lists the registered tools, in order.
func (r *Registry) ToolNames() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Describe returns the descriptions of the named tools, in order, for an agent.
func (r *Registry) Describe(names ...string) []runner.Tool {
	sort.Strings(names)
	var out []runner.Tool
	for _, name := range names {
		if t, ok := r.tool(name); ok {
			out = append(out, runner.Tool{Name: name, Description: t.Description, Class: t.Class, InputSchema: t.InputSchema})
		}
	}
	return out
}

// DescribeFor returns the descriptions of the named tools as a run sees them:
// the server's own tools, and those of its workspace's MCP connections.
func DescribeFor(ctx context.Context, eng *engine.Engine, reg *Registry, runID string, names ...string) []runner.Tool {
	sort.Strings(names)
	var out []runner.Tool
	for _, name := range names {
		if t, ok := reg.tool(name); ok {
			out = append(out, runner.Tool{Name: name, Description: t.Description, Class: t.Class, InputSchema: t.InputSchema})
		} else if t, err := eng.ToolForRun(ctx, runID, name); err == nil {
			out = append(out, runner.Tool{Name: name, Description: t.Description, Class: t.Class, InputSchema: t.InputSchema})
		}
	}
	return out
}

// ErrToolFailed wraps an error from a tool's own handler.
var ErrToolFailed = errors.New("tool failed")

// CallTool runs one tool call on behalf of the agent holding token. It is the
// tool proxy: the engine decides whether the call is allowed, the registry
// runs it, and every call, allowed or not, is recorded.
func CallTool(ctx context.Context, eng *engine.Engine, reg *Registry, token, name string, args json.RawMessage) (json.RawMessage, error) {
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}
	grant, err := eng.AuthorizeTool(ctx, token, name)
	if err != nil {
		if errors.Is(err, engine.ErrNotAllowed) {
			_ = eng.RecordToolCall(ctx, grant.AttemptID, engine.ToolCall{Tool: name, Status: "denied", Args: args, Error: err.Error()})
		}
		return nil, err
	}
	t, ok := reg.tool(name)
	if !ok {
		// Not one of the server's own tools: look among the MCP connections
		// of the workspace this run belongs to, and nowhere else.
		if remote, findErr := eng.ToolForRun(ctx, grant.RunID, name); findErr == nil {
			server := MCPServer{Name: remote.Connection, URL: remote.Endpoint.URL, HTTP: mcpClient, reg: reg, to: remote.Endpoint}
			t, ok = Tool{Description: remote.Description, InputSchema: remote.InputSchema, Class: remote.Class,
				Handler: func(ctx context.Context, c Call) (json.RawMessage, error) {
					return server.call(ctx, remote.Remote, c.Input)
				}}, true
		}
	}
	if !ok {
		err := fmt.Errorf("tool %s is not registered on this server: %w", name, engine.ErrNotFound)
		_ = eng.RecordToolCall(ctx, grant.AttemptID, engine.ToolCall{Tool: name, Class: grant.Class, Status: "failed", Args: args, Error: err.Error()})
		return nil, err
	}
	// A step may list a read tool under write, but never the reverse.
	if grant.Class == engine.ToolRead && t.Class != engine.ToolRead {
		err := fmt.Errorf("tool %s can change things but the step lists it as a read: %w", name, engine.ErrNotAllowed)
		_ = eng.RecordToolCall(ctx, grant.AttemptID, engine.ToolCall{Tool: name, Status: "denied", Args: args, Error: err.Error()})
		return nil, err
	}

	started := time.Now()
	out, err := t.Handler(ctx, Call{Input: args, IdempotencyKey: grant.AttemptID + ":" + name, RunID: grant.RunID, Step: grant.Step})
	rec := engine.ToolCall{Tool: name, Class: t.Class, Status: "succeeded", Args: args, Result: out, Duration: time.Since(started)}
	if err != nil {
		rec.Status, rec.Error = "failed", err.Error()
	}
	if recErr := eng.RecordToolCall(ctx, grant.AttemptID, rec); recErr != nil {
		return nil, fmt.Errorf("record the tool call: %w", recErr)
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrToolFailed, err)
	}
	return out, nil
}

// RegisterHTTPTools adds http.get and http.post. They let an agent reach a
// third party through one of Weir's named connections: the agent names the
// connection and a path, and never sees the address or any credential.
func RegisterHTTPTools(reg *Registry, eng *engine.Engine, client *http.Client) {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second, CheckRedirect: secrets.SameHost}
	}
	schema := func(withBody bool) map[string]any {
		props := map[string]any{
			"connection": map[string]any{"type": "string", "description": "The name of the connection to call."},
			"path":       map[string]any{"type": "string", "description": "The path to request, starting with a slash."},
		}
		if withBody {
			props["body"] = map[string]any{"type": "object", "description": "The JSON body to send."}
		}
		return map[string]any{"type": "object", "required": []string{"connection", "path"}, "properties": props, "additionalProperties": false}
	}
	do := func(method string) Handler {
		return func(ctx context.Context, c Call) (json.RawMessage, error) {
			var in struct {
				Connection string          `json:"connection"`
				Path       string          `json:"path"`
				Body       json.RawMessage `json:"body"`
			}
			if err := json.Unmarshal(c.Input, &in); err != nil {
				return nil, fmt.Errorf("arguments: %w", err)
			}
			if !strings.HasPrefix(in.Path, "/") || strings.Contains(in.Path, "://") {
				return nil, errors.New("path must start with a slash and stay on the connection's own address")
			}
			to, err := eng.Endpoint(ctx, c.RunID, in.Connection)
			if err != nil {
				return nil, err
			}
			req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(to.URL, "/")+in.Path, bytes.NewReader(in.Body))
			if err != nil {
				return nil, err
			}
			// The agent names a connection and never sees its credential.
			if err := reg.sign(req, to); err != nil {
				return nil, err
			}
			if len(in.Body) > 0 {
				req.Header.Set("Content-Type", "application/json")
			}
			req.Header.Set("Idempotency-Key", c.IdempotencyKey)
			resp, err := client.Do(req)
			if err != nil {
				return nil, err
			}
			defer resp.Body.Close()
			raw, _ := io.ReadAll(io.LimitReader(resp.Body, 256<<10))
			var body any = string(raw)
			if json.Valid(raw) {
				body = json.RawMessage(raw)
			}
			return json.Marshal(map[string]any{"status": resp.StatusCode, "body": body})
		}
	}
	reg.RegisterTool("http.get", Tool{Class: engine.ToolRead, InputSchema: schema(false), Handler: do(http.MethodGet),
		Description: "Fetch a path from a named connection with GET. Returns the status code and the body."})
	reg.RegisterTool("http.post", Tool{Class: engine.ToolWrite, InputSchema: schema(true), Handler: do(http.MethodPost),
		Description: "Send a JSON body to a path on a named connection with POST. Returns the status code and the body."})
}
