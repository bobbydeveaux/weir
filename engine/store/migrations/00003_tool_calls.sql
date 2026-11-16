-- +goose Up
-- One row for every tool call an agent makes or is refused.
CREATE TABLE tool_calls (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    attempt_id  uuid NOT NULL REFERENCES attempts (id),
    tool        text NOT NULL,
    class       text CHECK (class IN ('read', 'write')),   -- null when the tool was refused before its class was known
    status      text NOT NULL CHECK (status IN ('succeeded', 'failed', 'denied')),
    args        jsonb NOT NULL DEFAULT '{}'::jsonb,
    result      jsonb,
    error       text,
    duration_ms int NOT NULL DEFAULT 0,
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX tool_calls_attempt ON tool_calls (attempt_id, created_at);

-- +goose Down
DROP TABLE tool_calls;
