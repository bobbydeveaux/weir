package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

// notifyChannel is the Postgres channel on which a committed run event is announced.
const notifyChannel = "weir_run_events"

// hub passes "something happened to this run" from Postgres to whoever is
// streaming. It carries no data: a listener that is told a run moved reads the
// run's event log, which is the only source of truth.
type hub struct {
	mu   sync.Mutex
	subs map[chan Moved]struct{}
}

// Moved says a run in a workspace has a new event.
type Moved struct {
	Workspace string
	RunID     string
}

func (h *hub) publish(m Moved) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- m:
		default: // a slow listener misses a nudge, not an event: it catches up on its next read
		}
	}
}

// Subscribe returns a channel that is sent to each time a run in this
// engine's workspace has a new event, and a function that ends it. Nothing
// arrives unless Listen is running; a caller must not rely on it alone.
func (e *Engine) Subscribe() (<-chan Moved, func()) {
	all := make(chan Moved, 64)
	mine := make(chan Moved, 64)
	e.hub.mu.Lock()
	e.hub.subs[all] = struct{}{}
	e.hub.mu.Unlock()
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-done:
				return
			case m := <-all:
				if m.Workspace == e.workspace {
					select {
					case mine <- m:
					default:
					}
				}
			}
		}
	}()
	return mine, func() {
		e.hub.mu.Lock()
		delete(e.hub.subs, all)
		e.hub.mu.Unlock()
		close(done)
	}
}

// Listen waits on Postgres for announcements of new run events and passes
// them to subscribers, until ctx ends. It holds one database connection. If
// the connection drops it is opened again.
func (e *Engine) Listen(ctx context.Context) error {
	for ctx.Err() == nil {
		err := e.listenOnce(ctx)
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(2 * time.Second):
			}
		}
	}
	return nil
}

func (e *Engine) listenOnce(ctx context.Context) error {
	conn, err := e.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "LISTEN "+notifyChannel); err != nil {
		return err
	}
	for {
		n, err := conn.Conn().WaitForNotification(ctx)
		if err != nil {
			return err
		}
		if workspace, run, ok := strings.Cut(n.Payload, " "); ok {
			e.hub.publish(Moved{Workspace: workspace, RunID: run})
		}
	}
}

// announce tells listeners, when the transaction commits, that the run has a
// new event. Postgres delivers nothing if the transaction rolls back.
func (e *Engine) announce(ctx context.Context, tx pgx.Tx, run *runRow) error {
	_, err := tx.Exec(ctx, `SELECT pg_notify($1, $2)`, notifyChannel, run.workspace+" "+run.id)
	return err
}

// EventsAfter returns a run's events with a sequence number above after, in
// order. It is what a stream sends: ask again with the last number seen and
// nothing is missed or repeated.
func (e *Engine) EventsAfter(ctx context.Context, runID string, after int) ([]EventView, error) {
	var exists bool
	err := e.pool.QueryRow(ctx, `SELECT true FROM runs WHERE id = $1::uuid AND workspace_id = $2::uuid`, runID, e.workspace).Scan(&exists)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("run %s: %w", runID, ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	rows, err := e.pool.Query(ctx, `
		SELECT e.seq, e.type, e.actor, COALESCE(s.step_key, ''), e.data::text
		FROM events e LEFT JOIN steps s ON s.id = e.step_id
		WHERE e.run_id = $1::uuid AND e.seq > $2 ORDER BY e.seq`, runID, after)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (EventView, error) {
		var ev EventView
		var data string
		err := row.Scan(&ev.Seq, &ev.Type, &ev.Actor, &ev.Step, &data)
		ev.Data = json.RawMessage(data)
		return ev, err
	})
}
