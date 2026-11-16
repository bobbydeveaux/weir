<script lang="ts">
	import type { StepKind } from '#lib/types.ts';
	import KindIcon from './KindIcon.svelte';

	// A step, or just a kind, in that kind's colour. With no label it names the kind.
	let { kind, label, title }: { kind: StepKind | string; label?: string; title?: string } = $props();
</script>

<span class="chip k-{kind}" title={title ?? (label ? `${label} (${kind})` : kind)}>
	<KindIcon {kind} size={12} />
	<span>{label ?? kind}</span>
</span>

<style>
	.chip {
		--k: var(--muted);
		display: inline-flex;
		align-items: center;
		gap: 5px;
		padding: 1.5px 7px 1.5px 6px;
		border-radius: 5px;
		font: 600 11.5px/1.5 var(--mono);
		color: var(--k);
		background: color-mix(in srgb, var(--k) 12%, transparent);
		border: 1px solid color-mix(in srgb, var(--k) 34%, transparent);
		white-space: nowrap;
		max-width: 100%;
	}

	.chip span {
		overflow: hidden;
		text-overflow: ellipsis;
	}
</style>
