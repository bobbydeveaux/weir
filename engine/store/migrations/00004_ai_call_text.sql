-- +goose Up
-- The prompt as sent and the reply as received, so that a run can be audited
-- without the template and the model both still being available.
ALTER TABLE ai_calls ADD COLUMN prompt text, ADD COLUMN response text;

-- +goose Down
ALTER TABLE ai_calls DROP COLUMN prompt, DROP COLUMN response;
