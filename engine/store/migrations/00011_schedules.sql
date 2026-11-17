-- +goose Up
-- When each scheduled trigger of a workflow's live version is next due. The
-- rows are rewritten each time the workflow is published.
CREATE TABLE schedules (
    definition_id uuid NOT NULL REFERENCES definitions (id),
    trigger_index int  NOT NULL,           -- which trigger of the live version
    cron          text NOT NULL,
    next_at       timestamptz NOT NULL,
    last_at       timestamptz,
    last_error    text,                    -- why the last firing could not start a run
    PRIMARY KEY (definition_id, trigger_index)
);
CREATE INDEX schedules_due ON schedules (next_at);

-- +goose Down
DROP TABLE schedules;
