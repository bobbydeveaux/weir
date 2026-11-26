package worker_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bobbydeveaux/weir/engine"
	"github.com/bobbydeveaux/weir/internal/testdb"
	"github.com/bobbydeveaux/weir/spec"
	"github.com/bobbydeveaux/weir/worker"
)

// gauge records how many steps a handler is running at once, the most it
// has seen, and how many times each run's step was called.
type gauge struct {
	mu      sync.Mutex
	now     int
	most    int
	calls   map[string]int
	started chan struct{}
}

func newGauge() *gauge {
	return &gauge{calls: map[string]int{}, started: make(chan struct{}, 100)}
}

// handler runs for hold, or until release is closed when hold is zero.
func (g *gauge) handler(hold time.Duration, release <-chan struct{}) worker.Handler {
	return func(ctx context.Context, c worker.Call) (json.RawMessage, error) {
		g.mu.Lock()
		g.now++
		g.most = max(g.most, g.now)
		g.calls[c.RunID]++
		g.mu.Unlock()
		g.started <- struct{}{}
		defer func() {
			g.mu.Lock()
			g.now--
			g.mu.Unlock()
		}()
		if hold > 0 {
			timer := time.NewTimer(hold)
			defer timer.Stop()
			select {
			case <-timer.C:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			return json.RawMessage(`{}`), nil
		}
		select {
		case <-release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return json.RawMessage(`{}`), nil
	}
}

const slow = `
weir: "0.1"
namespace: test
name: slow
steps:
  - id: work
    kind: deterministic
    handler: test.slow
`

const quick = `
weir: "0.1"
namespace: test
name: quick
steps:
  - id: work
    kind: deterministic
    handler: core.noop
`

type bench struct {
	t    *testing.T
	ctx  context.Context
	pool *pgxpool.Pool
	eng  *engine.Engine
	reg  *worker.Registry
}

func newBench(t *testing.T, h worker.Handler) *bench {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	pool := testdb.New(ctx, t)
	eng, err := engine.New(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	reg := worker.NewRegistry()
	reg.Register("test.slow", h)
	eng.HandlerExists = reg.Exists
	for _, def := range []string{slow, quick} {
		if _, err := eng.Publish(ctx, []byte(def), "test"); err != nil {
			t.Fatalf("publish: %v", err)
		}
	}
	return &bench{t: t, ctx: ctx, pool: pool, eng: eng, reg: reg}
}

func (b *bench) start(name string, n int) []string {
	b.t.Helper()
	var ids []string
	for range n {
		id, _, err := b.eng.StartRun(b.ctx, engine.StartRequest{Namespace: "test", Name: name, Requester: "test", Input: json.RawMessage(`{}`)})
		if err != nil {
			b.t.Fatalf("start: %v", err)
		}
		ids = append(ids, id)
	}
	return ids
}

// serve runs w.Run in the background, and returns a function that stops it
// and returns what Run returned.
func (b *bench) serve(w *worker.Worker) func() error {
	ctx, cancel := context.WithCancel(b.ctx)
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	var once sync.Once
	var err error
	return func() error {
		once.Do(func() {
			cancel()
			err = <-done
		})
		return err
	}
}

// waitFor polls until every run has the status, or fails the test.
func (b *bench) waitFor(status string, ids []string) {
	b.t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for _, id := range ids {
		for {
			run, err := b.eng.Run(b.ctx, id)
			if err != nil {
				b.t.Fatal(err)
			}
			if run.Status == status {
				break
			}
			if time.Now().After(deadline) {
				b.t.Fatalf("run %s is %s; want %s", id, run.Status, status)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
}

// attempts counts the attempts at every step in the schema, by status.
func (b *bench) attempts() map[string]int {
	b.t.Helper()
	rows, err := b.pool.Query(b.ctx, `SELECT status, COUNT(*) FROM attempts GROUP BY status`)
	if err != nil {
		b.t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var status string
		var n int
		if err := rows.Scan(&status, &n); err != nil {
			b.t.Fatal(err)
		}
		out[status] = n
	}
	return out
}

func TestAWorkerRunsUpToItsConcurrencyOfStepsAtOnce(t *testing.T) {
	g := newGauge()
	b := newBench(t, g.handler(400*time.Millisecond, nil))
	ids := b.start("slow", 12)

	w := &worker.Worker{Engine: b.eng, Handlers: b.reg, ID: "busy", Concurrency: 4}
	began := time.Now()
	stop := b.serve(w)
	b.waitFor("succeeded", ids)
	took := time.Since(began)
	if err := stop(); err != nil {
		t.Fatalf("Run: %v", err)
	}

	g.mu.Lock()
	defer g.mu.Unlock()
	if g.most != 4 {
		t.Errorf("at most %d steps ran at once; want 4", g.most)
	}
	if len(g.calls) != len(ids) {
		t.Errorf("%d runs' steps were called; want %d", len(g.calls), len(ids))
	}
	for run, n := range g.calls {
		if n != 1 {
			t.Errorf("run %s's step was called %d times; want once", run, n)
		}
	}
	if got := b.attempts(); got["succeeded"] != len(ids) || len(got) != 1 {
		t.Errorf("attempts by status = %v; want %d succeeded and nothing else", got, len(ids))
	}
	// Twelve steps of 400 ms, four at a time, take three rounds. One at a
	// time they would take 4.8 seconds.
	if took > 3*time.Second {
		t.Errorf("twelve 400 ms steps four at a time took %s", took)
	}
}

func TestAWorkerRunsOneStepAtATimeByDefault(t *testing.T) {
	g := newGauge()
	b := newBench(t, g.handler(150*time.Millisecond, nil))
	ids := b.start("slow", 4)

	stop := b.serve(&worker.Worker{Engine: b.eng, Handlers: b.reg, ID: "single"})
	b.waitFor("succeeded", ids)
	if err := stop(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.most != 1 {
		t.Errorf("%d steps ran at once with no concurrency set; want 1", g.most)
	}
}

func TestEveryConcurrentStepKeepsItsLease(t *testing.T) {
	g := newGauge()
	b := newBench(t, g.handler(1500*time.Millisecond, nil))
	ids := b.start("slow", 3)

	// Each step runs five times as long as its lease, and the worker sweeps
	// every second: a step whose heartbeats stopped would be abandoned and
	// run again.
	w := &worker.Worker{Engine: b.eng, Handlers: b.reg, ID: "beating", Concurrency: 3, Lease: 300 * time.Millisecond}
	stop := b.serve(w)
	b.waitFor("succeeded", ids)
	if err := stop(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.most != 3 {
		t.Errorf("at most %d steps ran at once; want 3", g.most)
	}
	for run, n := range g.calls {
		if n != 1 {
			t.Errorf("run %s's step was called %d times; want once", run, n)
		}
	}
	if got := b.attempts(); got["succeeded"] != 3 || len(got) != 1 {
		t.Errorf("attempts by status = %v; want 3 succeeded and none abandoned", got)
	}
}

func TestABusyWorkerStillSweeps(t *testing.T) {
	g := newGauge()
	release := make(chan struct{})
	b := newBench(t, g.handler(0, release))

	// Another worker claims a step and vanishes, its lease short.
	lost := b.start("quick", 1)
	ghost, err := b.eng.Claim(b.ctx, []string{spec.KindDeterministic}, "ghost", 100*time.Millisecond)
	if err != nil || ghost == nil {
		t.Fatalf("the ghost's claim = %v, %v", ghost, err)
	}

	// This worker's only slot is taken by a step that will not end until it
	// is told to.
	busy := b.start("slow", 1)
	stop := b.serve(&worker.Worker{Engine: b.eng, Handlers: b.reg, ID: "busy", Concurrency: 1})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
		_ = stop()
	}()
	select {
	case <-g.started:
	case <-time.After(10 * time.Second):
		t.Fatal("the slow step never started")
	}

	deadline := time.Now().Add(5 * time.Second)
	for b.attempts()["abandoned"] != 1 {
		if time.Now().After(deadline) {
			t.Fatalf("attempts by status = %v while the worker was busy; want the ghost's abandoned", b.attempts())
		}
		time.Sleep(50 * time.Millisecond)
	}
	g.mu.Lock()
	stillRunning := g.now == 1
	g.mu.Unlock()
	if !stillRunning {
		t.Fatal("the slow step had finished before the sweep acted; the test proves nothing")
	}

	close(release)
	b.waitFor("succeeded", append(lost, busy...))
	if err := stop(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if err := b.eng.Complete(b.ctx, ghost.Token, engine.Result{Output: json.RawMessage(`{}`)}); !errors.Is(err, engine.ErrStaleToken) {
		t.Errorf("the ghost completing after its lease ran out = %v; want ErrStaleToken", err)
	}
}
