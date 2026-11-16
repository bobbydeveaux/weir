package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// A stream is a long-lived response in the Server-Sent Events format. The
// engine is told by Postgres when a run has a new event; as a safeguard a
// stream also looks for itself every few seconds, so it still works if that
// news does not arrive.
const (
	streamCheck     = 3 * time.Second
	streamHeartbeat = 20 * time.Second
)

func startStream(w http.ResponseWriter) (http.Flusher, bool) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		reply(w, http.StatusNotImplemented, map[string]string{"error": "this server cannot stream"})
		return nil, false
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no") // ask proxies not to hold the stream back
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	return flusher, true
}

// Events are sent unnamed, with their type inside the data, so that a browser
// receives every one through a single "message" listener.

// streamRun sends a run's events as they happen. Each carries its sequence
// number as its id, so a client that reconnects with Last-Event-ID (browsers
// do this by themselves) or ?after= carries on exactly where it stopped.
func (s *server) streamRun(w http.ResponseWriter, r *http.Request) {
	eng, runID := s.in(r), r.PathValue("id")
	after, _ := strconv.Atoi(r.Header.Get("Last-Event-ID"))
	if v := r.URL.Query().Get("after"); v != "" {
		after, _ = strconv.Atoi(v)
	}
	if _, err := eng.EventsAfter(r.Context(), runID, 1<<30); err != nil {
		fail(w, err)
		return
	}
	moved, stop := eng.Subscribe()
	defer stop()
	flusher, ok := startStream(w)
	if !ok {
		return
	}
	check := time.NewTicker(streamCheck)
	defer check.Stop()
	beat := time.NewTicker(streamHeartbeat)
	defer beat.Stop()
	for {
		events, err := eng.EventsAfter(r.Context(), runID, after)
		if err != nil {
			return
		}
		for _, ev := range events {
			data, _ := json.Marshal(ev)
			fmt.Fprintf(w, "id: %d\ndata: %s\n\n", ev.Seq, data)
			after = ev.Seq
		}
		if len(events) > 0 {
			flusher.Flush()
		}
		// Wait for news of this run, the next look, or the client going away.
	wait:
		for {
			select {
			case <-r.Context().Done():
				return
			case m := <-moved:
				if m.RunID == runID {
					break wait
				}
			case <-check.C:
				break wait
			case <-beat.C:
				fmt.Fprint(w, ": still here\n\n")
				flusher.Flush()
			}
		}
	}
}

// streamWorkspace says which runs in the workspace have moved, as they move.
// It carries no detail and cannot be resumed: it is a prompt to look again,
// for a list of runs to keep itself fresh.
func (s *server) streamWorkspace(w http.ResponseWriter, r *http.Request) {
	moved, stop := s.in(r).Subscribe()
	defer stop()
	flusher, ok := startStream(w)
	if !ok {
		return
	}
	beat := time.NewTicker(streamHeartbeat)
	defer beat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case m := <-moved:
			data, _ := json.Marshal(map[string]string{"run_id": m.RunID})
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		case <-beat.C:
			fmt.Fprint(w, ": still here\n\n")
			flusher.Flush()
		}
	}
}
