package api_test

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bobbydeveaux/weir/api"
	"github.com/bobbydeveaux/weir/engine"
	"github.com/bobbydeveaux/weir/internal/testdb"
	"github.com/bobbydeveaux/weir/runner"
	"github.com/bobbydeveaux/weir/worker"
)

// follow opens a stream and sends each event on a channel as "id type", or as
// "moved" for an event that has neither.
func follow(ctx context.Context, t *testing.T, url string, header map[string]string) <-chan string {
	t.Helper()
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	for k, v := range header {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("stream %s = %d %s", url, resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	out := make(chan string, 100)
	go func() {
		defer resp.Body.Close()
		defer close(out)
		id := ""
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if v, ok := strings.CutPrefix(line, "id: "); ok {
				id = v
			}
			if v, ok := strings.CutPrefix(line, "data: "); ok {
				var ev struct{ Type string }
				_ = json.Unmarshal([]byte(v), &ev)
				if ev.Type == "" {
					ev.Type = "moved"
				}
				out <- strings.TrimSpace(id + " " + ev.Type)
				id = ""
			}
		}
	}()
	return out
}

// next waits for one event from a stream.
func next(t *testing.T, ch <-chan string) string {
	t.Helper()
	select {
	case v, ok := <-ch:
		if !ok {
			t.Fatal("the stream ended")
		}
		return v
	case <-time.After(10 * time.Second):
		t.Fatal("no event arrived on the stream")
		return ""
	}
}

func TestARunCanBeFollowedAsAStream(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool := testdb.New(ctx, t)
	eng, err := engine.New(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = eng.Listen(ctx) }()
	reg := worker.NewRegistry()
	eng.HandlerExists = reg.Exists
	srv := httptest.NewServer(api.New(eng, reg))
	defer srv.Close()
	defer srv.CloseClientConnections() // streams stay open until someone hangs up

	call(t, srv.URL, "POST", "/v1/workspaces", "", `{"slug": "acme"}`, nil)
	const wf = `
weir: "0.1"
namespace: shop
name: signoff
steps:
  - id: approve
    kind: human
    assign: { role: lead }
  - id: done
    kind: deterministic
    handler: core.noop
`
	call(t, srv.URL, "POST", "/v1/definitions", "acme", wf, nil)
	var started struct{ ID string }
	call(t, srv.URL, "POST", "/v1/runs", "acme", `{"namespace": "shop", "name": "signoff"}`, &started)

	// The workspace is named in the address, as a browser's stream must.
	live := follow(ctx, t, srv.URL+"/v1/runs/"+started.ID+"/stream?workspace=acme", nil)
	workspace := follow(ctx, t, srv.URL+"/v1/stream?workspace=acme", nil)
	other := follow(ctx, t, srv.URL+"/v1/stream", nil) // the default workspace

	// What has already happened comes first, in order.
	if a, b := next(t, live), next(t, live); a != "1 run.started" || b != "2 step.waiting" {
		t.Fatalf("the stream began %q, %q", a, b)
	}

	// What happens next arrives without asking.
	var tasks []map[string]any
	call(t, srv.URL, "GET", "/v1/tasks", "acme", "", &tasks)
	call(t, srv.URL, "POST", "/v1/tasks/"+tasks[0]["id"].(string)+"/decision", "acme", `{"outcome": "approved"}`, nil)
	if got := next(t, live); got != "3 step.succeeded" {
		t.Fatalf("after the decision the stream sent %q", got)
	}
	if got := next(t, workspace); got != "moved" {
		t.Errorf("the workspace stream sent %q", got)
	}

	w := &worker.Worker{Engine: eng, Runner: &runner.Fake{}, Handlers: reg}
	for i := 0; i < 4; i++ {
		if _, err := w.RunOnce(ctx); err != nil {
			t.Fatal(err)
		}
	}
	var last string
	for last != "run.succeeded" {
		_, last, _ = strings.Cut(next(t, live), " ")
	}

	// A client that reconnects says where it got to and is sent only what it missed.
	resumed := follow(ctx, t, srv.URL+"/v1/runs/"+started.ID+"/stream?workspace=acme", map[string]string{"Last-Event-ID": "3"})
	if got := next(t, resumed); !strings.HasPrefix(got, "4 ") {
		t.Errorf("resuming after 3 began with %q", got)
	}

	// Another workspace hears nothing of it, and cannot follow the run.
	select {
	case got := <-other:
		t.Errorf("the default workspace's stream heard %q", got)
	case <-time.After(300 * time.Millisecond):
	}
	if code := call(t, srv.URL, "GET", "/v1/runs/"+started.ID+"/stream", "", "", nil); code != http.StatusNotFound {
		t.Errorf("following another workspace's run = %d, want 404", code)
	}
}
