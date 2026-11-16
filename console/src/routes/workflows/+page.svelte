<script lang="ts">
	import { api } from '#lib/api.ts';
	import ErrorNote from '#lib/components/ErrorNote.svelte';
	import KindChip from '#lib/components/KindChip.svelte';
	import Time from '#lib/components/Time.svelte';
	import { compact, money } from '#lib/format.ts';
	import { useQuery } from '#lib/query.svelte.ts';
	import { STEP_KINDS } from '#lib/types.ts';

	const defs = useQuery(() => api.definitions(), { keep: true, refresh: (d) => (d?.some((w) => w.active > 0) ? 5000 : 30000) });

	let search = $state('');
	const shown = $derived(
		(defs.data ?? []).filter((d) => !search.trim() || `${d.namespace}/${d.name}`.toLowerCase().includes(search.trim().toLowerCase()))
	);
</script>

<svelte:head><title>Workflows · Weir console</title></svelte:head>

<div class="page">
	<div class="page-head">
		<div>
			<h1>Workflows</h1>
			<p class="sub">Every definition in this workspace, at its live version.</p>
		</div>
		<div class="row">
			<div class="legend" aria-label="Step kinds">
				{#each STEP_KINDS as kind (kind)}<KindChip {kind} />{/each}
			</div>
			<input class="input" type="search" placeholder="Filter by name" bind:value={search} aria-label="Filter workflows" />
		</div>
	</div>

	{#if defs.error && !defs.data}
		<ErrorNote error={defs.error} what="workflows" />
	{:else if !defs.data}
		<div class="cards">
			{#each Array(3) as _, i (i)}<div class="card"><div class="skeleton" style="height: 120px"></div></div>{/each}
		</div>
	{:else if defs.data.length === 0}
		<div class="panel empty">
			<strong>No workflows yet</strong>
			Publish a definition with <code class="mono">POST /v1/definitions</code> (the YAML as the body) and it appears here.
		</div>
	{:else if shown.length === 0}
		<div class="panel empty"><strong>Nothing matches "{search}"</strong>Clear the filter to see all {defs.data.length}.</div>
	{:else}
		<div class="cards">
			{#each shown as d (d.namespace + '/' + d.name)}
				<a class="card" href="/workflows/{d.namespace}/{d.name}">
					<div class="top">
						<div class="name">
							<span class="ns mono">{d.namespace}/</span><span class="nm mono">{d.name}</span>
						</div>
						{#if d.live_version}
							<span class="tag live" title="Published by {d.live_published_by ?? 'unknown'}">live v{d.live_version}</span>
						{:else}
							<span class="tag">no live version</span>
						{/if}
					</div>

					<div class="strip" aria-label="Steps">
						{#each d.steps as s, i (s.id)}
							{#if i > 0}<span class="join" aria-hidden="true"></span>{/if}
							<KindChip kind={s.kind} label={s.id} />
						{:else}
							<span class="muted">No steps to show{d.live_version ? '' : ': nothing is live'}.</span>
						{/each}
					</div>

					<dl class="stats">
						<div>
							<dt>Runs</dt>
							<dd>{compact(d.runs)}</dd>
						</div>
						<div class="t-info">
							<dt>Active</dt>
							<dd class:tinted={d.active > 0}>{compact(d.active)}</dd>
						</div>
						{#if d.blocked > 0}
							<div class="t-warn">
								<dt>Blocked</dt>
								<dd class="tinted">{compact(d.blocked)}</dd>
							</div>
						{/if}
						<div class="t-ok">
							<dt>Succeeded</dt>
							<dd>{compact(d.succeeded)}</dd>
						</div>
						<div class="t-fail">
							<dt>Failed</dt>
							<dd class:tinted={d.failed > 0}>{compact(d.failed)}</dd>
						</div>
						<div>
							<dt>Spend</dt>
							<dd>{money(d.spent_usd)}</dd>
						</div>
					</dl>

					<div class="foot">
						<span>Last run <Time at={d.last_run_at} /></span>
						{#if d.latest_version && d.latest_version !== d.live_version}
							<span title="A newer version exists than the live one">latest v{d.latest_version}</span>
						{/if}
						{#if d.live_published_at}
							<span>live since <Time at={d.live_published_at} /></span>
						{/if}
					</div>
				</a>
			{/each}
		</div>
	{/if}
</div>

<style>
	.legend {
		display: flex;
		gap: 6px;
		flex-wrap: wrap;
	}

	.cards {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(min(100%, 430px), 1fr));
		gap: 14px;
	}

	.card {
		display: flex;
		flex-direction: column;
		gap: 12px;
		padding: 15px 16px;
		background: var(--surface);
		border: 1px solid var(--line);
		border-radius: var(--radius);
		min-width: 0;
		transition:
			border-color 0.12s,
			transform 0.12s;
	}

	a.card:hover {
		border-color: var(--accent);
		transform: translateY(-1px);
	}

	.top {
		display: flex;
		justify-content: space-between;
		align-items: center;
		gap: 10px;
	}

	.name {
		min-width: 0;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	.ns {
		color: var(--muted);
		font-size: 13px;
	}

	.nm {
		font-size: 15px;
		font-weight: 700;
	}

	.strip {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 5px 0;
	}

	.join {
		width: 10px;
		height: 1.5px;
		background: var(--line-strong);
	}

	.stats {
		display: grid;
		grid-template-columns: repeat(5, minmax(0, 1fr));
		gap: 8px;
		margin: 0;
		padding: 10px 0 0;
		border-top: 1px solid var(--line);
	}

	.stats dt {
		font-size: 10.5px;
		font-weight: 700;
		letter-spacing: 0.08em;
		text-transform: uppercase;
		color: var(--muted);
	}

	.stats dd {
		margin: 0;
		font-size: 15px;
		font-weight: 680;
		font-variant-numeric: tabular-nums;
		white-space: nowrap;
	}

	.stats dd.tinted {
		color: var(--t);
	}

	.foot {
		display: flex;
		flex-wrap: wrap;
		gap: 4px 14px;
		color: var(--muted);
		font-size: 12px;
	}

	.empty code {
		padding: 1px 5px;
		background: var(--surface-2);
		border-radius: 4px;
	}
</style>
