package api_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bobbydeveaux/weir/api"
	"github.com/bobbydeveaux/weir/engine"
	"github.com/bobbydeveaux/weir/internal/testdb"
	"github.com/bobbydeveaux/weir/worker"
)

// triggered is a one-step workflow with the given triggers block.
func triggered(name, triggers string) string {
	return fmt.Sprintf(`
weir: "0.1"
namespace: files
name: %s
%s
steps:
  - id: note
    kind: deterministic
    handler: core.echo
    input: { got: input }
`, name, triggers)
}

func TestAnEventStartsEveryWorkflowWaitingForIt(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool := testdb.New(ctx, t)
	eng, err := engine.New(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	reg := worker.NewRegistry()
	eng.HandlerExists = reg.Exists
	srv := httptest.NewServer(api.New(eng, reg))
	defer srv.Close()

	publish := func(ws, source string) {
		t.Helper()
		if code := call(t, srv.URL, "POST", "/v1/definitions", ws, source, nil); code != http.StatusCreated {
			t.Fatalf("publish in %q = %d\n%s", ws, code, source)
		}
	}
	// Two workflows want uploads: one all of them, one only PDFs, reshaping the input.
	publish("", triggered("archive", "triggers:\n  - event: file.uploaded"))
	publish("", triggered("read-pdf", `triggers:
  - event: file.uploaded
    when: event.data.type == "pdf"
    input: { document: event.data.name, from: event.source }`))
	publish("", triggered("unrelated", "triggers:\n  - event: file.deleted"))
	publish("", triggered("by-hand", ""))
	// Another workspace wants the same event; it must not hear this one.
	call(t, srv.URL, "POST", "/v1/workspaces", "", `{"slug": "rival"}`, nil)
	publish("rival", triggered("spy", "triggers:\n  - event: file.uploaded"))

	if code := call(t, srv.URL, "POST", "/v1/definitions", "", triggered("bad", "triggers:\n  - event: Not Valid"), nil); code == http.StatusCreated {
		t.Error("a trigger with a malformed event name was published")
	}

	type result struct {
		ID        string
		Duplicate bool
		Runs      []struct {
			Namespace, Name, Error string
			RunID                  string `json:"run_id"`
		}
	}
	var pdf result
	if code := call(t, srv.URL, "POST", "/v1/events", "", `{"type": "file.uploaded", "id": "f-1", "source": "portal",
		"data": {"name": "treaty.pdf", "type": "pdf"}}`, &pdf); code != http.StatusAccepted {
		t.Fatalf("emit = %d", code)
	}
	if len(pdf.Runs) != 2 || pdf.Runs[0].Name != "archive" || pdf.Runs[1].Name != "read-pdf" || pdf.Runs[0].RunID == "" || pdf.Runs[1].Error != "" {
		t.Fatalf("a PDF upload started %+v, want archive and read-pdf", pdf.Runs)
	}

	// The reshaped input reached the run; the plain trigger passed the data through.
	var run struct{ Input map[string]any }
	call(t, srv.URL, "GET", "/v1/runs/"+pdf.Runs[1].RunID, "", "", &run)
	if run.Input["document"] != "treaty.pdf" || run.Input["from"] != "portal" {
		t.Errorf("read-pdf's input = %v", run.Input)
	}
	call(t, srv.URL, "GET", "/v1/runs/"+pdf.Runs[0].RunID, "", "", &run)
	if run.Input["name"] != "treaty.pdf" {
		t.Errorf("archive's input = %v", run.Input)
	}

	// The same event again starts nothing new and reports the same runs.
	var again result
	call(t, srv.URL, "POST", "/v1/events", "", `{"type": "file.uploaded", "id": "f-1", "data": {"name": "treaty.pdf", "type": "pdf"}}`, &again)
	if !again.Duplicate || len(again.Runs) != 2 || again.Runs[0].RunID != pdf.Runs[0].RunID {
		t.Errorf("a repeated event = %+v", again)
	}

	// A text file fails the PDF trigger's condition.
	var txt result
	call(t, srv.URL, "POST", "/v1/events", "", `{"type": "file.uploaded", "id": "f-2", "data": {"name": "notes.txt", "type": "txt"}}`, &txt)
	if len(txt.Runs) != 1 || txt.Runs[0].Name != "archive" {
		t.Errorf("a text upload started %+v, want only archive", txt.Runs)
	}

	// An event nothing waits for is accepted and starts nothing.
	var nothing result
	if code := call(t, srv.URL, "POST", "/v1/events", "", `{"type": "file.renamed"}`, &nothing); code != http.StatusAccepted || len(nothing.Runs) != 0 {
		t.Errorf("an unwanted event = %d %+v", code, nothing)
	}

	var runs, events, triggers []map[string]any
	call(t, srv.URL, "GET", "/v1/runs", "", "", &runs)
	if len(runs) != 3 {
		t.Errorf("the workspace has %d runs, want 3", len(runs))
	}
	call(t, srv.URL, "GET", "/v1/runs", "rival", "", &runs)
	if len(runs) != 0 {
		t.Errorf("the other workspace heard the event: %v", runs)
	}
	call(t, srv.URL, "GET", "/v1/events", "", "", &events)
	if len(events) != 3 || events[0]["type"] != "file.renamed" {
		t.Errorf("events = %v", events)
	}
	call(t, srv.URL, "GET", "/v1/triggers", "", "", &triggers)
	if len(triggers) != 3 {
		t.Errorf("triggers = %v, want three", triggers)
	}
}
