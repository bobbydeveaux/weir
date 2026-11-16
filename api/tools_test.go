package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/bobbydeveaux/weir/api"
	"github.com/bobbydeveaux/weir/engine"
	"github.com/bobbydeveaux/weir/internal/mcpbridge"
	"github.com/bobbydeveaux/weir/internal/testdb"
	"github.com/bobbydeveaux/weir/runner"
	"github.com/bobbydeveaux/weir/worker"
)

const toolWorkflow = `
weir: "0.1"
namespace: shop
name: order-status
steps:
  - id: answer
    kind: ai
    prompt: order-status@1
    tools:
      read: [orders.lookup]
    output:
      schema:
        type: object
        required: [status]
        additionalProperties: false
        properties:
          status: { type: string }
    budget: { usd: 0.50 }
`

// toolSetup returns an engine with a read tool and a write tool registered,
// an API in front of it, and the workflow above published.
func toolSetup(ctx context.Context, t *testing.T) (*engine.Engine, *worker.Registry, *httptest.Server) {
	t.Helper()
	pool := testdb.New(ctx, t)
	eng, err := engine.New(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	reg := worker.NewRegistry()
	reg.RegisterTool("orders.lookup", worker.Tool{
		Class:       engine.ToolRead,
		Description: "Look up an order by its id. Returns its status.",
		InputSchema: map[string]any{"type": "object", "required": []string{"order"},
			"properties": map[string]any{"order": map[string]any{"type": "string"}}},
		Handler: func(_ context.Context, c worker.Call) (json.RawMessage, error) {
			var in struct {
				Order string `json:"order"`
			}
			_ = json.Unmarshal(c.Input, &in)
			if in.Order != "A-17" {
				return nil, errors.New("no such order")
			}
			return json.RawMessage(`{"order": "A-17", "status": "shipped"}`), nil
		},
	})
	reg.RegisterTool("orders.cancel", worker.Tool{Description: "Cancel an order.",
		Handler: func(context.Context, worker.Call) (json.RawMessage, error) {
			return json.RawMessage(`{"cancelled": true}`), nil
		}})
	eng.HandlerExists, eng.ToolClass = reg.Exists, reg.ToolClass

	server := httptest.NewServer(api.New(eng, reg))
	t.Cleanup(server.Close)
	if _, err := eng.PublishTemplate(ctx, "order-status", engine.Template{
		System: "You answer questions about orders. Use the tools you are given to find the answer. Never guess.",
		User:   "What is the status of order {{order}}?",
	}, "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := eng.Publish(ctx, []byte(toolWorkflow), "test"); err != nil {
		t.Fatalf("publish: %v", err)
	}
	return eng, reg, server
}

func TestPublishChecksTheToolsAStepLists(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	eng, _, _ := toolSetup(ctx, t)

	asRead := strings.Replace(toolWorkflow, "read: [orders.lookup]", "read: [orders.cancel]", 1)
	if _, err := eng.Publish(ctx, []byte(asRead), "test"); err == nil || !strings.Contains(err.Error(), "must be listed under write") {
		t.Fatalf("listing a write tool as a read = %v, want it refused", err)
	}
	unknown := strings.Replace(toolWorkflow, "read: [orders.lookup]", "read: [orders.nonsense]", 1)
	if _, err := eng.Publish(ctx, []byte(unknown), "test"); !errors.Is(err, engine.ErrNotFound) {
		t.Fatalf("listing an unknown tool = %v, want ErrNotFound", err)
	}
}

func TestAnAgentReachesOnlyTheToolsItsStepLists(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	eng, _, server := toolSetup(ctx, t)
	runID, _, err := eng.StartRun(ctx, engine.StartRequest{Namespace: "shop", Name: "order-status", Requester: "alice",
		Input: json.RawMessage(`{"order": "A-17"}`)})
	if err != nil {
		t.Fatal(err)
	}
	work, err := eng.Claim(ctx, []string{"ai"}, "test", time.Minute)
	if err != nil || work == nil {
		t.Fatalf("Claim = %v, %v", work, err)
	}

	// Drive the bridge exactly as an agent's command line would.
	ask := func(lines ...string) []map[string]any {
		t.Helper()
		var out bytes.Buffer
		b := &mcpbridge.Bridge{BaseURL: server.URL, Token: work.Token}
		if err := b.Serve(ctx, strings.NewReader(strings.Join(lines, "\n")+"\n"), &out); err != nil {
			t.Fatalf("bridge: %v", err)
		}
		var replies []map[string]any
		for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
			var m map[string]any
			if err := json.Unmarshal([]byte(line), &m); err != nil {
				t.Fatalf("bridge wrote %q: %v", line, err)
			}
			replies = append(replies, m)
		}
		return replies
	}
	replies := ask(
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"orders_lookup","arguments":{"order":"A-17"}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"orders_cancel","arguments":{"order":"A-17"}}}`,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"orders_lookup","arguments":{"order":"Z-99"}}}`,
	)
	if len(replies) != 5 {
		t.Fatalf("the bridge answered %d requests, want 5 (a notification gets no answer): %v", len(replies), replies)
	}
	tools := replies[1]["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 1 || tools[0].(map[string]any)["name"] != "orders_lookup" {
		t.Fatalf("the agent was offered %v, want only orders_lookup", tools)
	}
	text := func(reply map[string]any) (string, bool) {
		res := reply["result"].(map[string]any)
		return res["content"].([]any)[0].(map[string]any)["text"].(string), res["isError"].(bool)
	}
	if got, failed := text(replies[2]); failed || !strings.Contains(got, "shipped") {
		t.Errorf("the allowed call returned %q (failed=%v)", got, failed)
	}
	if got, failed := text(replies[3]); !failed {
		t.Errorf("a tool the step did not list returned %q, want it refused", got)
	}
	if got, failed := text(replies[4]); !failed || !strings.Contains(got, "no such order") {
		t.Errorf("a failing tool returned %q (failed=%v), want its error passed on", got, failed)
	}

	// Going round the bridge, straight to the API, is refused just the same.
	post := func(body string) int {
		t.Helper()
		resp, err := http.Post(server.URL+"/v1/tools/"+work.Token+"/call", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		return resp.StatusCode
	}
	if code := post(`{"name": "orders.cancel", "arguments": {}}`); code != http.StatusForbidden {
		t.Errorf("calling an unlisted tool directly = %d, want 403", code)
	}

	calls, err := eng.ToolCalls(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	var summary []string
	for _, c := range calls {
		summary = append(summary, c.Tool+":"+c.Status)
	}
	if got, want := strings.Join(summary, ","), "orders.lookup:succeeded,orders.lookup:failed,orders.cancel:denied"; got != want {
		t.Errorf("recorded calls = %s, want %s", got, want)
	}

	// Once the attempt is over, its token opens nothing.
	if err := eng.Complete(ctx, work.Token, engine.Result{Output: json.RawMessage(`{"status": "shipped"}`)}); err != nil {
		t.Fatal(err)
	}
	if code := post(`{"name": "orders.lookup", "arguments": {"order": "A-17"}}`); code != http.StatusConflict {
		t.Errorf("a tool call after the attempt finished = %d, want 409", code)
	}
}

func TestTheHTTPToolsGoThroughANamedConnection(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	eng, reg, _ := toolSetup(ctx, t)
	orders := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"path": "`+r.URL.Path+`"}`)
	}))
	defer orders.Close()
	if err := eng.PutConnection(ctx, "orders", orders.URL); err != nil {
		t.Fatal(err)
	}
	worker.RegisterHTTPTools(reg, eng, nil)
	if class, ok := reg.ToolClass("http.get"); !ok || class != engine.ToolRead {
		t.Fatalf("http.get = %s, %v; want a read tool", class, ok)
	}
	if class, _ := reg.ToolClass("http.post"); class != engine.ToolWrite {
		t.Fatalf("http.post = %s; want a write tool", class)
	}

	withHTTP := strings.Replace(toolWorkflow, "read: [orders.lookup]", "read: [http.get]", 1)
	if _, err := eng.Publish(ctx, []byte(withHTTP), "test"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := eng.StartRun(ctx, engine.StartRequest{Namespace: "shop", Name: "order-status", Requester: "alice"}); err != nil {
		t.Fatal(err)
	}
	work, err := eng.Claim(ctx, []string{"ai"}, "test", time.Minute)
	if err != nil || work == nil {
		t.Fatalf("Claim = %v, %v", work, err)
	}
	out, err := worker.CallTool(ctx, eng, reg, work.Token, "http.get", json.RawMessage(`{"connection": "orders", "path": "/orders/A-17"}`))
	if err != nil || !strings.Contains(string(out), "/orders/A-17") {
		t.Fatalf("http.get = %s, %v", out, err)
	}
	// The agent names a connection, never an address.
	if _, err := worker.CallTool(ctx, eng, reg, work.Token, "http.get", json.RawMessage(`{"connection": "orders", "path": "http://elsewhere.example/x"}`)); err == nil {
		t.Error("a path that names another address was accepted")
	}
	if _, err := worker.CallTool(ctx, eng, reg, work.Token, "http.get", json.RawMessage(`{"connection": "nowhere", "path": "/x"}`)); err == nil {
		t.Error("an unknown connection was accepted")
	}
}

// TestAgentUsesAToolLive runs a real agent that must call a tool to answer.
// It runs only when asked: it needs the Claude command line signed in, and a
// built weir binary to act as the bridge.
func TestAgentUsesAToolLive(t *testing.T) {
	bin := os.Getenv("WEIR_TEST_BRIDGE")
	if os.Getenv("WEIR_TEST_CLAUDE") == "" || bin == "" {
		t.Skip("set WEIR_TEST_CLAUDE=1 and WEIR_TEST_BRIDGE=<path to the weir binary> to run a real agent")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	eng, reg, server := toolSetup(ctx, t)
	runID, _, err := eng.StartRun(ctx, engine.StartRequest{Namespace: "shop", Name: "order-status", Requester: "alice",
		Input: json.RawMessage(`{"order": "A-17"}`)})
	if err != nil {
		t.Fatal(err)
	}
	w := &worker.Worker{Engine: eng, Handlers: reg, ToolURL: server.URL,
		Runner: &runner.Claude{Models: map[string]string{"": "haiku"}, Bridge: []string{bin, "mcp"}}}
	if found, err := w.RunOnce(ctx); err != nil || !found {
		t.Fatalf("RunOnce = %v, %v", found, err)
	}

	v, err := eng.Run(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	calls, _ := eng.ToolCalls(ctx, runID)
	t.Logf("run %s, spent $%.4f, output %s, %d tool call(s)", v.Status, v.SpentUSD, v.Output, len(calls))
	for _, c := range calls {
		t.Logf("  tool %s (%s) %s args=%s", c.Tool, c.Class, c.Status, c.Args)
	}
	if v.Status != "succeeded" {
		t.Fatalf("run = %s, error %s", v.Status, v.Error)
	}
	if !strings.Contains(string(v.Output), "shipped") {
		t.Errorf("output = %s, want the status the tool returned", v.Output)
	}
	if len(calls) == 0 || calls[0].Tool != "orders.lookup" || calls[0].Status != "succeeded" {
		t.Errorf("the agent answered without calling the tool: %+v", calls)
	}
}
