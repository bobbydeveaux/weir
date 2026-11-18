-- +goose Up
-- A connection can sign in with the server's own identity: a short-lived ID
-- token from the platform Weir runs on. It has no secret at all.
ALTER TABLE connections DROP CONSTRAINT connections_auth_kind_check;
ALTER TABLE connections
    ADD CONSTRAINT connections_auth_kind_check CHECK (auth_kind IN ('none', 'bearer', 'header', 'basic', 'github-app', 'id-token'));

-- +goose Down
ALTER TABLE connections DROP CONSTRAINT connections_auth_kind_check;
ALTER TABLE connections
    ADD CONSTRAINT connections_auth_kind_check CHECK (auth_kind IN ('none', 'bearer', 'header', 'basic', 'github-app'));
