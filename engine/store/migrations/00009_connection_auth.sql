-- +goose Up
-- How Weir proves who it is to a connection. The secret itself is never here:
-- secret_ref (already on the table) names it, and the platform supplies it.
ALTER TABLE connections
    ADD COLUMN auth_kind     text NOT NULL DEFAULT 'none' CHECK (auth_kind IN ('none', 'bearer', 'header', 'basic')),
    ADD COLUMN auth_header   text,   -- the header to send, for kind header
    ADD COLUMN auth_username text;   -- the user name, for kind basic

-- +goose Down
ALTER TABLE connections DROP COLUMN auth_kind, DROP COLUMN auth_header, DROP COLUMN auth_username;
