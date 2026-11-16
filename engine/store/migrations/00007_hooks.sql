-- +goose Up
-- A hook waits in the outbox until it is delivered. A delivery that fails is
-- tried again later, a limited number of times.
ALTER TABLE outbox
    ADD COLUMN attempts   int NOT NULL DEFAULT 0,
    ADD COLUMN next_at    timestamptz NOT NULL DEFAULT now(),
    ADD COLUMN last_error text,
    ADD COLUMN failed_at  timestamptz;       -- set when Weir has given up
CREATE INDEX outbox_hooks_due ON outbox (next_at) WHERE kind = 'hook' AND delivered_at IS NULL AND failed_at IS NULL;

-- +goose Down
DROP INDEX outbox_hooks_due;
ALTER TABLE outbox DROP COLUMN attempts, DROP COLUMN next_at, DROP COLUMN last_error, DROP COLUMN failed_at;
