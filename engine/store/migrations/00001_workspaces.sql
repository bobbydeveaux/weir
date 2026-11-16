-- +goose Up
-- Every other table carries a workspace_id. The first version has one workspace.
CREATE TABLE workspaces (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    slug       text NOT NULL UNIQUE CHECK (slug ~ '^[a-z][a-z0-9-]*$'),
    name       text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO workspaces (slug, name) VALUES ('default', 'Default');

-- +goose Down
DROP TABLE workspaces;
