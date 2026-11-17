// Follow one of Weir's event streams. The stream only says that something
// moved; what moved is read through the ordinary API, so the page shows the
// same thing whether the news came by stream or by the slow fallback poll.
// Call useStream during component initialisation.
//
// The stream is read with fetch and not with the browser's EventSource,
// because EventSource cannot send headers, and the API key and the workspace
// travel in headers.

import { session } from './session.svelte';

/** How long to wait before reconnecting after the stream drops. */
const RETRY_MS = 3000;

/**
 * Opens the stream at path() (under /v1) for the current workspace, and calls
 * onMove shortly after each event, once for a burst of them. If the stream
 * drops it is opened again, saying which event was the last one seen, so
 * nothing is missed.
 */
export function useStream(path: () => string | null, onMove: () => void): void {
	$effect(() => {
		const p = path();
		const workspace = session.workspace || 'default';
		const key = session.key;
		if (!p) return;

		const stop = new AbortController();
		let nudgeTimer: ReturnType<typeof setTimeout> | undefined;
		let retryTimer: ReturnType<typeof setTimeout> | undefined;
		let last = '';

		const nudge = () => {
			clearTimeout(nudgeTimer);
			nudgeTimer = setTimeout(onMove, 120);
		};

		async function follow() {
			const headers: Record<string, string> = { Accept: 'text/event-stream', 'X-Weir-Workspace': workspace };
			if (key) headers['Authorization'] = `Bearer ${key}`;
			if (last) headers['Last-Event-ID'] = last;
			try {
				const res = await fetch(`/v1${p}`, { headers, signal: stop.signal });
				if (!res.ok || !res.body) throw new Error(`stream answered ${res.status}`);
				const reader = res.body.pipeThrough(new TextDecoderStream()).getReader();
				let buffer = '';
				for (;;) {
					const { value, done } = await reader.read();
					if (done) break;
					buffer += value;
					// Messages are separated by a blank line.
					let end: number;
					while ((end = buffer.indexOf('\n\n')) >= 0) {
						const message = buffer.slice(0, end);
						buffer = buffer.slice(end + 2);
						let hasData = false;
						for (const line of message.split('\n')) {
							if (line.startsWith('id:')) last = line.slice(3).trim();
							if (line.startsWith('data:')) hasData = true;
						}
						if (hasData) nudge(); // a comment line is only a heartbeat
					}
				}
			} catch {
				// Dropped, refused or aborted. The poll carries on meanwhile.
			}
			if (!stop.signal.aborted) retryTimer = setTimeout(follow, RETRY_MS);
		}
		void follow();

		return () => {
			stop.abort();
			clearTimeout(nudgeTimer);
			clearTimeout(retryTimer);
		};
	});
}
