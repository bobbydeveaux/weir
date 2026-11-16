<script lang="ts">
	import { isRecord } from '#lib/format.ts';
	import Readable from './Readable.svelte';

	// JSON laid out for a person to read: keys as labels, text as text.
	let { value, depth = 0 }: { value: unknown; depth?: number } = $props();

	const label = (key: string) => key.replace(/[_-]+/g, ' ');
	const simple = (v: unknown) => v === null || ['string', 'number', 'boolean'].includes(typeof v);
</script>

{#if value === null || value === undefined}
	<span class="faint">none</span>
{:else if typeof value === 'string'}
	<span class="text">{value}</span>
{:else if typeof value === 'number' || typeof value === 'boolean'}
	<span class="mono">{String(value)}</span>
{:else if Array.isArray(value)}
	{#if value.length === 0}
		<span class="faint">empty list</span>
	{:else if value.every(simple)}
		<span class="text">{value.map((v) => String(v)).join(', ')}</span>
	{:else}
		<ol>
			{#each value as item, i (i)}
				<li><Readable value={item} depth={depth + 1} /></li>
			{/each}
		</ol>
	{/if}
{:else if isRecord(value)}
	{#if Object.keys(value).length === 0}
		<span class="faint">nothing</span>
	{:else if depth > 4}
		<span class="mono">{JSON.stringify(value)}</span>
	{:else}
		<dl class:nested={depth > 0}>
			{#each Object.entries(value) as [key, item] (key)}
				<dt>{label(key)}</dt>
				<dd><Readable value={item} depth={depth + 1} /></dd>
			{/each}
		</dl>
	{/if}
{/if}

<style>
	dl {
		display: grid;
		grid-template-columns: minmax(70px, max-content) minmax(0, 1fr);
		gap: 6px 14px;
		margin: 0;
	}

	dl.nested {
		padding-left: 10px;
		border-left: 2px solid var(--line);
	}

	dt {
		color: var(--muted);
		font-size: 12px;
		font-weight: 620;
		text-transform: capitalize;
		padding-top: 1px;
	}

	dd {
		margin: 0;
		min-width: 0;
	}

	ol {
		margin: 0;
		padding-left: 18px;
		display: grid;
		gap: 6px;
	}

	.text {
		white-space: pre-wrap;
		overflow-wrap: anywhere;
	}
</style>
