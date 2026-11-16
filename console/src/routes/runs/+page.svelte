<script lang="ts">
	import { api } from '#lib/api.ts';
	import ErrorNote from '#lib/components/ErrorNote.svelte';
	import RunsTable from '#lib/components/RunsTable.svelte';
	import { isActive } from '#lib/format.ts';
	import { useQuery } from '#lib/query.svelte.ts';
	import { useStream } from '#lib/stream.svelte.ts';
	import { RUN_STATUSES } from '#lib/types.ts';

	const LIMITS = [50, 100, 200];

	let workflow = $state(''); // "namespace/name", or empty for all
	let status = $state('');
	let limit = $state(50);

	const defs = useQuery(() => api.definitions(), { keep: true });

	const runs = useQuery(
		() => {
			const [namespace, name] = workflow ? workflow.split('/') : ['', ''];
			return api.runs({ namespace, name, status, limit });
		},
		// Every 2 s while anything listed is still moving; a slow check otherwise, so new runs show up.
		{ keep: true, refresh: () => 10000 }
	);
	// Any run in the workspace moving refreshes the list at once.
	useStream(() => '/stream', () => runs.reload());

	const live = $derived(runs.data?.some((r) => isActive(r.status)) ?? false);
	const filtered = $derived(workflow !== '' || status !== '');
</script>

<svelte:head><title>Runs · Weir console</title></svelte:head>

<div class="page">
	<div class="page-head">
		<div>
			<h1>Runs</h1>
			<p class="sub">
				Newest first.
				{#if live}<span class="livetag"><i></i>updating live</span>{/if}
			</p>
		</div>
		<div class="row">
			<label class="field">
				<span>Workflow</span>
				<select class="select" bind:value={workflow}>
					<option value="">All workflows</option>
					{#each defs.data ?? [] as d (d.namespace + '/' + d.name)}
						<option value="{d.namespace}/{d.name}">{d.namespace}/{d.name}</option>
					{/each}
				</select>
			</label>
			<label class="field">
				<span>Status</span>
				<select class="select" bind:value={status}>
					<option value="">Any status</option>
					{#each RUN_STATUSES as s (s)}<option value={s}>{s}</option>{/each}
				</select>
			</label>
			<label class="field">
				<span>Show</span>
				<select class="select" bind:value={limit}>
					{#each LIMITS as n (n)}<option value={n}>{n}</option>{/each}
				</select>
			</label>
		</div>
	</div>

	<section class="panel" aria-busy={runs.loading}>
		{#if runs.error && !runs.data}
			<div class="panel-body"><ErrorNote error={runs.error} what="runs" /></div>
		{:else if !runs.data}
			<div class="panel-body"><div class="skeleton" style="height: 220px"></div></div>
		{:else if runs.data.length === 0}
			<div class="empty">
				{#if filtered}
					<strong>No runs match these filters</strong>
					<button type="button" class="btn small" onclick={() => ((workflow = ''), (status = ''))}>Clear filters</button>
				{:else}
					<strong>Nothing has run in this workspace</strong>
					Open a <a class="link" href="/workflows">workflow</a> and start a run.
				{/if}
			</div>
		{:else}
			<RunsTable runs={runs.data} />
			{#if runs.data.length >= limit}
				<p class="more muted">Showing the newest {limit}. The API returns at most 200; narrow the filters to see older runs.</p>
			{/if}
		{/if}
	</section>
	{#if runs.error && runs.data}
		<ErrorNote error={runs.error} what="the latest runs" />
	{/if}
</div>

<style>
	.livetag {
		display: inline-flex;
		align-items: center;
		gap: 6px;
		margin-left: 6px;
		color: var(--accent);
		font-weight: 600;
	}

	.livetag i {
		width: 7px;
		height: 7px;
		border-radius: 50%;
		background: currentColor;
		animation: pulse 1.1s ease-in-out infinite;
	}

	.more {
		padding: 10px 14px;
		border-top: 1px solid var(--line);
		font-size: 12.5px;
	}
</style>
