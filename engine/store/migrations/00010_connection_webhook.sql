-- +goose Up
-- How a third party signs the requests it sends to Weir through this
-- connection. Like auth, it holds a reference to the shared secret, never the
-- secret. Null means the connection receives nothing.
ALTER TABLE connections ADD COLUMN webhook jsonb;

-- +goose Down
ALTER TABLE connections DROP COLUMN webhook;
