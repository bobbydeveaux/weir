-- +goose Up
-- A key lets an application or a person call the API as a member of one
-- workspace. Only a hash of the key is kept, so a key can be shown once and
-- never again.
CREATE TABLE api_keys (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces (id),
    name         text NOT NULL,                     -- who the key speaks as
    role         text NOT NULL CHECK (role IN ('reader', 'writer', 'admin')),
    act_as       boolean NOT NULL DEFAULT false,    -- may name the person it is acting for
    key_hash     bytea NOT NULL UNIQUE,
    created_by   text NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz,
    revoked_at   timestamptz,
    UNIQUE (workspace_id, name)
);

-- +goose Down
DROP TABLE api_keys;
