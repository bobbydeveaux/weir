package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bobbydeveaux/weir/api"
	"github.com/bobbydeveaux/weir/engine"
	"github.com/bobbydeveaux/weir/internal/testdb"
	"github.com/bobbydeveaux/weir/worker"
)

const research = `
weir: "0.1"
namespace: kb
name: research
steps:
  - id: look
    kind: ai
    prompt: look@1
    tools: { read: [kb.search] }
    budget: { usd: 1 }
    output: { schema: { type: object } }
`

// mcpServer plays an MCP server with one read-only tool, and counts its calls.
func mcpServer(t *testing.T, calls *int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string `json:"method"`
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &req)
		result := `{}`
		switch req.Method {
		case "tools/list":
			result = `{"tools":[{"name":"search","description":"Search the pages","inputSchema":{"type":"object"},"annotations":{"readOnlyHint":true}}]}`
		case "tools/call":
			*calls++
			result = `{"content":[{"type":"text","text":"{\"hits\": 3}"}]}`
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":`+result+`}`)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestAnMCPConnectionsToolsBelongToItsWorkspaceOnly(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool := testdb.New(ctx, t)
	eng, err := engine.New(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	reg := worker.NewRegistry()
	eng.HandlerExists, eng.ToolClass = reg.Exists, reg.ToolClass
	srv := httptest.NewServer(api.New(eng, reg))
	defer srv.Close()
	var calls int
	mcp := mcpServer(t, &calls)

	for _, ws := range []string{"acme", "rival"} {
		call(t, srv.URL, "POST", "/v1/workspaces", "", `{"slug": "`+ws+`"}`, nil)
		if code := call(t, srv.URL, "POST", "/v1/templates/look", ws, `{"user": "look"}`, nil); code != http.StatusCreated {
			t.Fatalf("%s: template = %d", ws, code)
		}
	}
	// Only acme connects the server.
	var made struct{ Tools []string }
	if code := call(t, srv.URL, "PUT", "/v1/connections/kb", "acme", `{"kind": "mcp", "base_url": "`+mcp.URL+`"}`, &made); code != http.StatusOK {
		t.Fatalf("connect = %d", code)
	}
	if strings.Join(made.Tools, ",") != "kb.search" {
		t.Fatalf("tools = %v", made.Tools)
	}
	if code := call(t, srv.URL, "PUT", "/v1/connections/gone", "acme", `{"kind": "mcp", "base_url": "http://127.0.0.1:1"}`, nil); code != http.StatusBadGateway {
		t.Errorf("connecting a server that is not there = %d, want 502", code)
	}

	// acme may publish a workflow that uses the tool; rival may not, and cannot see it.
	if code := call(t, srv.URL, "POST", "/v1/definitions", "rival", research, nil); code == http.StatusCreated {
		t.Fatal("rival published a workflow using acme's tool")
	}
	if code := call(t, srv.URL, "POST", "/v1/definitions", "acme", research, nil); code != http.StatusCreated {
		t.Fatalf("acme publish = %d", code)
	}
	names := func(ws string) string {
		var registry struct {
			Tools []struct{ Name, Connection string }
		}
		call(t, srv.URL, "GET", "/v1/registry", ws, "", &registry)
		var out []string
		for _, tool := range registry.Tools {
			out = append(out, tool.Name)
		}
		return strings.Join(out, ",")
	}
	if !strings.Contains(names("acme"), "kb.search") || strings.Contains(names("rival"), "kb.search") {
		t.Fatalf("registry: acme sees %q, rival sees %q", names("acme"), names("rival"))
	}
	var connections []map[string]any
	call(t, srv.URL, "GET", "/v1/connections", "acme", "", &connections)
	if len(connections) != 1 || connections[0]["kind"] != "mcp" || len(connections[0]["tools"].([]any)) != 1 {
		t.Fatalf("connections = %v", connections)
	}

	// A running step in acme reaches the tool through the proxy, and the call is recorded.
	var started struct{ ID string }
	call(t, srv.URL, "POST", "/v1/runs", "acme", `{"namespace": "kb", "name": "research"}`, &started)
	work, err := eng.Claim(ctx, []string{"ai"}, "test", time.Minute)
	if err != nil || work == nil {
		t.Fatalf("claim: %v %v", work, err)
	}
	listed := worker.DescribeFor(ctx, eng, reg, work.RunID, "kb.search")
	if len(listed) != 1 || listed[0].Class != engine.ToolRead {
		t.Fatalf("the step is offered %v", listed)
	}
	out, err := worker.CallTool(ctx, eng, reg, work.Token, "kb.search", json.RawMessage(`{"query": "x"}`))
	if err != nil || !strings.Contains(string(out), `"hits": 3`) || calls != 1 {
		t.Fatalf("call = %s, %v (server saw %d calls)", out, err, calls)
	}
	if _, err := worker.CallTool(ctx, eng, reg, work.Token, "kb.delete", nil); !errors.Is(err, engine.ErrNotAllowed) {
		t.Errorf("an unlisted tool: err = %v, want not allowed", err)
	}
	var record struct {
		ToolCalls []struct{ Tool, Status string } `json:"tool_calls"`
	}
	call(t, srv.URL, "GET", "/v1/runs/"+started.ID+"/record", "acme", "", &record)
	if len(record.ToolCalls) != 2 || record.ToolCalls[0].Status != "succeeded" || record.ToolCalls[1].Status != "denied" {
		t.Errorf("recorded tool calls = %+v", record.ToolCalls)
	}
}
