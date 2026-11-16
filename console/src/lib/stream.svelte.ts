// Follow one of Weir's event streams. The stream only says that something
// moved; what moved is read through the ordinary API, so the page shows the
// same thing whether the news came by stream or by the slow fallback poll.
// Call useStream during component initialisation.

import { session } from './session.svelte';

/**
 * Opens the stream at path() (under /v1) for the current workspace, and calls
 * onMove shortly after each event, once for a burst of them. The browser
 * reconnects by itself and says where it got to, so nothing is missed.
 */
export function useStream(path: () => string | null, onMove: () => void): void {
	$effect(() => {
		const p = path();
		if (!p || typeof EventSource === 'undefined') return;
		const url = `/v1${p}${p.includes('?') ? '&' : '?'}workspace=${encodeURIComponent(session.workspace || 'default')}`;
		const source = new EventSource(url);
		let timer: ReturnType<typeof setTimeout> | undefined;
		const nudge = () => {
			clearTimeout(timer);
			timer = setTimeout(onMove, 120);
		};
		source.onmessage = nudge;
		return () => {
			clearTimeout(timer);
			source.close();
		};
	});
}
