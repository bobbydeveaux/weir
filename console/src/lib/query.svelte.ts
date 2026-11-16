// A small reactive wrapper around one API read: loading, data, error, and
// optional polling. Call useQuery during component initialisation.

import { toApiError, type ApiError } from './api';

export class Query<T> {
	data = $state.raw<T | undefined>(undefined);
	error = $state.raw<ApiError | null>(null);
	/** True until the first answer for the current inputs arrives. */
	loading = $state(true);
	/** Fetch again now, without clearing what is on screen. */
	reload: () => void = () => {};
}

export interface QueryOptions<T> {
	/** Milliseconds until the next silent refetch, given the latest data. Zero stops polling. */
	refresh?: (data: T | undefined) => number;
	/** Keep the previous data on screen while new inputs load. */
	keep?: boolean;
}

/** How soon to try again after a failure, so the page recovers by itself. */
const RETRY_MS = 5000;

/**
 * Runs fetcher now and again whenever any state it reads changes (including
 * the workspace, which the API client reads). A fetcher that returns null is
 * waiting on something and fetches nothing.
 */
export function useQuery<T>(fetcher: () => Promise<T> | null, opts: QueryOptions<T> = {}): Query<T> {
	const q = new Query<T>();
	let token = 0;
	let timer: ReturnType<typeof setTimeout> | undefined;

	function run(silent: boolean) {
		const mine = ++token;
		clearTimeout(timer);
		let pending: Promise<T> | null;
		try {
			pending = fetcher();
		} catch (e) {
			pending = Promise.reject(e);
		}
		if (!pending) {
			q.data = undefined;
			q.error = null;
			q.loading = true;
			return;
		}
		if (!silent) {
			q.loading = true;
			if (!opts.keep) q.data = undefined;
		}
		pending
			.then(
				(data) => {
					if (mine !== token) return;
					q.data = data;
					q.error = null;
				},
				(e) => {
					if (mine !== token) return;
					q.error = toApiError(e);
				}
			)
			.finally(() => {
				if (mine !== token) return;
				q.loading = false;
				const ms = q.error ? RETRY_MS : (opts.refresh?.(q.data) ?? 0);
				if (ms > 0) timer = setTimeout(() => run(true), ms);
			});
	}

	q.reload = () => run(true);

	$effect(() => {
		run(false);
		return () => {
			token++;
			clearTimeout(timer);
		};
	});

	return q;
}
