-- +goose Up
-- A step can wait for an event. Each attempt that starts waiting is recorded
-- here, so that an arriving event finds the steps that want its type without
-- reading every waiting run. The row is kept once the wait is over: it says
-- which event the step took, and stops the same run taking that event twice.
CREATE TABLE event_waits (
    attempt_id   uuid PRIMARY KEY REFERENCES attempts(id) ON DELETE CASCADE,
    workspace_id uuid NOT NULL REFERENCES workspaces(id),
    run_id       uuid NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    event_type   text NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    event_id     uuid REFERENCES inbound_events(id)   -- the event the step took, once it has
);
CREATE INDEX event_waits_open ON event_waits (workspace_id, event_type) WHERE event_id IS NULL;
CREATE INDEX event_waits_run ON event_waits (run_id) WHERE event_id IS NOT NULL;

-- What an event moved on, beside what it started.
ALTER TABLE inbound_events ADD COLUMN resumed jsonb NOT NULL DEFAULT '[]'::jsonb;

-- A step that starts waiting looks back over the events of its type.
CREATE INDEX inbound_events_type ON inbound_events (workspace_id, type, received_at);

-- +goose Down
DROP INDEX inbound_events_type;
ALTER TABLE inbound_events DROP COLUMN resumed;
DROP TABLE event_waits;
