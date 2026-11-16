package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// This file holds the read side of the engine: what a console or an
// application needs to show workflows, runs and usage. Each method returns
// JSON built by Postgres, so the shapes are defined by the queries below.

// list runs a query and returns its rows as a JSON array.
func (e *Engine) list(ctx context.Context, query string, args ...any) (json.RawMessage, error) {
	var out string
	err := e.pool.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(q)), '[]'::jsonb)::text FROM (`+query+`) q`, args...).Scan(&out)
	return json.RawMessage(out), err
}

// one runs a query that returns a single row and returns it as a JSON object.
func (e *Engine) one(ctx context.Context, what, query string, args ...any) (json.RawMessage, error) {
	var out string
	err := e.pool.QueryRow(ctx, `SELECT to_jsonb(q)::text FROM (`+query+`) q`, args...).Scan(&out)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("%s: %w", what, ErrNotFound)
	}
	return json.RawMessage(out), err
}

// Definitions lists every workflow with its live version and how it has been used.
func (e *Engine) Definitions(ctx context.Context) (json.RawMessage, error) {
	return e.list(ctx, `
		SELECT d.namespace, d.name, d.created_at,
			(SELECT max(v.version) FROM definition_versions v WHERE v.definition_id = d.id) AS latest_version,
			live.version AS live_version, live.published_at AS live_published_at, live.published_by AS live_published_by,
			(SELECT COALESCE(jsonb_agg(jsonb_build_object('id', s->>'id', 'kind', s->>'kind')), '[]'::jsonb)
				FROM jsonb_array_elements(live.spec->'steps') s) AS steps,
			COALESCE(u.runs, 0) AS runs, COALESCE(u.active, 0) AS active, COALESCE(u.blocked, 0) AS blocked,
			COALESCE(u.succeeded, 0) AS succeeded,
			COALESCE(u.failed, 0) AS failed, COALESCE(u.spent_usd, 0)::float8 AS spent_usd, u.last_run_at
		FROM definitions d
		LEFT JOIN LATERAL (
			SELECT v.version, v.spec, v.published_at, v.published_by FROM aliases a
			JOIN definition_versions v ON v.id = a.version_id WHERE a.definition_id = d.id AND a.name = 'live') live ON true
		LEFT JOIN LATERAL (
			SELECT count(*) AS runs, count(*) FILTER (WHERE r.status IN ('running', 'waiting', 'compensating')) AS active,
				count(*) FILTER (WHERE r.status = 'blocked') AS blocked,
				count(*) FILTER (WHERE r.status = 'succeeded') AS succeeded,
				count(*) FILTER (WHERE r.status IN ('failed', 'cancelled')) AS failed,
				sum(r.spent_usd) AS spent_usd, max(r.created_at) AS last_run_at
			FROM runs r WHERE r.definition_id = d.id) u ON true
		WHERE d.workspace_id = $1::uuid ORDER BY d.namespace, d.name`, e.workspace)
}

// Definition returns one workflow: its versions, its aliases, and the source
// and parsed form of one version. Version 0 means the live one.
func (e *Engine) Definition(ctx context.Context, namespace, name string, version int) (json.RawMessage, error) {
	return e.one(ctx, "workflow "+namespace+"/"+name, `
		SELECT d.namespace, d.name, d.created_at, shown.version, shown.source, shown.spec, shown.spec_hash,
			shown.contract_version, shown.published_by, shown.published_at,
			(SELECT jsonb_agg(jsonb_build_object('version', v.version, 'published_by', v.published_by, 'published_at', v.published_at,
					'spec_hash', v.spec_hash, 'runs', (SELECT count(*) FROM runs r WHERE r.definition_version_id = v.id))
					ORDER BY v.version DESC)
				FROM definition_versions v WHERE v.definition_id = d.id) AS versions,
			(SELECT COALESCE(jsonb_agg(jsonb_build_object('name', a.name, 'version', v.version, 'updated_by', a.updated_by,
					'updated_at', a.updated_at) ORDER BY a.name), '[]'::jsonb)
				FROM aliases a JOIN definition_versions v ON v.id = a.version_id WHERE a.definition_id = d.id) AS aliases
		FROM definitions d
		JOIN LATERAL (
			SELECT v.* FROM definition_versions v WHERE v.definition_id = d.id AND (
				($4::int > 0 AND v.version = $4::int) OR
				($4::int = 0 AND v.id = (SELECT a.version_id FROM aliases a WHERE a.definition_id = d.id AND a.name = 'live')))
		) shown ON true
		WHERE d.workspace_id = $1::uuid AND d.namespace = $2 AND d.name = $3`, e.workspace, namespace, name, version)
}

// RunFilter narrows a list of runs. Empty fields match everything.
type RunFilter struct {
	Namespace, Name, Status, Requester string
	// Limit is the most runs to return. Zero means 50; the most is 200.
	Limit int
}

// Runs lists runs, newest first.
func (e *Engine) Runs(ctx context.Context, f RunFilter) (json.RawMessage, error) {
	if f.Limit <= 0 {
		f.Limit = 50
	}
	if f.Limit > 200 {
		f.Limit = 200
	}
	return e.list(ctx, `
		SELECT r.id, d.namespace, d.name, v.version, r.status, r.requester, r.spent_usd::float8 AS spent_usd,
			r.budget_usd::float8 AS budget_usd, r.created_at, r.finished_at,
			(SELECT s.step_key FROM steps s WHERE s.run_id = r.id ORDER BY s.created_at DESC, s.visit DESC LIMIT 1) AS current_step,
			(SELECT count(DISTINCT s.step_key) FROM steps s WHERE s.run_id = r.id AND s.status = 'succeeded') AS steps_done,
			-- A finished run is measured against the steps it visited: a branch it
			-- never took is not unfinished work. A run still going is measured
			-- against every step, since its path is not yet known.
			CASE WHEN r.finished_at IS NOT NULL
				THEN (SELECT count(DISTINCT s.step_key) FROM steps s WHERE s.run_id = r.id)
				ELSE jsonb_array_length(v.spec->'steps') END AS steps_total
		FROM runs r JOIN definitions d ON d.id = r.definition_id JOIN definition_versions v ON v.id = r.definition_version_id
		WHERE r.workspace_id = $1::uuid AND ($2 = '' OR d.namespace = $2) AND ($3 = '' OR d.name = $3)
			AND ($4 = '' OR r.status = $4) AND ($5 = '' OR r.requester = $5)
		ORDER BY r.created_at DESC LIMIT $6`, e.workspace, f.Namespace, f.Name, f.Status, f.Requester, f.Limit)
}

// RunRecord returns what a run's steps did beyond their outputs: every model
// call and every tool call, each tied to its step.
func (e *Engine) RunRecord(ctx context.Context, runID string) (json.RawMessage, error) {
	return e.one(ctx, "run "+runID, `
		SELECT d.namespace, d.name, v.version, r.requester, r.created_at, r.finished_at, r.budget_usd::float8 AS budget_usd,
			(SELECT COALESCE(jsonb_agg(jsonb_build_object('step', s.step_key, 'visit', s.visit, 'attempt', a.number,
					'runner', c.runner, 'provider', c.provider, 'model', c.model, 'template', c.template_key,
					'template_version', c.template_version, 'prompt_hash', c.prompt_hash, 'tokens_in', c.tokens_in,
					'tokens_out', c.tokens_out, 'cost_usd', c.cost_usd::float8, 'cost_estimated', c.cost_estimated,
					'duration_ms', c.duration_ms, 'error', c.error, 'prompt', c.prompt, 'response', c.response, 'at', c.created_at)
					ORDER BY c.created_at), '[]'::jsonb)
				FROM ai_calls c JOIN attempts a ON a.id = c.attempt_id JOIN steps s ON s.id = a.step_id WHERE s.run_id = r.id) AS ai_calls,
			(SELECT COALESCE(jsonb_agg(jsonb_build_object('step', s.step_key, 'visit', s.visit, 'tool', c.tool, 'class', c.class,
					'status', c.status, 'args', c.args, 'result', c.result, 'error', c.error, 'duration_ms', c.duration_ms,
					'at', c.created_at) ORDER BY c.created_at), '[]'::jsonb)
				FROM tool_calls c JOIN attempts a ON a.id = c.attempt_id JOIN steps s ON s.id = a.step_id WHERE s.run_id = r.id) AS tool_calls,
			(SELECT COALESCE(jsonb_agg(jsonb_build_object('step', s.step_key, 'visit', s.visit, 'input', s.input,
					'started_at', s.created_at, 'finished_at', s.finished_at, 'spent_usd', s.spent_usd::float8)
					ORDER BY s.created_at, s.visit), '[]'::jsonb)
				FROM steps s WHERE s.run_id = r.id) AS step_times,
			(SELECT COALESCE(jsonb_agg(jsonb_build_object('seq', ev.seq, 'at', ev.at) ORDER BY ev.seq), '[]'::jsonb)
				FROM events ev WHERE ev.run_id = r.id) AS event_times,
			(SELECT COALESCE(jsonb_agg(jsonb_build_object('type', o.payload->'body'->>'type', 'step', o.payload->'body'->>'step',
					'connection', o.payload->>'connection', 'path', o.payload->>'path', 'attempts', o.attempts,
					'queued_at', o.created_at, 'delivered_at', o.delivered_at, 'failed_at', o.failed_at, 'error', o.last_error)
					ORDER BY o.id), '[]'::jsonb)
				FROM outbox o WHERE o.run_id = r.id AND o.kind = 'hook') AS hooks
		FROM runs r JOIN definitions d ON d.id = r.definition_id JOIN definition_versions v ON v.id = r.definition_version_id
		WHERE r.id = $1::uuid AND r.workspace_id = $2::uuid`, runID, e.workspace)
}

// Templates lists every template with its latest version.
func (e *Engine) Templates(ctx context.Context) (json.RawMessage, error) {
	return e.list(ctx, `
		SELECT t.key, v.version, v.body, v.body_hash, v.published_by, v.published_at,
			(SELECT count(*) FROM template_versions x WHERE x.template_id = t.id) AS versions
		FROM templates t JOIN LATERAL (
			SELECT * FROM template_versions v WHERE v.template_id = t.id ORDER BY v.version DESC LIMIT 1) v ON true
		WHERE t.workspace_id = $1::uuid ORDER BY t.key`, e.workspace)
}

// TemplateVersions returns every version of one template, newest first.
func (e *Engine) TemplateVersions(ctx context.Context, key string) (json.RawMessage, error) {
	return e.one(ctx, "template "+key, `
		SELECT t.key, (SELECT jsonb_agg(jsonb_build_object('version', v.version, 'body', v.body, 'body_hash', v.body_hash,
				'published_by', v.published_by, 'published_at', v.published_at) ORDER BY v.version DESC)
			FROM template_versions v WHERE v.template_id = t.id) AS versions
		FROM templates t WHERE t.workspace_id = $1::uuid AND t.key = $2`, e.workspace, key)
}

// Connections lists the named third parties. Secrets are never returned.
func (e *Engine) Connections(ctx context.Context) (json.RawMessage, error) {
	return e.list(ctx, `
		SELECT name, kind, base_url, read_only, tools, synced_at, secret_ref IS NOT NULL AS has_secret, created_at
		FROM connections WHERE workspace_id = $1::uuid ORDER BY name`, e.workspace)
}

// Usage sums what has been run and spent over the last days days, for one
// namespace or, when namespace is empty, for everything.
func (e *Engine) Usage(ctx context.Context, namespace string, days int) (json.RawMessage, error) {
	if days <= 0 {
		days = 30
	}
	if days > 366 {
		days = 366
	}
	const runs = `runs r JOIN definitions d ON d.id = r.definition_id
		WHERE r.workspace_id = $1::uuid AND ($2 = '' OR d.namespace = $2) AND r.created_at >= now() - make_interval(days => $3::int)`
	const calls = `ai_calls c JOIN attempts a ON a.id = c.attempt_id JOIN steps s ON s.id = a.step_id JOIN runs r ON r.id = s.run_id
		JOIN definitions d ON d.id = r.definition_id
		WHERE r.workspace_id = $1::uuid AND ($2 = '' OR d.namespace = $2) AND r.created_at >= now() - make_interval(days => $3::int)`
	return e.one(ctx, "usage", `
		SELECT $3::int AS days, $2::text AS namespace,
			(SELECT jsonb_build_object('runs', count(*),
					'active', count(*) FILTER (WHERE r.status IN ('running', 'waiting', 'compensating')),
					'blocked', count(*) FILTER (WHERE r.status = 'blocked'),
					'succeeded', count(*) FILTER (WHERE r.status = 'succeeded'),
					'failed', count(*) FILTER (WHERE r.status IN ('failed', 'cancelled')),
					'spent_usd', COALESCE(sum(r.spent_usd), 0)::float8) FROM `+runs+`) AS totals,
			(SELECT jsonb_build_object('calls', count(*), 'tokens_in', COALESCE(sum(c.tokens_in), 0),
					'tokens_out', COALESCE(sum(c.tokens_out), 0), 'cost_usd', COALESCE(sum(c.cost_usd), 0)::float8) FROM `+calls+`) AS ai,
			(SELECT COALESCE(jsonb_agg(x ORDER BY x.spent_usd DESC, x.runs DESC), '[]'::jsonb) FROM (
				SELECT d.namespace, d.name, count(*) AS runs, count(*) FILTER (WHERE r.status = 'succeeded') AS succeeded,
					count(*) FILTER (WHERE r.status IN ('failed', 'cancelled')) AS failed,
					COALESCE(sum(r.spent_usd), 0)::float8 AS spent_usd FROM `+runs+` GROUP BY d.namespace, d.name) x) AS by_workflow,
			(SELECT COALESCE(jsonb_agg(x ORDER BY x.calls DESC), '[]'::jsonb) FROM (
				SELECT c.provider, c.model, count(*) AS calls, sum(c.tokens_in) AS tokens_in, sum(c.tokens_out) AS tokens_out,
					sum(c.cost_usd)::float8 AS cost_usd, bool_or(c.cost_estimated) AS cost_estimated,
					round(avg(c.duration_ms)) AS avg_ms, count(*) FILTER (WHERE c.error IS NOT NULL) AS errors
				FROM `+calls+` GROUP BY c.provider, c.model) x) AS by_model,
			(SELECT COALESCE(jsonb_agg(x ORDER BY x.day), '[]'::jsonb) FROM (
				SELECT to_char(date_trunc('day', r.created_at), 'YYYY-MM-DD') AS day, count(*) AS runs,
					COALESCE(sum(r.spent_usd), 0)::float8 AS spent_usd FROM `+runs+` GROUP BY 1) x) AS by_day`,
		e.workspace, namespace, days)
}
