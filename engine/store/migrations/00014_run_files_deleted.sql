-- +goose Up
-- A step that works in a prepared directory can delete a file as well as
-- change one. A deleted file has a row and no bytes.
ALTER TABLE run_files ADD COLUMN deleted boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE run_files DROP COLUMN deleted;
