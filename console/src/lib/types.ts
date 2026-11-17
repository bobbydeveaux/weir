// The shapes the Weir API returns. Field names follow the Go structs and the
// SQL in engine/views.go exactly; nothing here is invented.

export type StepKind = 'decide' | 'ai' | 'deterministic' | 'human' | 'external';
export const STEP_KINDS: StepKind[] = ['decide', 'ai', 'deterministic', 'human', 'external'];

export type RunStatus = 'running' | 'waiting' | 'blocked' | 'compensating' | 'succeeded' | 'failed' | 'cancelled';
export const RUN_STATUSES: RunStatus[] = ['running', 'waiting', 'blocked', 'compensating', 'succeeded', 'failed', 'cancelled'];

export type StepStatus =
	| 'runnable'
	| 'running'
	| 'waiting'
	| 'succeeded'
	| 'failed'
	| 'skipped'
	| 'cancelled'
	| 'compensated';

/** Any JSON value the API passes through untouched. */
export type Json = unknown;

// --- workspaces ---------------------------------------------------------------

export interface Workspace {
	slug: string;
	name: string;
	created_at: string;
	workflows: number;
	runs: number;
}

// --- events and triggers -----------------------------------------------------------

/** What an event did for one workflow whose trigger matched. */
export interface Triggered {
	namespace: string;
	name: string;
	run_id?: string;
	error?: string;
}

/** One row of GET /v1/events. */
export interface InboundEvent {
	id: string;
	type: string;
	source: string;
	key: string | null;
	data: Json;
	sent_by: string;
	received_at: string;
	runs: Triggered[] | null;
}

/** One row of GET /v1/triggers: an event type and a workflow it starts. */
export interface TriggerRow {
	/** The event that starts the workflow, or null for a schedule. */
	event: string | null;
	/** A cron expression in UTC, or null for an event. */
	schedule: string | null;
	next_at: string | null;
	last_at: string | null;
	last_error: string | null;
	namespace: string;
	name: string;
	version: number;
	when: string | null;
}

// --- definitions ---------------------------------------------------------------

/** One row of GET /v1/definitions. */
export interface DefinitionSummary {
	namespace: string;
	name: string;
	created_at: string;
	latest_version: number | null;
	live_version: number | null;
	live_published_at: string | null;
	live_published_by: string | null;
	steps: { id: string; kind: StepKind }[];
	runs: number;
	active: number;
	/** Runs held at a failed step, waiting for a person to resume or cancel them. */
	blocked: number;
	succeeded: number;
	failed: number;
	spent_usd: number;
	last_run_at: string | null;
}

export interface SpecSchema {
	schema: Record<string, Json>;
}

export interface SpecBudget {
	usd?: number;
	minutes?: number;
}

export interface SpecNext {
	when?: string;
	go: string;
}

export interface SpecStep {
	id: string;
	kind: StepKind;

	// shared
	input?: Record<string, Json>;
	output?: SpecSchema;
	retry?: { attempts?: number; backoff?: string };
	timeout?: string;
	next?: SpecNext[];
	on_failure?: string;
	compensate?: string;
	end?: boolean;

	// decide
	template?: string;
	labels?: string[];
	confidence?: number;
	default?: string;
	describe?: Record<string, string>;
	provider?: string;

	// ai
	prompt?: string;
	tier?: string;
	tools?: { read?: string[]; write?: string[]; approve?: string[] };
	budget?: SpecBudget;

	// deterministic
	handler?: string;

	// human
	assign?: { role?: string; user?: string; not?: string };
	escalate?: { after: string; role: string }[];
	outcomes?: string[];

	// external
	connection?: string;
	call?: { method?: string; path: string; body?: Record<string, Json> };
	resume?: string;
	poll?: { every?: string; until: string };

	// accepted by the engine but not yet acted on
	hooks?: Json;
	sandbox?: Json;
}

export interface Spec {
	weir: string;
	namespace: string;
	name: string;
	version?: number;
	input?: SpecSchema;
	budget?: SpecBudget;
	steps: SpecStep[];
	triggers?: Json;
	tiers?: Json;
	hooks?: Json;
}

export interface DefinitionVersion {
	version: number;
	published_by: string;
	published_at: string;
	spec_hash: string;
	runs: number;
}

export interface DefinitionAlias {
	name: string;
	version: number;
	updated_by: string;
	updated_at: string;
}

/** GET /v1/definitions/{namespace}/{name}. */
export interface DefinitionDetail {
	namespace: string;
	name: string;
	created_at: string;
	version: number;
	source: string;
	spec: Spec;
	spec_hash: string;
	contract_version: string;
	published_by: string;
	published_at: string;
	versions: DefinitionVersion[];
	aliases: DefinitionAlias[];
}

// --- runs ----------------------------------------------------------------------

/** One row of GET /v1/runs. */
export interface RunSummary {
	id: string;
	namespace: string;
	name: string;
	version: number;
	status: RunStatus;
	requester: string;
	spent_usd: number;
	budget_usd: number | null;
	created_at: string;
	finished_at: string | null;
	current_step: string | null;
	steps_done: number;
	steps_total: number;
}

export interface StepView {
	key: string;
	visit: number;
	kind: StepKind;
	status: StepStatus;
	attempts: number;
	label?: string;
	outcome?: string;
	output?: Json;
	error?: Json;
	/** Not sent today; rendered if the API starts sending it. */
	data?: Json;
}

export interface EventView {
	seq: number;
	type: string;
	actor: string;
	step?: string;
	data?: Json;
}

/** GET /v1/runs/{id}. */
export interface RunView {
	id: string;
	status: RunStatus;
	input: Json;
	output?: Json;
	error?: Json;
	spent_usd: number;
	steps: StepView[];
	events: EventView[];
}

export interface AiCall {
	step: string;
	visit: number;
	attempt: number;
	runner: string | null;
	provider: string | null;
	model: string | null;
	template: string | null;
	template_version: number | null;
	prompt_hash: string | null;
	tokens_in: number | null;
	tokens_out: number | null;
	cost_usd: number | null;
	cost_estimated: boolean | null;
	duration_ms: number | null;
	error: string | null;
	at: string;
	/** May be added by the API soon. */
	prompt?: string | null;
	response?: string | null;
	data?: Json;
}

export interface ToolCall {
	step: string;
	visit: number;
	tool: string;
	class: string;
	status: string;
	args: Json;
	result: Json;
	error: string | null;
	duration_ms: number | null;
	at: string;
}

export interface StepTime {
	step: string;
	visit: number;
	input: Json;
	started_at: string;
	finished_at: string | null;
	spent_usd: number | null;
}

/** One entry of GET /v1/runs/{id}/files: a file a step produced. */
export interface RunFile {
	step: string;
	path: string;
	size: number;
	sha256: string;
	content_type: string;
	created_at: string;
}

/** GET /v1/runs/{id}/record. */
export interface RunRecord {
	namespace: string;
	name: string;
	version: number;
	requester: string;
	created_at: string;
	finished_at: string | null;
	budget_usd: number | null;
	ai_calls: AiCall[];
	tool_calls: ToolCall[];
	step_times: StepTime[];
	event_times: { seq: number; at: string }[];
	/** Not sent today; rendered per step if the API starts sending it. */
	decisions?: ({ step?: string; visit?: number; data?: Json } & Record<string, Json>)[];
}

export interface StartRunRequest {
	namespace: string;
	name: string;
	alias?: string;
	input?: Json;
	idempotency_key?: string;
}

export interface StartRunResponse {
	id: string;
	created: boolean;
}

/** What the diagram shows on a step while displaying a run. Console-side only. */
export interface StepOverlay {
	status: StepStatus;
	visits: number;
	/** The label a decide step chose, or a human step's outcome. */
	note?: string;
}

// --- tasks ---------------------------------------------------------------------

/** One row of GET /v1/tasks: a human step that is waiting. */
export interface Task {
	id: string;
	run_id: string;
	step: string;
	role?: string;
	user?: string;
	outcomes: string[] | null;
	escalation_level: number;
	namespace: string;
	name: string;
	input: Json;
	created_at: string;
}

// --- templates -----------------------------------------------------------------

export interface TemplateBody {
	system?: string;
	user: string;
	model?: string;
}

/** One row of GET /v1/templates: a template at its latest version. */
export interface TemplateSummary {
	key: string;
	version: number;
	body: TemplateBody;
	body_hash: string;
	published_by: string;
	published_at: string;
	versions: number;
}

export interface TemplateVersion {
	version: number;
	body: TemplateBody;
	body_hash: string;
	published_by: string;
	published_at: string;
}

/** GET /v1/templates/{key}. */
export interface TemplateDetail {
	key: string;
	versions: TemplateVersion[];
}

// --- connections and registry -----------------------------------------------------

export interface Connection {
	name: string;
	/** "http" for a plain address, "mcp" for an MCP server that brings tools. */
	kind: string;
	base_url: string;
	/** For an MCP connection: every tool is treated as a read. */
	read_only: boolean;
	/** For an MCP connection: what the server offered when last asked. */
	tools: Tool[] | null;
	synced_at: string | null;
	/** How Weir signs in. The secret is a reference such as env:NAME, never a value. */
	auth: { kind: string; secret?: string; header?: string; username?: string } | null;
	/** How a third party signs what it sends to Weir through this connection. Null if it receives nothing. */
	webhook: { kind: string; secret?: string; header?: string; event_header?: string } | null;
	has_secret: boolean;
	created_at: string;
}

export interface Tool {
	name: string;
	description: string;
	class: string;
	input_schema: Record<string, Json> | null;
	/** The MCP connection the tool comes from. Empty for the server's own tools. */
	connection?: string;
}

/** GET /v1/registry. */
export interface Registry {
	handlers: string[] | null;
	tools: Tool[] | null;
	decide_providers: string[] | null;
}

// --- usage ---------------------------------------------------------------------

export interface UsageByWorkflow {
	namespace: string;
	name: string;
	runs: number;
	succeeded: number;
	failed: number;
	spent_usd: number;
}

export interface UsageByModel {
	provider: string | null;
	model: string | null;
	calls: number;
	tokens_in: number | null;
	tokens_out: number | null;
	cost_usd: number | null;
	cost_estimated: boolean | null;
	avg_ms: number | null;
	errors: number;
}

export interface UsageByDay {
	day: string; // YYYY-MM-DD
	runs: number;
	spent_usd: number;
}

/** GET /v1/usage. */
export interface Usage {
	days: number;
	namespace: string;
	totals: { runs: number; active: number; blocked: number; succeeded: number; failed: number; spent_usd: number };
	ai: { calls: number; tokens_in: number; tokens_out: number; cost_usd: number };
	by_workflow: UsageByWorkflow[];
	by_model: UsageByModel[];
	by_day: UsageByDay[];
}
