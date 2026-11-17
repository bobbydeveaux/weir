// Package engine runs workflows. Every exported method that changes state does
// so in one database transaction: the state change, the event that records it,
// and any outbox row commit together or not at all.
package engine

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/bobbydeveaux/weir/internal/secrets"
	"github.com/bobbydeveaux/weir/spec"
)

// Errors callers can test for.
var (
	// ErrStaleToken means the token does not belong to a live attempt: the
	// attempt already finished, timed out, or was retried.
	ErrStaleToken = errors.New("token does not match a live attempt")
	// ErrNotFound means the named thing does not exist.
	ErrNotFound = errors.New("not found")
	// ErrNotAllowed means the actor may not take this action.
	ErrNotAllowed = errors.New("not allowed")
	// ErrTaskClosed means a human task was already decided or has expired.
	ErrTaskClosed = errors.New("task is no longer open")
	// ErrNotBlocked means a run that is not blocked was asked to resume.
	ErrNotBlocked = errors.New("run is not blocked")
)

// Engine is the workflow engine. Create one with New.
type Engine struct {
	pool *pgxpool.Pool
	// hub passes news of run events to whoever is streaming. Engines made by
	// In share it.
	hub *hub
	// slug is the workspace's name, as people write it.
	slug string
	// workspace is the id of the workspace this engine reads and writes.
	// Everything a caller can name (workflows, templates, connections, runs,
	// tasks) is looked up inside it. Work in progress is the exception: a
	// worker claims steps from every workspace, and each step carries its own.
	workspace string

	// HandlerExists, when set, lets Publish refuse a definition that names an
	// unregistered deterministic handler.
	HandlerExists func(name string) bool
	// ToolClass, when set, reports whether a tool exists and whether it is a
	// read or a write. Publish uses it to check the tools an ai step lists.
	ToolClass func(name string) (class string, ok bool)
	// BackoffBase is the delay before a step's first retry. It doubles each time.
	BackoffBase time.Duration
	// MaxAbandoned is how many times a step is re-run after its worker
	// disappeared before the step is failed.
	MaxAbandoned int
}

// New returns an engine for the default workspace of the database behind pool.
func New(ctx context.Context, pool *pgxpool.Pool) (*Engine, error) {
	e := &Engine{pool: pool, hub: &hub{subs: map[chan Moved]struct{}{}}, BackoffBase: 2 * time.Second, MaxAbandoned: 3}
	e.slug = "default"
	err := pool.QueryRow(ctx, `SELECT id::text FROM workspaces WHERE slug = 'default'`).Scan(&e.workspace)
	if err != nil {
		return nil, fmt.Errorf("find the default workspace (has the schema been migrated?): %w", err)
	}
	return e, nil
}

// Workspace is one tenant: a team, a customer or an application.
type Workspace struct {
	Slug      string    `json:"slug"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	// Workflows and Runs count what the workspace holds, so that a person
	// looking at an empty one can see where the work is.
	Workflows int `json:"workflows"`
	Runs      int `json:"runs"`
}

// In returns an engine for the workspace with the given slug. It shares this
// engine's database and settings.
func (e *Engine) In(ctx context.Context, slug string) (*Engine, error) {
	scoped := *e
	scoped.slug = slug
	err := e.pool.QueryRow(ctx, `SELECT id::text FROM workspaces WHERE slug = $1`, slug).Scan(&scoped.workspace)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("workspace %s: %w", slug, ErrNotFound)
	}
	return &scoped, err
}

// CreateWorkspace adds a workspace. Creating one that exists changes nothing.
func (e *Engine) CreateWorkspace(ctx context.Context, slug, name string) error {
	if name == "" {
		name = slug
	}
	_, err := e.pool.Exec(ctx, `INSERT INTO workspaces (slug, name) VALUES ($1, $2) ON CONFLICT (slug) DO NOTHING`, slug, name)
	if err != nil {
		return fmt.Errorf("create workspace %s (a slug is lower-case letters, digits and hyphens): %w", slug, err)
	}
	return nil
}

// Workspaces lists every workspace.
func (e *Engine) Workspaces(ctx context.Context) ([]Workspace, error) {
	rows, err := e.pool.Query(ctx, `
		SELECT w.slug, w.name, w.created_at,
			(SELECT count(*) FROM definitions d WHERE d.workspace_id = w.id),
			(SELECT count(*) FROM runs r WHERE r.workspace_id = w.id)
		FROM workspaces w ORDER BY w.slug`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Workspace, error) {
		var w Workspace
		err := row.Scan(&w.Slug, &w.Name, &w.CreatedAt, &w.Workflows, &w.Runs)
		return w, err
	})
}

// inTx runs fn in a transaction, committing only if it returns nil.
func (e *Engine) inTx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	tx, err := e.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // a rollback after commit is a no-op
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// --- templates and connections -------------------------------------------------

// Template is the body of a prompt or decide template version.
type Template struct {
	System string `json:"system,omitempty"`
	User   string `json:"user"`
	Model  string `json:"model,omitempty"`
}

// PublishTemplate stores body as the next immutable version of the template
// named key and returns that version number.
func (e *Engine) PublishTemplate(ctx context.Context, key string, body Template, by string) (int, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return 0, err
	}
	var version int
	err = e.inTx(ctx, func(tx pgx.Tx) error {
		var id string
		err := tx.QueryRow(ctx, `
			INSERT INTO templates (workspace_id, key) VALUES ($1::uuid, $2)
			ON CONFLICT (workspace_id, key) DO UPDATE SET key = EXCLUDED.key
			RETURNING id::text`, e.workspace, key).Scan(&id)
		if err != nil {
			return fmt.Errorf("store template %q: %w", key, err)
		}
		return tx.QueryRow(ctx, `
			INSERT INTO template_versions (template_id, version, body, body_hash, published_by)
			SELECT $1::uuid, COALESCE(MAX(version), 0) + 1, $2::jsonb, $3, $4
			FROM template_versions WHERE template_id = $1::uuid
			RETURNING version`, id, string(raw), hashHex(raw), by).Scan(&version)
	})
	return version, err
}

func (e *Engine) loadTemplate(ctx context.Context, q querier, workspace, ref string) (*Template, error) {
	key, version, ok := spec.TemplateRef(ref)
	if !ok {
		return nil, fmt.Errorf("%q is not a pinned template reference", ref)
	}
	var raw string
	err := q.QueryRow(ctx, `
		SELECT v.body::text FROM template_versions v JOIN templates t ON t.id = v.template_id
		WHERE t.workspace_id = $1::uuid AND t.key = $2 AND v.version = $3`, workspace, key, version).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("template %s: %w", ref, ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	var t Template
	return &t, json.Unmarshal([]byte(raw), &t)
}

// Auth says how Weir proves who it is to a connection. It holds a reference
// to a secret and never the secret.
type Auth = secrets.Auth

// Endpoint is where a connection is, and how to sign in to it.
type Endpoint struct {
	// Connection is the connection's name.
	Connection string
	// URL is its base address.
	URL string
	// Workspace is the name of the workspace that owns it. A secret is
	// looked up for that workspace and no other.
	Workspace string
	Auth      Auth
}

// Workspace returns the name of the workspace this engine reads and writes.
func (e *Engine) Workspace() string { return e.slug }

// PutConnection creates or updates a named third party: its address, and how
// Weir signs in to it.
func (e *Engine) PutConnection(ctx context.Context, name, baseURL string, auth Auth) error {
	if err := auth.Check(); err != nil {
		return fmt.Errorf("connection %s: %w", name, err)
	}
	kind := auth.Kind
	if kind == "" {
		kind = secrets.None
	}
	_, err := e.pool.Exec(ctx, `
		INSERT INTO connections (workspace_id, name, base_url, auth_kind, secret_ref, auth_header, auth_username)
		VALUES ($1::uuid, $2, $3, $4, NULLIF($5, ''), NULLIF($6, ''), NULLIF($7, ''))
		ON CONFLICT (workspace_id, name) DO UPDATE
		SET base_url = EXCLUDED.base_url, kind = 'http', tools = '[]'::jsonb, auth_kind = EXCLUDED.auth_kind,
			secret_ref = EXCLUDED.secret_ref, auth_header = EXCLUDED.auth_header, auth_username = EXCLUDED.auth_username`,
		e.workspace, name, baseURL, kind, auth.Secret, auth.Header, auth.Username)
	return err
}

// endpoint finds a connection in a workspace, by the workspace's id.
func (e *Engine) endpoint(ctx context.Context, q querier, workspace, name string) (Endpoint, error) {
	ep := Endpoint{Connection: name}
	err := q.QueryRow(ctx, `
		SELECT c.base_url, w.slug, c.auth_kind, COALESCE(c.secret_ref, ''), COALESCE(c.auth_header, ''), COALESCE(c.auth_username, '')
		FROM connections c JOIN workspaces w ON w.id = c.workspace_id
		WHERE c.workspace_id = $1::uuid AND c.name = $2`, workspace, name).
		Scan(&ep.URL, &ep.Workspace, &ep.Auth.Kind, &ep.Auth.Secret, &ep.Auth.Header, &ep.Auth.Username)
	if errors.Is(err, pgx.ErrNoRows) {
		return ep, fmt.Errorf("connection %q: %w", name, ErrNotFound)
	}
	return ep, err
}

// --- publishing ------------------------------------------------------------------

// Published describes a newly published version.
type Published struct {
	Namespace string
	Name      string
	Version   int
	// Unsupported lists parts of the definition that parsed but are not acted on yet.
	Unsupported []string
}

// Publish validates source and stores it as the next immutable version of its
// definition. The alias "live" is moved to the new version.
func (e *Engine) Publish(ctx context.Context, source []byte, by string) (*Published, error) {
	def, err := spec.Parse(source)
	if err != nil {
		return nil, err
	}
	if err := def.Validate(spec.ValidateOptions{HandlerExists: e.HandlerExists}); err != nil {
		return nil, err
	}
	// Every reference must resolve now, so that a run cannot fail later for a missing one.
	for _, s := range def.Steps {
		switch s.Kind {
		case spec.KindDecide:
			if _, err := e.loadTemplate(ctx, e.pool, e.workspace, s.Template); err != nil {
				return nil, fmt.Errorf("step %s: %w", s.ID, err)
			}
		case spec.KindAI:
			if _, err := e.loadTemplate(ctx, e.pool, e.workspace, s.Prompt); err != nil {
				return nil, fmt.Errorf("step %s: %w", s.ID, err)
			}
		case spec.KindExternal:
			if _, err := e.endpoint(ctx, e.pool, e.workspace, s.Connection); err != nil {
				return nil, fmt.Errorf("step %s: %w", s.ID, err)
			}
		}
	}
	for i, h := range def.Hooks {
		if _, err := e.endpoint(ctx, e.pool, e.workspace, h.Connection); err != nil {
			return nil, fmt.Errorf("hooks[%d]: %w", i, err)
		}
	}
	for _, s := range def.Steps {
		for _, f := range s.Files {
			if _, err := e.endpoint(ctx, e.pool, e.workspace, f.Connection); err != nil {
				return nil, fmt.Errorf("step %s: files: %w", s.ID, err)
			}
		}
		if s.Output != nil {
			if _, err := compileSchema(s.Output.Schema); err != nil {
				return nil, fmt.Errorf("step %s: output schema: %w", s.ID, err)
			}
		}
	}
	if err := e.checkTools(ctx, def); err != nil {
		return nil, err
	}
	specJSON, err := json.Marshal(def)
	if err != nil {
		return nil, err
	}

	out := &Published{Namespace: def.Namespace, Name: def.Name, Unsupported: def.Unsupported()}
	err = e.inTx(ctx, func(tx pgx.Tx) error {
		var defID, versionID string
		err := tx.QueryRow(ctx, `
			INSERT INTO definitions (workspace_id, namespace, name) VALUES ($1::uuid, $2, $3)
			ON CONFLICT (workspace_id, namespace, name) DO UPDATE SET name = EXCLUDED.name
			RETURNING id::text`, e.workspace, def.Namespace, def.Name).Scan(&defID)
		if err != nil {
			return err
		}
		err = tx.QueryRow(ctx, `
			INSERT INTO definition_versions (definition_id, version, source, spec, spec_hash, contract_version, published_by)
			SELECT $1::uuid, COALESCE(MAX(version), 0) + 1, $2, $3::jsonb, $4, $5, $6
			FROM definition_versions WHERE definition_id = $1::uuid
			RETURNING id::text, version`,
			defID, string(source), string(specJSON), hashHex(specJSON), def.Weir, by).Scan(&versionID, &out.Version)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO aliases (definition_id, name, version_id, updated_by) VALUES ($1::uuid, 'live', $2::uuid, $3)
			ON CONFLICT (definition_id, name)
			DO UPDATE SET version_id = EXCLUDED.version_id, updated_by = EXCLUDED.updated_by, updated_at = now()`,
			defID, versionID, by)
		if err != nil {
			return err
		}
		// The live version's schedules replace whatever was there before.
		return setSchedules(ctx, tx, defID, def)
	})
	if err != nil {
		return nil, fmt.Errorf("publish %s/%s: %w", def.Namespace, def.Name, err)
	}
	return out, nil
}

// --- starting a run ----------------------------------------------------------------

// StartRequest describes a run to start.
type StartRequest struct {
	Namespace string
	Name      string
	// Alias selects the version. Empty means "live".
	Alias string
	// Input is the run's input as JSON. Empty means {}.
	Input json.RawMessage
	// Requester is who or what started the run.
	Requester string
	// IdempotencyKey, when set, makes starting twice return the same run.
	IdempotencyKey string
}

// StartRun starts a run and returns its id. created is false when an existing
// run with the same idempotency key was returned instead.
func (e *Engine) StartRun(ctx context.Context, req StartRequest) (runID string, created bool, err error) {
	if req.Requester == "" {
		return "", false, errors.New("a run needs a requester")
	}
	if req.Alias == "" {
		req.Alias = "live"
	}
	if len(req.Input) == 0 {
		req.Input = json.RawMessage(`{}`)
	}
	err = e.inTx(ctx, func(tx pgx.Tx) error {
		var defID, versionID, specJSON string
		var version int
		err := tx.QueryRow(ctx, `
			SELECT d.id::text, v.id::text, v.spec::text, v.version
			FROM definitions d
			JOIN aliases a ON a.definition_id = d.id AND a.name = $4
			JOIN definition_versions v ON v.id = a.version_id
			WHERE d.workspace_id = $1::uuid AND d.namespace = $2 AND d.name = $3`,
			e.workspace, req.Namespace, req.Name, req.Alias).Scan(&defID, &versionID, &specJSON, &version)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("definition %s/%s at alias %q: %w", req.Namespace, req.Name, req.Alias, ErrNotFound)
		}
		if err != nil {
			return err
		}
		if req.IdempotencyKey != "" {
			err := tx.QueryRow(ctx, `SELECT id::text FROM runs WHERE definition_id = $1::uuid AND idempotency_key = $2`,
				defID, req.IdempotencyKey).Scan(&runID)
			if err == nil {
				return nil // already started
			}
			if !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
		}
		var def spec.Definition
		if err := json.Unmarshal([]byte(specJSON), &def); err != nil {
			return fmt.Errorf("read stored definition: %w", err)
		}
		def.Version = version
		if def.Input != nil {
			if err := validateJSON(def.Input.Schema, req.Input); err != nil {
				return fmt.Errorf("run input: %w", err)
			}
		}
		var budget *float64
		if def.Budget != nil && def.Budget.USD > 0 {
			budget = &def.Budget.USD
		}
		err = tx.QueryRow(ctx, `
			INSERT INTO runs (workspace_id, definition_id, definition_version_id, status, input, requester, idempotency_key, budget_usd)
			VALUES ($1::uuid, $2::uuid, $3::uuid, 'running', $4::jsonb, $5, NULLIF($6, ''), $7::numeric)
			RETURNING id::text`,
			e.workspace, defID, versionID, string(req.Input), req.Requester, req.IdempotencyKey, budget).Scan(&runID)
		if err != nil {
			return err
		}
		created = true
		run := &runRow{id: runID, workspace: e.workspace, status: "running", input: string(req.Input), requester: req.Requester, budget: budget, def: &def}
		if err := e.event(ctx, tx, run, nil, nil, "run.started", req.Requester, map[string]any{"version": def.Version}); err != nil {
			return err
		}
		scope, err := e.scope(ctx, tx, run)
		if err != nil {
			return err
		}
		return e.createStep(ctx, tx, run, &def.Steps[0], scope)
	})
	return runID, created, err
}

// --- reading a run -------------------------------------------------------------------

// RunView is a run with its steps and events, for display.
type RunView struct {
	ID       string          `json:"id"`
	Status   string          `json:"status"`
	Input    json.RawMessage `json:"input"`
	Output   json.RawMessage `json:"output,omitempty"`
	Error    json.RawMessage `json:"error,omitempty"`
	SpentUSD float64         `json:"spent_usd"`
	Steps    []StepView      `json:"steps"`
	Events   []EventView     `json:"events"`
}

// StepView is one visit to a step.
type StepView struct {
	Key      string          `json:"key"`
	Visit    int             `json:"visit"`
	Kind     string          `json:"kind"`
	Status   string          `json:"status"`
	Attempts int             `json:"attempts"`
	Label    string          `json:"label,omitempty"`
	Outcome  string          `json:"outcome,omitempty"`
	Output   json.RawMessage `json:"output,omitempty"`
	Error    json.RawMessage `json:"error,omitempty"`
}

// EventView is one entry in a run's event log.
type EventView struct {
	Seq   int             `json:"seq"`
	Type  string          `json:"type"`
	Actor string          `json:"actor"`
	Step  string          `json:"step,omitempty"`
	Data  json.RawMessage `json:"data,omitempty"`
}

// Run returns a run with its steps and its event log.
func (e *Engine) Run(ctx context.Context, runID string) (*RunView, error) {
	v := &RunView{ID: runID}
	var input string
	var output, runErr *string
	err := e.pool.QueryRow(ctx, `
		SELECT status, input::text, output::text, error::text, spent_usd::float8
		FROM runs WHERE id = $1::uuid AND workspace_id = $2::uuid`, runID, e.workspace).
		Scan(&v.Status, &input, &output, &runErr, &v.SpentUSD)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("run %s: %w", runID, ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	v.Input, v.Output, v.Error = json.RawMessage(input), rawOrNil(output), rawOrNil(runErr)

	rows, err := e.pool.Query(ctx, `
		SELECT step_key, visit, kind, status, attempts_made, COALESCE(label, ''), COALESCE(outcome, ''), output::text, error::text
		FROM steps WHERE run_id = $1::uuid ORDER BY created_at, visit`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var s StepView
		var out, stepErr *string
		if err := rows.Scan(&s.Key, &s.Visit, &s.Kind, &s.Status, &s.Attempts, &s.Label, &s.Outcome, &out, &stepErr); err != nil {
			return nil, err
		}
		s.Output, s.Error = rawOrNil(out), rawOrNil(stepErr)
		v.Steps = append(v.Steps, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	evs, err := e.pool.Query(ctx, `
		SELECT e.seq, e.type, e.actor, COALESCE(s.step_key, ''), e.data::text
		FROM events e LEFT JOIN steps s ON s.id = e.step_id
		WHERE e.run_id = $1::uuid ORDER BY e.seq`, runID)
	if err != nil {
		return nil, err
	}
	defer evs.Close()
	for evs.Next() {
		var ev EventView
		var data string
		if err := evs.Scan(&ev.Seq, &ev.Type, &ev.Actor, &ev.Step, &data); err != nil {
			return nil, err
		}
		ev.Data = json.RawMessage(data)
		v.Events = append(v.Events, ev)
	}
	return v, evs.Err()
}

// --- shared helpers --------------------------------------------------------------------

// querier is satisfied by both a pool and a transaction.
type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// runRow is the part of a run the engine works with.
type runRow struct {
	id        string
	workspace string
	status    string
	input     string
	requester string
	budget    *float64
	spent     float64
	def       *spec.Definition
}

// loadRun reads a run and locks its row for the rest of the transaction.
func (e *Engine) loadRun(ctx context.Context, tx pgx.Tx, runID string) (*runRow, error) {
	r := &runRow{id: runID}
	var specJSON string
	var version int
	err := tx.QueryRow(ctx, `
		SELECT r.workspace_id::text, r.status, r.input::text, r.requester, r.budget_usd::float8, r.spent_usd::float8, v.spec::text, v.version
		FROM runs r JOIN definition_versions v ON v.id = r.definition_version_id
		WHERE r.id = $1::uuid FOR UPDATE OF r`, runID).
		Scan(&r.workspace, &r.status, &r.input, &r.requester, &r.budget, &r.spent, &specJSON, &version)
	if err != nil {
		return nil, fmt.Errorf("load run %s: %w", runID, err)
	}
	r.def = &spec.Definition{}
	if err := json.Unmarshal([]byte(specJSON), r.def); err != nil {
		return nil, fmt.Errorf("read stored definition: %w", err)
	}
	r.def.Version = version
	return r, nil
}

// event appends one entry to the run's event log.
func (e *Engine) event(ctx context.Context, tx pgx.Tx, run *runRow, stepID, attemptID *string, typ, actor string, data any) error {
	raw := []byte(`{}`)
	if data != nil {
		var err error
		if raw, err = json.Marshal(data); err != nil {
			return err
		}
	}
	var seq int
	if err := tx.QueryRow(ctx, `UPDATE runs SET event_seq = event_seq + 1 WHERE id = $1::uuid RETURNING event_seq`, run.id).Scan(&seq); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO events (workspace_id, run_id, seq, step_id, attempt_id, type, actor, data)
		VALUES ($1::uuid, $2::uuid, $3, $4::uuid, $5::uuid, $6, $7, $8::jsonb)`,
		run.workspace, run.id, seq, stepID, attemptID, typ, actor, string(raw))
	if err != nil {
		return err
	}
	if err := e.announce(ctx, tx, run); err != nil {
		return err
	}
	return e.enqueueHooks(ctx, tx, run, stepID, seq, typ, actor, raw)
}

// wake records that a step is ready, so a worker can be woken.
func (e *Engine) wake(ctx context.Context, tx pgx.Tx, runID string) error {
	_, err := tx.Exec(ctx, `INSERT INTO outbox (kind, run_id) VALUES ('wake', $1::uuid)`, runID)
	return err
}

// scope builds what expressions can see: the run's input and every step so far.
func (e *Engine) scope(ctx context.Context, tx pgx.Tx, run *runRow) (spec.Scope, error) {
	var input any
	if err := json.Unmarshal([]byte(run.input), &input); err != nil {
		return spec.Scope{}, fmt.Errorf("read run input: %w", err)
	}
	sc := spec.Scope{
		Input: input,
		Steps: map[string]any{},
		Run:   map[string]any{"id": run.id, "requester": run.requester},
	}
	rows, err := tx.Query(ctx, `
		SELECT DISTINCT ON (step_key) step_key, output::text, COALESCE(label, ''), COALESCE(outcome, '')
		FROM steps WHERE run_id = $1::uuid AND compensates IS NULL
		ORDER BY step_key, visit DESC`, run.id)
	if err != nil {
		return sc, err
	}
	defer rows.Close()
	for rows.Next() {
		var key, label, outcome string
		var out *string
		if err := rows.Scan(&key, &out, &label, &outcome); err != nil {
			return sc, err
		}
		var output any
		if out != nil {
			if err := json.Unmarshal([]byte(*out), &output); err != nil {
				return sc, err
			}
		}
		sc.Steps[key] = map[string]any{"output": output, "label": label, "outcome": outcome}
	}
	return sc, rows.Err()
}

// newToken returns a fresh token and the hash that is stored for it.
func newToken() (token string, hash []byte, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, err
	}
	token = hex.EncodeToString(raw)
	return token, tokenHash(token), nil
}

func tokenHash(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

func hashHex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func rawOrNil(s *string) json.RawMessage {
	if s == nil {
		return nil
	}
	return json.RawMessage(*s)
}

func compileSchema(schema map[string]any) (*jsonschema.Schema, error) {
	raw, err := json.Marshal(schema)
	if err != nil {
		return nil, err
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource("schema.json", doc); err != nil {
		return nil, err
	}
	return c.Compile("schema.json")
}

// validateJSON checks value against schema.
func validateJSON(schema map[string]any, value json.RawMessage) error {
	sch, err := compileSchema(schema)
	if err != nil {
		return fmt.Errorf("schema: %w", err)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(value))
	if err != nil {
		return fmt.Errorf("not valid JSON: %w", err)
	}
	return sch.Validate(doc)
}
