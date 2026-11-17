// Who is asking and for which workspace, plus what the console knows about
// whether the API can be reached. Shared by every page.

const WORKSPACE_KEY = 'weir.workspace';
const ACTOR_KEY = 'weir.actor';

// Storage can be missing or blocked (private windows, strict settings), so
// every access is guarded and the console works without it.
function read(key: string, fallback: string): string {
	try {
		return localStorage.getItem(key) ?? fallback;
	} catch {
		return fallback;
	}
}

function write(key: string, value: string) {
	try {
		localStorage.setItem(key, value);
	} catch {
		// Not remembered; nothing else to do.
	}
}

// A link may name its workspace (?workspace=acme), so that a link sent to
// someone opens in the right place. It is then remembered like any other choice.
function linked(): string | null {
	try {
		const slug = new URLSearchParams(location.search).get('workspace');
		if (slug) write(WORKSPACE_KEY, slug);
		return slug;
	} catch {
		return null;
	}
}

// The API key is kept for this browser tab only, and is gone when the tab
// closes: it is a credential, and should not outlive the visit.
const SECRET_KEY = 'weir.key';

function readSecret(): string {
	try {
		return sessionStorage.getItem(SECRET_KEY) ?? '';
	} catch {
		return '';
	}
}

export function setKey(key: string) {
	session.key = key.trim();
	try {
		if (session.key) sessionStorage.setItem(SECRET_KEY, session.key);
		else sessionStorage.removeItem(SECRET_KEY);
	} catch {
		// Not kept; it lasts until the page is reloaded.
	}
}

export const session = $state({
	/** The API key, when the server asks for one. Empty on a server that does not. */
	key: readSecret(),
	workspace: linked() ?? read(WORKSPACE_KEY, 'default'),
	actor: read(ACTOR_KEY, '')
});

export function setWorkspace(slug: string) {
	session.workspace = slug || 'default';
	write(WORKSPACE_KEY, session.workspace);
}

export function setActor(name: string) {
	session.actor = name.trim();
	write(ACTOR_KEY, session.actor);
}

/** Set by the API client after every request. */
export const health = $state({
	unreachable: false,
	/** The URL of the last request that could not reach the API. */
	url: '',
	detail: ''
});

/**
 * Counters a page bumps after a write, so that reads elsewhere on screen (the
 * workspace switcher, the task count in the nav) fetch again.
 */
export const stale = $state({ workspaces: 0, tasks: 0 });

/** A coarse clock, so relative times on screen keep moving. */
export const clock = $state({ now: Date.now() });

export function tick() {
	clock.now = Date.now();
}
