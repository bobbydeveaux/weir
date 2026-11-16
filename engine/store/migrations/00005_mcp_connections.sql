-- +goose Up
-- A connection is either a plain HTTP address or an MCP server. An MCP
-- connection brings tools with it; they belong to its workspace and to no other.
ALTER TABLE connections
    ADD COLUMN kind      text NOT NULL DEFAULT 'http' CHECK (kind IN ('http', 'mcp')),
    ADD COLUMN read_only boolean NOT NULL DEFAULT false,
    ADD COLUMN tools     jsonb NOT NULL DEFAULT '[]'::jsonb,  -- what the server offered when last asked
    ADD COLUMN synced_at timestamptz;

-- +goose Down
ALTER TABLE connections DROP COLUMN kind, DROP COLUMN read_only, DROP COLUMN tools, DROP COLUMN synced_at;
