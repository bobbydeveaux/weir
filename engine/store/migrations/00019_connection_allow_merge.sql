-- +goose Up
-- Whether Weir may merge a pull request through this connection. Whoever
-- manages the connection decides, not the author of a workflow. It is false
-- unless someone sets it.
ALTER TABLE connections ADD COLUMN allow_merge boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE connections DROP COLUMN allow_merge;
