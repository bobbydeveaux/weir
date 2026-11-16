<script lang="ts">
	import type { ApiError } from '#lib/api.ts';

	// What a section shows when its read failed. When the API cannot be
	// reached at all, the banner at the top of the page says why, so this
	// stays short.
	let { error, what = 'this' }: { error: ApiError | null | undefined; what?: string } = $props();
</script>

{#if error}
	<div class="note" class:down={error.unreachable} role="alert">
		{#if error.unreachable}
			<strong>No data for {what}</strong>
			<span>The Weir API cannot be reached. Trying again every few seconds.</span>
		{:else if error.status === 404}
			<strong>Not found</strong>
			<span>{error.message}</span>
		{:else}
			<strong>Could not load {what}</strong>
			<span>{error.message}{error.status ? ` (HTTP ${error.status})` : ''}</span>
		{/if}
	</div>
{/if}

<style>
	.note {
		display: flex;
		flex-direction: column;
		gap: 2px;
		padding: 12px 14px;
		border: 1px solid color-mix(in srgb, var(--fail) 40%, var(--line));
		background: color-mix(in srgb, var(--fail) 8%, var(--surface));
		border-radius: var(--radius);
		overflow-wrap: anywhere;
	}

	.note strong {
		color: var(--fail);
		font-weight: 680;
	}

	.note span {
		color: var(--muted);
	}

	.down {
		border-style: dashed;
		border-color: var(--line-strong);
		background: none;
	}

	.down strong {
		color: var(--text);
	}
</style>
