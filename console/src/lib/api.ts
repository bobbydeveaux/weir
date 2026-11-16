// The one place the console talks to the Weir API. Every call goes to the
// relative path /v1 and carries the workspace and, for writes, the actor.

import { health, session } from './session.svelte';
import type {
	Connection,
	DefinitionDetail,
	DefinitionSummary,
	InboundEvent,
	Registry,
	TriggerRow,
	RunRecord,
	RunSummary,
	RunView,
	StartRunRequest,
	StartRunResponse,
	Task,
	TemplateDetail,
	TemplateSummary,
	Usage,
	Workspace
} from './types';

export const API_BASE = '/v1';

export class ApiError extends Error {
	/** HTTP status, or 0 when no response arrived or the request was never sent. */
	status: number;
	/** True when the API itself could not be reached, as opposed to it saying no. */
	unreachable: boolean;
	url: string;

	constructor(message: string, status: number, url: string, unreachable = false) {
		super(message);
		this.name = 'ApiError';
		this.status = status;
		this.url = url;
		this.unreachable = unreachable;
	}
}

export function toApiError(e: unknown): ApiError {
	if (e instanceof ApiError) return e;
	return new ApiError(e instanceof Error ? e.message : String(e), 0, '');
}

interface Options {
	query?: Record<string, string | number | undefined | null>;
	body?: unknown;
	/** Writes need an actor. */
	write?: boolean;
	/** False for the calls that are not about one workspace. */
	scoped?: boolean;
}

function absolute(path: string): string {
	return typeof location === 'undefined' ? path : location.origin + path;
}

async function request<T>(method: string, path: string, opts: Options = {}): Promise<T> {
	const params = new URLSearchParams();
	for (const [k, v] of Object.entries(opts.query ?? {})) {
		if (v !== undefined && v !== null && v !== '' && v !== 0) params.set(k, String(v));
	}
	const qs = params.toString();
	const url = API_BASE + path + (qs ? '?' + qs : '');

	// Read the session before the first await, so callers inside an effect
	// re-run when the workspace changes.
	const headers: Record<string, string> = { Accept: 'application/json' };
	if (opts.scoped !== false) headers['X-Weir-Workspace'] = session.workspace || 'default';
	if (opts.write) {
		if (!session.actor) {
			throw new ApiError('Set your name in the "Acting as" field at the top of the page first: Weir records who made every change.', 0, url);
		}
		headers['X-Weir-Actor'] = session.actor;
	}
	if (opts.body !== undefined) headers['Content-Type'] = 'application/json';

	let res: Response;
	try {
		res = await fetch(url, { method, headers, body: opts.body === undefined ? undefined : JSON.stringify(opts.body) });
	} catch (e) {
		throw down(url, e instanceof Error ? e.message : 'the request failed');
	}

	const text = await res.text();
	let parsed: unknown;
	let isJson = false;
	if (text) {
		try {
			parsed = JSON.parse(text);
			isJson = true;
		} catch {
			// Not JSON: an HTML error page or a proxy's plain-text message.
		}
	}

	// A proxy answers for an API that is down with a gateway error or a
	// non-JSON 5xx; a static host with no API answers with HTML.
	if ((!isJson && (res.status >= 500 || res.ok)) || [502, 503, 504].includes(res.status)) {
		if (res.ok && !text) {
			health.unreachable = false;
			return undefined as T;
		}
		throw down(url, `HTTP ${res.status}${res.ok ? ' with a body that is not JSON' : ''}`);
	}

	health.unreachable = false;
	if (!res.ok) {
		const message =
			isJson && parsed && typeof parsed === 'object' && 'error' in parsed
				? String((parsed as { error: unknown }).error)
				: `The API answered ${res.status} ${res.statusText}`.trim();
		throw new ApiError(message, res.status, url);
	}
	return parsed as T;
}

function down(url: string, detail: string): ApiError {
	health.unreachable = true;
	health.url = absolute(url);
	health.detail = detail;
	return new ApiError(`Cannot reach the Weir API at ${absolute(url)} (${detail}).`, 0, url, true);
}

const enc = encodeURIComponent;

export interface RunsFilter {
	namespace?: string;
	name?: string;
	status?: string;
	limit?: number;
}

export const api = {
	// Workspaces are not themselves inside a workspace, so these two calls do
	// not send the workspace header: a stale choice must not break the switcher.
	workspaces: () => request<Workspace[] | null>('GET', '/workspaces', { scoped: false }).then((w) => w ?? []),
	createWorkspace: (slug: string, name: string) =>
		request<{ slug: string }>('POST', '/workspaces', { body: { slug, name }, write: true, scoped: false }),

	definitions: () => request<DefinitionSummary[]>('GET', '/definitions'),
	definition: (namespace: string, name: string, version?: number) =>
		request<DefinitionDetail>('GET', `/definitions/${enc(namespace)}/${enc(name)}`, { query: { version } }),

	runs: (filter: RunsFilter = {}) => request<RunSummary[]>('GET', '/runs', { query: { ...filter } }),
	run: (id: string) =>
		request<RunView>('GET', `/runs/${enc(id)}`).then((r) => ({ ...r, steps: r.steps ?? [], events: r.events ?? [] })),
	runRecord: (id: string) => request<RunRecord>('GET', `/runs/${enc(id)}/record`),
	startRun: (body: StartRunRequest) => request<StartRunResponse>('POST', '/runs', { body, write: true }),
	/** budgetUsd, when above zero, replaces the run's budget. */
	resumeRun: (id: string, budgetUsd?: number) =>
		request<{ status: string }>('POST', `/runs/${enc(id)}/resume`, {
			body: budgetUsd && budgetUsd > 0 ? { budget_usd: budgetUsd } : {},
			write: true
		}),

	cancelRun: (id: string, reason: string) =>
		request<{ status: string }>('POST', `/runs/${enc(id)}/cancel`, { body: { reason }, write: true }),

	tasks: () => request<Task[] | null>('GET', '/tasks').then((t) => t ?? []),
	decideTask: (id: string, outcome: string, note: string) =>
		request<{ status: string }>('POST', `/tasks/${enc(id)}/decision`, { body: { outcome, note }, write: true }),

	templates: () => request<TemplateSummary[]>('GET', '/templates'),
	template: (key: string) => request<TemplateDetail>('GET', `/templates/${enc(key)}`),

	connections: () => request<Connection[]>('GET', '/connections'),
	usage: (days: number, namespace?: string) => request<Usage>('GET', '/usage', { query: { days, namespace } }),
	registry: () => request<Registry>('GET', '/registry'),

	events: (limit = 50) => request<InboundEvent[] | null>('GET', '/events', { query: { limit } }).then((e) => e ?? []),
	triggers: () => request<TriggerRow[] | null>('GET', '/triggers').then((t) => t ?? [])
};
