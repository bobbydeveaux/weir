<script lang="ts">
	import { absTime, relTime } from '#lib/format.ts';
	import { clock } from '#lib/session.svelte.ts';

	// A time said relatively, with the exact time on hover.
	let { at }: { at: string | null | undefined } = $props();
</script>

{#if at}
	<time datetime={at} title={absTime(at)}>{relTime(at, clock.now)}</time>
{:else}
	<span class="faint">—</span>
{/if}

<style>
	time {
		white-space: nowrap;
	}
</style>
