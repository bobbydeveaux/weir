// Package mcpbridge is a small MCP server that an agent's command line starts
// as a child process. It speaks MCP over standard input and output, and passes
// every tool call to Weir's API. It holds no tools of its own, so the only
// things an agent can do are the things Weir allows that attempt to do.
package mcpbridge

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Version is reported to the client.
const Version = "0.1"

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// described is a tool as Weir's API lists it.
type described struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

// Bridge serves MCP for one attempt.
type Bridge struct {
	// BaseURL is the address of Weir's API.
	BaseURL string
	// Token is the attempt's token.
	Token  string
	Client *http.Client

	// real maps the name an agent sees to the tool's name in Weir.
	real map[string]string
}

// SafeName turns a Weir tool name into one every MCP client accepts.
func SafeName(name string) string { return strings.NewReplacer(".", "_", "/", "_").Replace(name) }

// Serve reads requests from in and writes replies to out until in ends.
func (b *Bridge) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	if b.Client == nil {
		b.Client = http.DefaultClient
	}
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 64<<10), 8<<20)
	enc := json.NewEncoder(out)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var req request
		if err := json.Unmarshal(line, &req); err != nil {
			_ = enc.Encode(response{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{-32700, "the message was not valid JSON"}})
			continue
		}
		if len(req.ID) == 0 {
			continue // a notification: nothing to answer
		}
		res := response{JSONRPC: "2.0", ID: req.ID}
		result, err := b.handle(ctx, req)
		if err != nil {
			res.Error = err
		} else {
			res.Result = result
		}
		if err := enc.Encode(res); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func (b *Bridge) handle(ctx context.Context, req request) (any, *rpcError) {
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &p)
		if p.ProtocolVersion == "" {
			p.ProtocolVersion = "2025-06-18"
		}
		return map[string]any{
			"protocolVersion": p.ProtocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "weir", "version": Version},
		}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		tools, err := b.list(ctx)
		if err != nil {
			return nil, &rpcError{-32603, err.Error()}
		}
		return map[string]any{"tools": tools}, nil
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, &rpcError{-32602, "the tool call was not understood"}
		}
		text, failed := b.call(ctx, p.Name, p.Arguments)
		return map[string]any{"content": []map[string]any{{"type": "text", "text": text}}, "isError": failed}, nil
	}
	return nil, &rpcError{-32601, "method " + req.Method + " is not supported"}
}

func (b *Bridge) list(ctx context.Context) ([]map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.BaseURL+"/v1/tools/"+b.Token, nil)
	if err != nil {
		return nil, err
	}
	resp, err := b.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("listing tools returned %d", resp.StatusCode)
	}
	var tools []described
	if err := json.NewDecoder(resp.Body).Decode(&tools); err != nil {
		return nil, err
	}
	b.real = map[string]string{}
	out := make([]map[string]any, 0, len(tools))
	for _, t := range tools {
		safe := SafeName(t.Name)
		b.real[safe] = t.Name
		schema := t.InputSchema
		if schema == nil {
			schema = map[string]any{"type": "object"}
		}
		out = append(out, map[string]any{"name": safe, "description": t.Description, "inputSchema": schema})
	}
	return out, nil
}

// call runs one tool through Weir. It returns the text to show the agent and
// whether the call failed. A refusal is reported to the agent as a failed
// call, so it can carry on without the tool.
func (b *Bridge) call(ctx context.Context, name string, args json.RawMessage) (string, bool) {
	if b.real == nil {
		if _, err := b.list(ctx); err != nil {
			return "The tools could not be listed: " + err.Error(), true
		}
	}
	real, ok := b.real[name]
	if !ok {
		return "There is no tool called " + name + ".", true
	}
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}
	body, _ := json.Marshal(map[string]any{"name": real, "arguments": args})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.BaseURL+"/v1/tools/"+b.Token+"/call", bytes.NewReader(body))
	if err != nil {
		return err.Error(), true
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := b.Client.Do(req)
	if err != nil {
		return "The tool could not be reached: " + err.Error(), true
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var reply struct {
		Result json.RawMessage `json:"result"`
		Error  string          `json:"error"`
	}
	_ = json.Unmarshal(raw, &reply)
	if resp.StatusCode != http.StatusOK {
		if reply.Error == "" {
			reply.Error = fmt.Sprintf("the call returned %d", resp.StatusCode)
		}
		return reply.Error, true
	}
	return string(reply.Result), false
}
