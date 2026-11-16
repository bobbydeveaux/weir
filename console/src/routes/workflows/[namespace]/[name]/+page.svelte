<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { api } from '#lib/api.ts';
	import Diagram from '#lib/components/Diagram.svelte';
	import ErrorNote from '#lib/components/ErrorNote.svelte';
	import Json from '#lib/components/Json.svelte';
	import KindChip from '#lib/components/KindChip.svelte';
	import RunsTable from '#lib/components/RunsTable.svelte';
	import StartRunDialog from '#lib/components/StartRunDialog.svelte';
	import StepConfig from '#lib/components/StepConfig.svelte';
	import Time from '#lib/components/Time.svelte';
	import Yaml from '#lib/components/Yaml.svelte';
	import { int, isActive, money } from '#lib/format.ts';
	import { useQuery } from '#lib/query.svelte.ts';
	import { STEP_KINDS } from '#lib/types.ts';

	type Tab = 'diagram' | 'yaml' | 'versions' | 'runs';

	const namespace = $derived(page.params.namespace ?? '');
	const name = $derived(page.params.name ?? '');
	const wanted = $derived(Number(page.url.searchParams.get('version')) || 0);

	const def = useQuery(() => api.definition(namespace, name, wanted || undefined), { keep: true });
	const runs = useQuery(() => api.runs({ namespace, name, limit: 50 }), {
		keep: true,
		refresh: (list) => (list?.some((r) => isActive(r.status)) ? 2000 : 15000)
	});

	let tab = $state<Tab>('diagram');
	let selected = $state<string | null>(null);
	let starting = $state(false);
	let copied = $state(false);

	// Guard against showing the previous workflow while the next one loads.
	const d = $derived(def.data && def.data.namespace === namespace && def.data.name === name ? def.data : undefined);
	const steps = $derived(d?.spec?.steps ?? []);
	const step = $derived(steps.find((s) => s.id === selected) ?? null);
	const live = $derived(d?.aliases.find((a) => a.name === 'live')?.version ?? null);
	const versions = $derived(d?.versions ?? []);
	const kinds = $derived(STEP_KINDS.filter((k) => steps.some((s) => s.kind === k)));
	const aliasesOf = (version: number) => (d?.aliases ?? []).filter((a) => a.version === version).map((a) => a.name);

	function show(version: number | null) {
		const url = new URL(page.url.href);
		if (version && version !== live) url.searchParams.set('version', String(version));
		else url.searchParams.delete('version');
		void goto(url.pathname + url.search, { reset: false });
	}

	async function copy() {
		if (!d) return;
		try {
			await navigator.clipboard.writeText(d.source);
			copied = true;
			setTimeout(() => (copied = false), 1500);
		} catch {
			// Clipboard access was refused; the text can still be selected by hand.
		}
	}
</script>

<svelte:head><title>{name} · Workflows · Weir console</title></svelte:head>

<div class="page">
	<div class="page-head">
		<div>
			<div class="crumbs"><a href="/workflows">Workflows</a><span>/</span><span>{namespace}</span></div>
			<h1 class="title">
				<span class="mono">{name}</span>
				{#if d}
					<span class="tag" class:live={d.version === live}>v{d.version}{d.version === live ? ' · live' : ''}</span>
				{/if}
			</h1>
			{#if d}
				<p class="sub">
					Published by <b class="mono">{d.published_by}</b>
					<Time at={d.published_at} /> · {steps.length}
					{steps.length === 1 ? 'step' : 'steps'}
					{#if d.spec.budget?.usd}· run budget <b class="mono">{money(d.spec.budget.usd)}</b>{/if}
					{#if d.spec.budget?.minutes}· <b class="mono">{d.spec.budget.minutes} min</b>{/if}
				</p>
			{/if}
		</div>
		<button type="button" class="btn primary" disabled={!d || live === null} onclick={() => (starting = true)} title={live === null ? 'No live version to run' : ''}>
			<svg viewBox="0 0 16 16" width="13" height="13" fill="currentColor" aria-hidden="true"><path d="M4 2.5v11l9-5.5z" /></svg>
			Start run
		</button>
	</div>

	{#if def.error && !d}
		<ErrorNote error={def.error} what="this workflow" />
		{#if def.error.status === 404}
			<p class="muted">
				{#if wanted}
					Version {wanted} of {namespace}/{name} does not exist. <a class="link" href="/workflows/{namespace}/{name}">Show the live version</a>.
				{:else}
					Either the workflow does not exist in this workspace or it has no live version.
				{/if}
			</p>
		{/if}
	{:else if !d}
		<div class="panel panel-body"><div class="skeleton" style="height: 320px"></div></div>
	{:else}
		{#if live !== null && d.version !== live}
			<div class="notice">
				<span>You are looking at <b>v{d.version}</b>. New runs use the live version, <b>v{live}</b>.</span>
				<button type="button" class="btn small" onclick={() => show(null)}>Back to live</button>
			</div>
		{/if}

		<div class="tabs" role="tablist">
			<button type="button" role="tab" aria-selected={tab === 'diagram'} onclick={() => (tab = 'diagram')}>Diagram</button>
			<button type="button" role="tab" aria-selected={tab === 'yaml'} onclick={() => (tab = 'yaml')}>YAML source</button>
			<button type="button" role="tab" aria-selected={tab === 'versions'} onclick={() => (tab = 'versions')}>
				Versions<span class="count">{versions.length}</span>
			</button>
			<button type="button" role="tab" aria-selected={tab === 'runs'} onclick={() => (tab = 'runs')}>
				Runs{#if runs.data}<span class="count">{runs.data.length}{runs.data.length === 50 ? '+' : ''}</span>{/if}
			</button>
		</div>

		{#if tab === 'diagram'}
			<div class="split">
				<section class="panel canvas">
					<div class="panel-head">
						<h2>Steps, in order</h2>
						<div class="legend">
							{#each kinds as kind (kind)}<KindChip {kind} />{/each}
						</div>
					</div>
					<div class="panel-body">
						<Diagram {steps} {selected} onselect={(id) => (selected = selected === id ? null : id)} />
					</div>
				</section>

				<aside class="panel side">
					{#if step}
						<div class="panel-head">
							<div class="side-title">
								<KindChip kind={step.kind} />
								<h3 class="mono">{step.id}</h3>
							</div>
							<button type="button" class="btn small" onclick={() => (selected = null)}>Close</button>
						</div>
						<div class="panel-body">
							<StepConfig {step} {steps} />
						</div>
					{:else}
						<div class="panel-head"><h2>Workflow</h2></div>
						<div class="panel-body about">
							<p class="muted">Select a step in the diagram to read its configuration.</p>
							<dl class="kv">
								<dt>Namespace</dt>
								<dd class="mono">{d.namespace}</dd>
								<dt>Name</dt>
								<dd class="mono">{d.name}</dd>
								<dt>Version</dt>
								<dd class="mono">{d.version}</dd>
								<dt>Contract</dt>
								<dd class="mono">weir {d.contract_version}</dd>
								<dt>Spec hash</dt>
								<dd class="mono hash" title={d.spec_hash}>{d.spec_hash}</dd>
								<dt>Budget</dt>
								<dd>
									{#if d.spec.budget?.usd || d.spec.budget?.minutes}
										{#if d.spec.budget.usd}<span class="mono">{money(d.spec.budget.usd)}</span>{/if}
										{#if d.spec.budget.minutes}<span class="mono">{d.spec.budget.minutes} min</span>{/if}
										<span class="muted">per run</span>
									{:else}
										<span class="muted">none</span>
									{/if}
								</dd>
								<dt>Aliases</dt>
								<dd class="aliases">
									{#each d.aliases as a (a.name)}
										<span class="tag" class:live={a.name === 'live'} title="Moved by {a.updated_by}">{a.name} → v{a.version}</span>
									{:else}
										<span class="muted">none</span>
									{/each}
								</dd>
							</dl>
							{#if d.spec.input?.schema}
								<h2>Run input schema</h2>
								<Json value={d.spec.input.schema} max={260} />
							{/if}
							{#if d.spec.triggers !== undefined || d.spec.tiers !== undefined || d.spec.hooks !== undefined}
								<h2>Accepted, not yet enforced</h2>
								<Json value={{ triggers: d.spec.triggers, tiers: d.spec.tiers, hooks: d.spec.hooks }} max={220} />
							{/if}
						</div>
					{/if}
				</aside>
			</div>
		{:else if tab === 'yaml'}
			<section class="panel">
				<div class="panel-head">
					<h2>Source of v{d.version}, exactly as published</h2>
					<button type="button" class="btn small" onclick={copy}>{copied ? 'Copied' : 'Copy'}</button>
				</div>
				<Yaml code={d.source} />
			</section>
		{:else if tab === 'versions'}
			<section class="panel">
				<div class="table-wrap">
					<table class="table">
						<thead>
							<tr>
								<th>Version</th>
								<th>Published by</th>
								<th>Published</th>
								<th class="num">Runs</th>
								<th>Spec hash</th>
								<th></th>
							</tr>
						</thead>
						<tbody>
							{#each versions as v (v.version)}
								<tr class:current={v.version === d.version}>
									<td>
										<b class="mono">v{v.version}</b>
										{#each aliasesOf(v.version) as a (a)}
											<span class="tag" class:live={a === 'live'}>{a}</span>
										{/each}
									</td>
									<td class="mono">{v.published_by}</td>
									<td><Time at={v.published_at} /></td>
									<td class="num mono">{int(v.runs)}</td>
									<td class="mono muted" title={v.spec_hash}>{v.spec_hash.slice(0, 12)}</td>
									<td class="num">
										{#if v.version === d.version}
											<span class="muted">showing</span>
										{:else}
											<button type="button" class="btn small" onclick={() => show(v.version)}>View</button>
										{/if}
									</td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			</section>
		{:else}
			<section class="panel">
				{#if runs.error && !runs.data}
					<div class="panel-body"><ErrorNote error={runs.error} what="runs" /></div>
				{:else if !runs.data}
					<div class="panel-body"><div class="skeleton" style="height: 120px"></div></div>
				{:else if runs.data.length === 0}
					<div class="empty"><strong>This workflow has not run yet</strong>Use "Start run" to give it an input.</div>
				{:else}
					<RunsTable runs={runs.data} showWorkflow={false} />
				{/if}
			</section>
		{/if}
	{/if}
</div>

{#if d}
	<StartRunDialog bind:open={starting} {namespace} {name} aliases={d.aliases} />
{/if}

<style>
	.title {
		display: flex;
		align-items: center;
		gap: 10px;
		flex-wrap: wrap;
	}

	.title .mono {
		font-size: 21px;
	}

	.sub b {
		color: var(--text);
		font-weight: 650;
	}

	.notice {
		display: flex;
		justify-content: space-between;
		align-items: center;
		gap: 12px;
		padding: 9px 14px;
		border-radius: var(--radius);
		border: 1px solid color-mix(in srgb, var(--warn) 50%, var(--line));
		background: color-mix(in srgb, var(--warn) 9%, var(--surface));
	}

	.split {
		display: grid;
		grid-template-columns: minmax(0, 1fr) minmax(340px, 430px);
		gap: 16px;
		align-items: start;
	}

	.canvas .panel-body {
		background-image: radial-gradient(color-mix(in srgb, var(--line-strong) 55%, transparent) 1px, transparent 1px);
		background-size: 18px 18px;
		border-radius: 0 0 var(--radius) var(--radius);
		padding: 18px 10px;
	}

	.legend {
		display: flex;
		gap: 6px;
		flex-wrap: wrap;
		justify-content: flex-end;
	}

	.side {
		position: sticky;
		top: calc(var(--topbar) + 14px);
		max-height: calc(100vh - var(--topbar) - 28px);
		overflow-y: auto;
	}

	.side-title {
		display: flex;
		align-items: center;
		gap: 10px;
		min-width: 0;
	}

	.side-title h3 {
		font-size: 15px;
		overflow: hidden;
		text-overflow: ellipsis;
	}

	.about {
		display: flex;
		flex-direction: column;
		gap: 12px;
	}

	.hash {
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	.aliases {
		display: flex;
		gap: 5px;
		flex-wrap: wrap;
	}

	tr.current td {
		background: color-mix(in srgb, var(--accent) 7%, transparent);
	}

	td .tag {
		margin-left: 6px;
	}

	@media (max-width: 1080px) {
		.split {
			grid-template-columns: minmax(0, 1fr);
		}

		.side {
			position: static;
			max-height: none;
		}
	}
</style>
