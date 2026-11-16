-- +goose Up
-- An event is something an application says has happened. Weir finds the
-- workflows whose triggers match it and starts a run of each.
CREATE TABLE inbound_events (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces (id),
    type         text NOT NULL,
    source       text NOT NULL DEFAULT '',
    key          text,                               -- the sender's own id for the event, so a repeat is recognised
    data         jsonb NOT NULL DEFAULT '{}'::jsonb,
    sent_by      text NOT NULL,
    received_at  timestamptz NOT NULL DEFAULT now(),
    started      jsonb NOT NULL DEFAULT '[]'::jsonb  -- the runs it started, or why one could not start
);
CREATE UNIQUE INDEX inbound_events_key ON inbound_events (workspace_id, type, key) WHERE key IS NOT NULL;
CREATE INDEX inbound_events_recent ON inbound_events (workspace_id, received_at DESC);

-- +goose Down
DROP TABLE inbound_events;
