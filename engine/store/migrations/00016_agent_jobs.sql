-- +goose Up
-- An agent that runs in a container of its own fetches its job from Weir and
-- sends its result back. The job is here; the working directory it starts
-- from and the result it sends are in the file store.
CREATE TABLE agent_jobs (
    attempt_id   uuid PRIMARY KEY REFERENCES attempts(id) ON DELETE CASCADE,
    workspace_id uuid NOT NULL REFERENCES workspaces(id),
    job          jsonb NOT NULL,
    has_archive  boolean NOT NULL DEFAULT false,
    created_at   timestamptz NOT NULL DEFAULT now(),
    finished_at  timestamptz            -- set when the agent has sent its result
);

-- +goose Down
DROP TABLE agent_jobs;
