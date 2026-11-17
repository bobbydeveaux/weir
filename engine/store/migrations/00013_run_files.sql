-- +goose Up
-- Files a step produced. The bytes are in the file store, under blob_key;
-- this table says which run and step they belong to.
CREATE TABLE run_files (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id),
    run_id       uuid NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    step_id      uuid NOT NULL REFERENCES steps(id) ON DELETE CASCADE,
    path         text NOT NULL,
    size         bigint NOT NULL,
    sha256       text NOT NULL,
    content_type text NOT NULL DEFAULT 'application/octet-stream',
    blob_key     text NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (step_id, path)
);
CREATE INDEX run_files_run ON run_files (run_id);

-- +goose Down
DROP TABLE run_files;
