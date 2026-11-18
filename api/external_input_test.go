package api_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bobbydeveaux/weir/engine"
	"github.com/bobbydeveaux/weir/internal/testdb"
	"github.com/bobbydeveaux/weir/worker"
)

// An external step says what it sends as input, like every other kind of
// step. It used to be read only from call.body, and input was dropped
// without a word.
func TestAnExternalStepSendsItsInput(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	var mu sync.Mutex
	got := map[string]string{}
	app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		got[r.URL.Path] = string(raw)
		mu.Unlock()
		_, _ = io.WriteString(w, `{"ok": true}`)
	}))
	t.Cleanup(app.Close)
	eng, err := engine.New(ctx, testdb.New(ctx, t))
	if err != nil {
		t.Fatal(err)
	}
	if err := eng.PutConnection(ctx, "app", app.URL, engine.Auth{}); err != nil {
		t.Fatal(err)
	}
	step := func(id, path, given string) string {
		return `
  - id: ` + id + `
    kind: external
    connection: app
    call: { path: ` + path + given + ` }
    resume: poll
    poll: { until: "response.ok" }`
	}
	definition := `
weir: "0.1"
namespace: docs
name: send
steps:` + strings.Replace(step("as_input", "/input", ""), "    resume: poll", "    input: { order: input.order, note: '\"sent as input\"' }\n    resume: poll", 1) +
		step("as_body", "/body", `, body: { order: input.order, note: '"sent as body"' }`) +
		step("nothing", "/nothing", "")
	if _, err := eng.Publish(ctx, []byte(definition), "test"); err != nil {
		t.Fatalf("publish: %v", err)
	}
	// Saying it twice is refused, so that neither is quietly dropped.
	twice := strings.Replace(definition, `call: { path: /input }`, `call: { path: /input, body: { a: input.order } }`, 1)
	if _, err := eng.Publish(ctx, []byte(twice), "test"); err == nil || !strings.Contains(err.Error(), "said twice") {
		t.Errorf("input and call.body together = %v, want it refused", err)
	}

	runID, _, err := eng.StartRun(ctx, engine.StartRequest{Namespace: "docs", Name: "send", Requester: "alice", Input: json.RawMessage(`{"order": "A-17"}`)})
	if err != nil {
		t.Fatal(err)
	}
	w := &worker.Worker{Engine: eng, Handlers: worker.NewRegistry()}
	for {
		found, err := w.RunOnce(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !found {
			break
		}
	}
	if v, err := eng.Run(ctx, runID); err != nil || v.Status != "succeeded" {
		t.Fatalf("run = %s, %v", mustJSON(v), err)
	}
	mu.Lock()
	defer mu.Unlock()
	for path, note := range map[string]string{"/input": "sent as input", "/body": "sent as body"} {
		var sent struct{ Order, Note string }
		if err := json.Unmarshal([]byte(got[path]), &sent); err != nil || sent.Order != "A-17" || sent.Note != note {
			t.Errorf("%s was sent %q", path, got[path])
		}
	}
	if got["/nothing"] != "{}" {
		t.Errorf("a step given nothing sent %q", got["/nothing"])
	}
}
