-- +goose Up

-- What can run ---------------------------------------------------------------

CREATE TABLE definitions (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces (id),
    namespace    text NOT NULL CHECK (namespace ~ '^[a-z][a-z0-9-]*$'),
    name         text NOT NULL CHECK (name ~ '^[a-z][a-z0-9-]*$'),
    created_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, namespace, name)
);

CREATE TABLE definition_versions (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    definition_id    uuid NOT NULL REFERENCES definitions (id),
    version          int  NOT NULL CHECK (version >= 1),
    source           text NOT NULL,   -- the YAML exactly as published
    spec             jsonb NOT NULL,  -- the parsed form the engine reads
    spec_hash        text NOT NULL,
    contract_version text NOT NULL,
    published_by     text NOT NULL,
    published_at     timestamptz NOT NULL DEFAULT now(),
    UNIQUE (definition_id, version)
);

CREATE TABLE aliases (
    definition_id uuid NOT NULL REFERENCES definitions (id),
    name          text NOT NULL CHECK (name ~ '^[a-z][a-z0-9-]*$'),
    version_id    uuid NOT NULL REFERENCES definition_versions (id),
    updated_by    text NOT NULL,
    updated_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (definition_id, name)
);

CREATE TABLE templates (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces (id),
    key          text NOT NULL CHECK (key ~ '^[a-z][a-z0-9-]*$'),
    UNIQUE (workspace_id, key)
);

CREATE TABLE template_versions (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    template_id  uuid NOT NULL REFERENCES templates (id),
    version      int  NOT NULL CHECK (version >= 1),
    body         jsonb NOT NULL,
    body_hash    text NOT NULL,
    published_by text NOT NULL,
    published_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (template_id, version)
);

CREATE TABLE connections (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces (id),
    name         text NOT NULL CHECK (name ~ '^[a-z][a-z0-9_-]*$'),
    base_url     text NOT NULL,
    secret_ref   text,                -- where the secret lives; never the secret itself
    created_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, name)
);

-- What is running --------------------------------------------------------------

CREATE TABLE runs (
    id                    uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id          uuid NOT NULL REFERENCES workspaces (id),
    definition_id         uuid NOT NULL REFERENCES definitions (id),
    definition_version_id uuid NOT NULL REFERENCES definition_versions (id),
    status                text NOT NULL CHECK (status IN
                              ('running', 'waiting', 'blocked', 'compensating', 'succeeded', 'failed', 'cancelled')),
    input                 jsonb NOT NULL DEFAULT '{}'::jsonb,
    output                jsonb,
    requester             text NOT NULL,
    idempotency_key       text,
    budget_usd            numeric(14, 6),
    spent_usd             numeric(14, 6) NOT NULL DEFAULT 0,
    event_seq             int NOT NULL DEFAULT 0,
    error                 jsonb,
    created_at            timestamptz NOT NULL DEFAULT now(),
    finished_at           timestamptz
);
CREATE UNIQUE INDEX runs_idempotency ON runs (definition_id, idempotency_key) WHERE idempotency_key IS NOT NULL;

CREATE TABLE steps (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id        uuid NOT NULL REFERENCES runs (id),
    step_key      text NOT NULL,
    visit         int  NOT NULL DEFAULT 1,
    kind          text NOT NULL CHECK (kind IN ('decide', 'ai', 'deterministic', 'human', 'external')),
    status        text NOT NULL CHECK (status IN
                      ('runnable', 'running', 'waiting', 'succeeded', 'failed', 'skipped', 'cancelled', 'compensated')),
    compensates   uuid REFERENCES steps (id),  -- set on a step that undoes another
    input         jsonb NOT NULL DEFAULT '{}'::jsonb,
    output        jsonb,
    label         text,
    outcome       text,
    attempts_made int NOT NULL DEFAULT 0,
    max_attempts  int NOT NULL DEFAULT 1 CHECK (max_attempts >= 1),
    not_before    timestamptz NOT NULL DEFAULT now(),
    spent_usd     numeric(14, 6) NOT NULL DEFAULT 0,
    error         jsonb,
    created_at    timestamptz NOT NULL DEFAULT now(),
    finished_at   timestamptz,
    UNIQUE (run_id, step_key, visit)
);
-- Keeps the claim query fast as finished steps pile up.
CREATE INDEX steps_runnable ON steps (not_before) WHERE status = 'runnable';
CREATE INDEX steps_run ON steps (run_id);

CREATE TABLE attempts (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    step_id     uuid NOT NULL REFERENCES steps (id),
    number      int  NOT NULL CHECK (number >= 1),
    status      text NOT NULL CHECK (status IN
                    ('running', 'waiting', 'succeeded', 'failed', 'timed_out', 'abandoned', 'deferred')),
    token_hash  bytea NOT NULL UNIQUE,  -- SHA-256 of the token; the token itself is never stored
    worker_id   text,
    lease_until timestamptz,
    actor       text,
    error       jsonb,
    cost_usd    numeric(14, 6) NOT NULL DEFAULT 0,
    started_at  timestamptz NOT NULL DEFAULT now(),
    finished_at timestamptz,
    UNIQUE (step_id, number)
);
-- A step has at most one live attempt.
CREATE UNIQUE INDEX attempts_one_live ON attempts (step_id) WHERE status IN ('running', 'waiting');
CREATE INDEX attempts_lease ON attempts (lease_until) WHERE status = 'running';

CREATE TABLE human_tasks (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    attempt_id       uuid NOT NULL UNIQUE REFERENCES attempts (id),
    run_id           uuid NOT NULL REFERENCES runs (id),
    step_id          uuid NOT NULL REFERENCES steps (id),
    assignee_role    text,
    assignee_user    text,
    excluded_actor   text,              -- who may not decide this task
    outcomes         text[] NOT NULL,
    status           text NOT NULL CHECK (status IN ('pending', 'done', 'expired')),
    outcome          text,
    decided_by       text,
    decided_at       timestamptz,
    note             text,
    escalation_level int NOT NULL DEFAULT 0,
    created_at       timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX human_tasks_pending ON human_tasks (assignee_role) WHERE status = 'pending';

CREATE TABLE timers (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id     uuid NOT NULL REFERENCES runs (id),
    step_id    uuid NOT NULL REFERENCES steps (id),
    attempt_id uuid NOT NULL REFERENCES attempts (id),
    kind       text NOT NULL CHECK (kind IN ('timeout', 'escalate')),
    fire_at    timestamptz NOT NULL,
    fired_at   timestamptz,
    data       jsonb NOT NULL DEFAULT '{}'::jsonb
);
CREATE INDEX timers_due ON timers (fire_at) WHERE fired_at IS NULL;

-- What happened ----------------------------------------------------------------

CREATE TABLE events (
    id           bigserial PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES workspaces (id),
    run_id       uuid NOT NULL REFERENCES runs (id),
    seq          int  NOT NULL,          -- orders events within a run
    step_id      uuid,
    attempt_id   uuid,
    type         text NOT NULL,
    actor        text NOT NULL,
    at           timestamptz NOT NULL DEFAULT now(),
    data         jsonb NOT NULL DEFAULT '{}'::jsonb,
    UNIQUE (run_id, seq)
);

CREATE TABLE ai_calls (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    attempt_id       uuid NOT NULL REFERENCES attempts (id),
    runner           text NOT NULL,
    provider         text NOT NULL,
    model            text NOT NULL,
    template_key     text,
    template_version int,
    prompt_hash      text,
    tokens_in        bigint NOT NULL DEFAULT 0,
    tokens_out       bigint NOT NULL DEFAULT 0,
    cost_usd         numeric(14, 6) NOT NULL DEFAULT 0,
    cost_estimated   boolean NOT NULL DEFAULT false,
    duration_ms      int NOT NULL DEFAULT 0,
    error            text,
    created_at       timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX ai_calls_attempt ON ai_calls (attempt_id);

-- Getting things out -----------------------------------------------------------

CREATE TABLE outbox (
    id           bigserial PRIMARY KEY,
    kind         text NOT NULL CHECK (kind IN ('wake', 'hook')),
    run_id       uuid REFERENCES runs (id),
    payload      jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at   timestamptz NOT NULL DEFAULT now(),
    delivered_at timestamptz
);
CREATE INDEX outbox_pending ON outbox (id) WHERE delivered_at IS NULL;

-- Rules the database enforces ---------------------------------------------------

-- +goose StatementBegin
CREATE FUNCTION weir_reject_change() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'rows in % cannot be changed or deleted', TG_TABLE_NAME
        USING ERRCODE = 'integrity_constraint_violation';
END
$$;
-- +goose StatementEnd

CREATE TRIGGER definition_versions_immutable BEFORE UPDATE OR DELETE ON definition_versions
    FOR EACH ROW EXECUTE FUNCTION weir_reject_change();
CREATE TRIGGER template_versions_immutable BEFORE UPDATE OR DELETE ON template_versions
    FOR EACH ROW EXECUTE FUNCTION weir_reject_change();
CREATE TRIGGER events_append_only BEFORE UPDATE OR DELETE ON events
    FOR EACH ROW EXECUTE FUNCTION weir_reject_change();

-- +goose Down
DROP TABLE outbox, ai_calls, events, timers, human_tasks, attempts, steps, runs,
    connections, template_versions, templates, aliases, definition_versions, definitions;
DROP FUNCTION weir_reject_change();
