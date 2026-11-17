package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/bobbydeveaux/weir/api"
	"github.com/bobbydeveaux/weir/engine"
	"github.com/bobbydeveaux/weir/internal/testdb"
	"github.com/bobbydeveaux/weir/worker"
)

func scheduled(triggers string) string {
	return `
weir: "0.1"
namespace: ops
name: morning
` + triggers + `
steps:
  - id: note
    kind: deterministic
    handler: core.echo
`
}

func TestAScheduleStartsARunWhenItIsDue(t *testing.T) {
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
	call(t, srv.URL, "POST", "/v1/workspaces", "", `{"slug": "acme"}`, nil)

	// Things that are not schedules are refused when published.
	for _, bad := range []string{
		"triggers:\n  - schedule: every morning",
		"triggers:\n  - schedule: \"61 * * * *\"",
		"triggers:\n  - schedule: \"0 7 * * *\"\n    event: file.uploaded",
		"triggers:\n  - schedule: \"0 7 * * *\"\n    when: event.data.x == 1",
	} {
		if code := call(t, srv.URL, "POST", "/v1/definitions", "acme", scheduled(bad), nil); code == http.StatusCreated {
			t.Errorf("published a workflow with %q", bad)
		}
	}

	weekdays := "triggers:\n  - schedule: \"0 7 * * 1-5\"\n    input: { due: event.data.at, why: '\"the schedule\"' }"
	if code := call(t, srv.URL, "POST", "/v1/definitions", "acme", scheduled(weekdays), nil); code != http.StatusCreated {
		t.Fatalf("publish = %d", code)
	}
	var triggers []map[string]any
	call(t, srv.URL, "GET", "/v1/triggers", "acme", "", &triggers)
	if len(triggers) != 1 || triggers[0]["schedule"] != "0 7 * * 1-5" || triggers[0]["next_at"] == nil {
		t.Fatalf("triggers = %v, want the schedule with the time it is next due", triggers)
	}

	// Nothing is due yet.
	if swept, err := eng.Sweep(ctx); err != nil || swept.Scheduled != 0 {
		t.Fatalf("sweep before it is due: %+v %v", swept, err)
	}
	var runs []map[string]any
	call(t, srv.URL, "GET", "/v1/runs", "acme", "", &runs)
	if len(runs) != 0 {
		t.Fatalf("a run started before the schedule was due: %v", runs)
	}

	// Make it due, as time would, and sweep from several places at once.
	due := time.Now().Add(-time.Minute).UTC().Truncate(time.Second)
	if _, err := pool.Exec(ctx, `UPDATE schedules SET next_at = $1`, due); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := eng.Sweep(ctx); err != nil {
				t.Errorf("sweep: %v", err)
			}
		}()
	}
	wg.Wait()

	call(t, srv.URL, "GET", "/v1/runs", "acme", "", &runs)
	if len(runs) != 1 || runs[0]["requester"] != "schedule" {
		t.Fatalf("after four sweeps there are %d runs: %v; want exactly one, started by the schedule", len(runs), runs)
	}
	var run struct{ Input map[string]any }
	call(t, srv.URL, "GET", "/v1/runs/"+runs[0]["id"].(string), "acme", "", &run)
	if run.Input["why"] != "the schedule" || run.Input["due"] != due.Format(time.RFC3339) {
		t.Errorf("the run's input = %v, want the time it was due (%s)", run.Input, due.Format(time.RFC3339))
	}
	// It has moved on to a time in the future, and the default workspace saw none of it.
	var next time.Time
	if err := pool.QueryRow(ctx, `SELECT next_at FROM schedules`).Scan(&next); err != nil || !next.After(time.Now()) {
		t.Errorf("the schedule is next due at %s (%v), want a time in the future", next, err)
	}
	call(t, srv.URL, "GET", "/v1/runs", "", "", &runs)
	if len(runs) != 0 {
		t.Errorf("the default workspace has the scheduled run: %v", runs)
	}

	// Publishing the workflow without its schedule stops it.
	if code := call(t, srv.URL, "POST", "/v1/definitions", "acme", scheduled("triggers:\n  - event: ops.wake"), nil); code != http.StatusCreated {
		t.Fatalf("republish = %d", code)
	}
	var left int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM schedules`).Scan(&left); err != nil || left != 0 {
		t.Errorf("%d schedule(s) remain after the workflow was published without one (%v)", left, err)
	}
}
