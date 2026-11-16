package worker

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bobbydeveaux/weir/engine"
)

func TestRemoteMCPToolsJoinTheRegistry(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string `json:"method"`
			Params struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			} `json:"params"`
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &req)
		result := `{}`
		switch req.Method {
		case "tools/list":
			result = `{"tools":[{"name":"search","description":"Search pages","inputSchema":{"type":"object"}},
				{"name":"peek","annotations":{"readOnlyHint":true}}]}`
		case "tools/call":
			if req.Params.Name == "missing" {
				result = `{"content":[{"type":"text","text":"no such page"}],"isError":true}`
			} else {
				text, _ := json.Marshal(`{"hits": 2, "asked": ` + string(req.Params.Arguments) + `}`)
				result = `{"content":[{"type":"text","text":` + string(text) + `}]}`
			}
		}
		// Answer as an event stream, the harder of the two forms to read.
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":"+strings.ReplaceAll(result, "\n", "")+"}\n\n")
	}))
	defer srv.Close()

	reg := NewRegistry()
	names, err := reg.RegisterMCP(context.Background(), MCPServer{Name: "knowledge", URL: srv.URL})
	if err != nil {
		t.Fatalf("RegisterMCP: %v", err)
	}
	if strings.Join(names, ",") != "knowledge.search,knowledge.peek" {
		t.Fatalf("names = %v", names)
	}
	// A tool is a write unless the server says it only reads.
	if class, _ := reg.ToolClass("knowledge.search"); class != engine.ToolWrite {
		t.Errorf("search is %q, want a write", class)
	}
	if class, _ := reg.ToolClass("knowledge.peek"); class != engine.ToolRead {
		t.Errorf("peek is %q, want a read", class)
	}
	tool, _ := reg.tool("knowledge.search")
	out, err := tool.Handler(context.Background(), Call{Input: json.RawMessage(`{"query": "shortcodes"}`)})
	if err != nil || !strings.Contains(string(out), `"hits": 2`) || !strings.Contains(string(out), "shortcodes") {
		t.Fatalf("call = %s, %v", out, err)
	}

	// A whole server can be declared read-only, and a tool's own error comes back as an error.
	if _, err := reg.RegisterMCP(context.Background(), MCPServer{Name: "kb", URL: srv.URL, ReadOnly: true}); err != nil {
		t.Fatal(err)
	}
	if class, _ := reg.ToolClass("kb.search"); class != engine.ToolRead {
		t.Errorf("kb.search is %q, want a read", class)
	}
	failing := MCPServer{Name: "kb", URL: srv.URL, HTTP: http.DefaultClient}
	if _, err := failing.call(context.Background(), "missing", nil); err == nil || !strings.Contains(err.Error(), "no such page") {
		t.Errorf("err = %v, want the tool's own message", err)
	}
}
