<script lang="ts">
	import { tone } from '#lib/format.ts';

	let { status, small = false }: { status: string; small?: boolean } = $props();

	const live = $derived(status === 'running' || status === 'compensating');
</script>

<span class="pill t-{tone(status)}" class:small class:live>{status}</span>

<style>
	.pill {
		display: inline-flex;
		align-items: center;
		gap: 6px;
		padding: 2px 9px 2px 8px;
		border-radius: 999px;
		font-size: 12px;
		font-weight: 650;
		line-height: 1.45;
		white-space: nowrap;
		color: var(--t);
		background: color-mix(in srgb, var(--t) 13%, transparent);
	}

	.pill::before {
		content: '';
		width: 6.5px;
		height: 6.5px;
		border-radius: 50%;
		background: currentColor;
		flex: none;
	}

	.live::before {
		animation: pulse 1.1s ease-in-out infinite;
	}

	.small {
		font-size: 11px;
		padding: 0.5px 7px 0.5px 6px;
		gap: 5px;
	}
</style>
