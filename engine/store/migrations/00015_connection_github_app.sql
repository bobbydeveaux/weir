-- +goose Up
-- A connection can sign in as a GitHub App. auth_app names the App (its
-- client id or App id); secret_ref names its private key, as for every kind.
ALTER TABLE connections DROP CONSTRAINT connections_auth_kind_check;
ALTER TABLE connections
    ADD CONSTRAINT connections_auth_kind_check CHECK (auth_kind IN ('none', 'bearer', 'header', 'basic', 'github-app')),
    ADD COLUMN auth_app text;

-- +goose Down
ALTER TABLE connections DROP CONSTRAINT connections_auth_kind_check, DROP COLUMN auth_app;
ALTER TABLE connections ADD CONSTRAINT connections_auth_kind_check CHECK (auth_kind IN ('none', 'bearer', 'header', 'basic'));
