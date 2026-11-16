package worker

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/bobbydeveaux/weir/engine"
)

// MCPServer is a remote MCP server whose tools Weir offers to agents. Agents
// never talk to it themselves: every call still goes through Weir's tool
// proxy, which checks it against the step's list and records it.
type MCPServer struct {
	// Name prefixes the server's tools: a tool "search" on a server named
	// "knowledge" is "knowledge.search" in a workflow.
	Name string
	// URL is the server's streamable HTTP endpoint.
	URL string
	// ReadOnly marks every tool on the server as a read. Otherwise a tool is a
	// read only if the server itself says so (readOnlyHint), and a write if not.
	ReadOnly bool
	// Headers are sent with every request, for example to identify Weir.
	Headers map[string]string
	HTTP    *http.Client
}

// mcpClient is used to reach MCP servers when none is given.
var mcpClient = &http.Client{Timeout: 60 * time.Second}

// DiscoverMCP asks an MCP server what tools it offers. A tool is a read only
// if the server itself says so.
func DiscoverMCP(ctx context.Context, url string) ([]engine.ConnectionTool, error) {
	return MCPServer{Name: url, URL: url, HTTP: mcpClient}.discover(ctx)
}

// RegisterMCP asks the server for its tools and adds each to the registry,
// where every workspace can use them. It returns the names it added. For
// tools that belong to one workspace, store an MCP connection instead.
func (r *Registry) RegisterMCP(ctx context.Context, s MCPServer) ([]string, error) {
	if s.HTTP == nil {
		s.HTTP = mcpClient
	}
	tools, err := s.discover(ctx)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, t := range tools {
		class := t.Class
		if s.ReadOnly {
			class = engine.ToolRead
		}
		remote, server := t.Name, s
		name := s.Name + "." + t.Name
		r.RegisterTool(name, Tool{Description: t.Description, InputSchema: t.InputSchema, Class: class,
			Handler: func(ctx context.Context, c Call) (json.RawMessage, error) {
				return server.call(ctx, remote, c.Input)
			}})
		names = append(names, name)
	}
	return names, nil
}

func (s MCPServer) discover(ctx context.Context) ([]engine.ConnectionTool, error) {
	if _, err := s.rpc(ctx, "initialize", map[string]any{
		"protocolVersion": "2025-06-18", "capabilities": map[string]any{},
		"clientInfo": map[string]any{"name": "weir", "version": "0.1"}}); err != nil {
		return nil, fmt.Errorf("MCP server %s: %w", s.Name, err)
	}
	raw, err := s.rpc(ctx, "tools/list", map[string]any{})
	if err != nil {
		return nil, fmt.Errorf("MCP server %s: %w", s.Name, err)
	}
	var listed struct {
		Tools []struct {
			Name        string         `json:"name"`
			Description string         `json:"description"`
			InputSchema map[string]any `json:"inputSchema"`
			Annotations struct {
				ReadOnlyHint bool `json:"readOnlyHint"`
			} `json:"annotations"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(raw, &listed); err != nil {
		return nil, fmt.Errorf("MCP server %s: its tool list was not understood: %w", s.Name, err)
	}
	out := make([]engine.ConnectionTool, 0, len(listed.Tools))
	for _, t := range listed.Tools {
		class := engine.ToolWrite
		if t.Annotations.ReadOnlyHint {
			class = engine.ToolRead
		}
		out = append(out, engine.ConnectionTool{Name: t.Name, Description: t.Description, Class: class, InputSchema: t.InputSchema})
	}
	return out, nil
}

// call runs one tool on the server and returns what it said as JSON.
func (s MCPServer) call(ctx context.Context, tool string, args json.RawMessage) (json.RawMessage, error) {
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}
	raw, err := s.rpc(ctx, "tools/call", map[string]any{"name": tool, "arguments": args})
	if err != nil {
		return nil, err
	}
	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		StructuredContent json.RawMessage `json:"structuredContent"`
		IsError           bool            `json:"isError"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("the tool's reply was not understood: %w", err)
	}
	var text []string
	for _, c := range result.Content {
		if c.Type == "text" {
			text = append(text, c.Text)
		}
	}
	joined := strings.Join(text, "\n")
	if result.IsError {
		return nil, errors.New(joined)
	}
	if len(result.StructuredContent) > 0 && string(result.StructuredContent) != "null" {
		return result.StructuredContent, nil
	}
	if json.Valid([]byte(joined)) {
		return json.RawMessage(joined), nil
	}
	out, _ := json.Marshal(joined)
	return out, nil
}

// rpc sends one JSON-RPC request and returns its result.
func (s MCPServer) rpc(ctx context.Context, method string, params any) (json.RawMessage, error) {
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.URL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	for k, v := range s.Headers {
		req.Header.Set(k, v)
	}
	resp, err := s.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s returned %d", method, resp.StatusCode)
	}
	// A server may answer as plain JSON or as a one-message event stream.
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		scanner := bufio.NewScanner(bytes.NewReader(raw))
		scanner.Buffer(make([]byte, 0, 64<<10), 4<<20)
		raw = nil
		for scanner.Scan() {
			if data, ok := strings.CutPrefix(scanner.Text(), "data:"); ok {
				raw = []byte(strings.TrimSpace(data))
			}
		}
	}
	var reply struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &reply); err != nil {
		return nil, fmt.Errorf("%s: the reply was not JSON", method)
	}
	if reply.Error != nil {
		return nil, fmt.Errorf("%s: %s", method, reply.Error.Message)
	}
	return reply.Result, nil
}
