-- +goose Up
-- Files kept by Weir when no bucket is configured. With WEIR_BLOB_URL set,
-- files go to that bucket and this table stays empty.
CREATE TABLE blobs (
    key          text PRIMARY KEY,
    content_type text NOT NULL DEFAULT 'application/octet-stream',
    size         bigint NOT NULL,
    sha256       text NOT NULL,
    data         bytea NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE blobs;
