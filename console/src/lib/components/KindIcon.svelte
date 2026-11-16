<script lang="ts">
	import type { StepKind } from '#lib/types.ts';

	let { kind, size = 14 }: { kind: StepKind | string; size?: number } = $props();
</script>

<svg viewBox="0 0 16 16" width={size} height={size} fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
	{#if kind === 'decide'}
		<path d="M8 1.6 14.4 8 8 14.4 1.6 8Z" />
		<path d="M5.6 8h4.8M8.6 6.2 10.4 8 8.6 9.8" />
	{:else if kind === 'ai'}
		<path d="M7 2.2 8.3 6l3.9 1.3-3.9 1.3L7 12.4 5.7 8.6 1.8 7.3 5.7 6Z" />
		<path d="M12.6 10.6v3M11.1 12.1h3" />
	{:else if kind === 'deterministic'}
		<path d="M5.6 3.4 1.8 8l3.8 4.6M10.4 3.4 14.2 8l-3.8 4.6" />
	{:else if kind === 'human'}
		<circle cx="8" cy="5" r="2.7" />
		<path d="M2.6 14.2c.6-3 2.7-4.4 5.4-4.4s4.8 1.4 5.4 4.4" />
	{:else if kind === 'external'}
		<path d="M9.2 2.4h4.4v4.4M13.6 2.4 7.6 8.4M11.6 9.8v3.8H2.4V4.4h3.8" />
	{:else}
		<circle cx="8" cy="8" r="5.5" />
	{/if}
</svg>
