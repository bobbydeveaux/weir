<script lang="ts">
	import { api } from '#lib/api.ts';
	import ErrorNote from '#lib/components/ErrorNote.svelte';
	import SpendChart from '#lib/components/SpendChart.svelte';
	import TaskCard from '#lib/components/TaskCard.svelte';
	import { compact, duration, int, money } from '#lib/format.ts';
	import { useQuery } from '#lib/query.svelte.ts';
	import { session, stale } from '#lib/session.svelte.ts';

	const RANGES = [7, 30, 90];
	let days = $state(30);

	const usage = useQuery(() => api.usage(days), { keep: true, refresh: () => 15000 });
	const tasks = useQuery(
		() => {
			void stale.tasks;
			return api.tasks();
		},
		{ keep: true, refresh: () => 10000 }
	);

	const u = $derived(usage.data);
	const finished = $derived(u ? u.totals.succeeded + u.totals.failed : 0);
	const successRate = $derived(u && finished > 0 ? Math.round((u.totals.succeeded / finished) * 100) : null);
	const topSpend = $derived(Math.max(...(u?.by_workflow ?? []).map((w) => w.spent_usd), 0));

	function decided() {
		stale.tasks++;
	}
</script>

<svelte:head><title>Overview · Weir console</title></svelte:head>

<div class="page">
	<div class="page-head">
		<div>
			<h1>Overview</h1>
			<p class="sub">What has run and what it cost in workspace <b class="mono">{session.workspace}</b>.</p>
		</div>
		<div class="seg" role="group" aria-label="Time range">
			{#each RANGES as r (r)}
				<button type="button" aria-pressed={days === r} onclick={() => (days = r)}>{r} days</button>
			{/each}
		</div>
	</div>

	{#if usage.error && !u}
		<ErrorNote error={usage.error} what="usage" />
	{:else}
		<section class="tiles" aria-busy={usage.loading}>
			{#if u}
				<div class="tile">
					<span class="k">Runs</span>
					<span class="v">{compact(u.totals.runs)}</span>
					<span class="s">last {u.days} days</span>
				</div>
				<div class="tile t-info">
					<span class="k">Active</span>
					<span class="v" class:tinted={u.totals.active > 0}>{compact(u.totals.active)}</span>
					<span class="s">running or waiting</span>
				</div>
				{#if u.totals.blocked > 0}
					<div class="tile t-warn">
						<span class="k">Blocked</span>
						<span class="v tinted">{compact(u.totals.blocked)}</span>
						<span class="s">a step failed; resume or cancel</span>
					</div>
				{/if}
				<div class="tile t-ok">
					<span class="k">Succeeded</span>
					<span class="v">{compact(u.totals.succeeded)}</span>
					<span class="s">{successRate === null ? 'none finished yet' : `${successRate}% of finished runs`}</span>
				</div>
				<div class="tile t-fail">
					<span class="k">Failed</span>
					<span class="v" class:tinted={u.totals.failed > 0}>{compact(u.totals.failed)}</span>
					<span class="s">failed or cancelled</span>
				</div>
				<div class="tile">
					<span class="k">Spend</span>
					<span class="v">{money(u.totals.spent_usd)}</span>
					<span class="s">{u.totals.runs ? `${money(u.totals.spent_usd / u.totals.runs)} per run` : 'no runs'}</span>
				</div>
				<div class="tile">
					<span class="k">Model calls</span>
					<span class="v">{compact(u.ai.calls)}</span>
					<span class="s" title="{int(u.ai.tokens_in)} tokens in, {int(u.ai.tokens_out)} tokens out">
						{compact(u.ai.tokens_in)} in · {compact(u.ai.tokens_out)} out
					</span>
				</div>
			{:else}
				{#each Array(6) as _, i (i)}
					<div class="tile"><span class="skeleton" style="width: 50%"></span><span class="skeleton" style="height: 28px; width: 70%"></span></div>
				{/each}
			{/if}
		</section>

		<div class="cols">
			<section class="panel">
				<div class="panel-head"><h2>Spend per day</h2></div>
				<div class="panel-body">
					{#if u}
						<SpendChart byDay={u.by_day} days={u.days} />
					{:else}
						<div class="skeleton" style="height: 170px"></div>
					{/if}
				</div>
			</section>

			<section class="panel">
				<div class="panel-head">
					<h2>Open human tasks</h2>
					{#if tasks.data?.length}<a class="link" href="/tasks">All {tasks.data.length}</a>{/if}
				</div>
				<div class="panel-body tasks">
					{#if tasks.error && !tasks.data}
						<ErrorNote error={tasks.error} what="tasks" />
					{:else if !tasks.data}
						<div class="skeleton" style="height: 90px"></div>
					{:else if tasks.data.length === 0}
						<div class="empty"><strong>Nobody is being waited on</strong>Human steps that need a decision appear here.</div>
					{:else}
						{#each tasks.data.slice(0, 3) as task (task.id)}
							<TaskCard {task} ondecided={decided} />
						{/each}
						{#if tasks.data.length > 3}
							<a class="link more" href="/tasks">{tasks.data.length - 3} more waiting</a>
						{/if}
					{/if}
				</div>
			</section>
		</div>

		<section class="panel">
			<div class="panel-head"><h2>By workflow</h2></div>
			{#if !u}
				<div class="panel-body"><div class="skeleton" style="height: 80px"></div></div>
			{:else if u.by_workflow.length === 0}
				<div class="empty"><strong>No runs in the last {u.days} days</strong>Start one from a workflow's page.</div>
			{:else}
				<div class="table-wrap">
					<table class="table">
						<thead>
							<tr>
								<th>Workflow</th>
								<th class="num">Runs</th>
								<th class="num">Succeeded</th>
								<th class="num">Failed</th>
								<th class="num">Spend</th>
								<th class="share">Share of spend</th>
							</tr>
						</thead>
						<tbody>
							{#each u.by_workflow as w (w.namespace + '/' + w.name)}
								<tr>
									<td>
										<a class="mono wf" href="/workflows/{w.namespace}/{w.name}"><span class="muted">{w.namespace}/</span>{w.name}</a>
									</td>
									<td class="num mono">{int(w.runs)}</td>
									<td class="num mono">{int(w.succeeded)}</td>
									<td class="num mono" class:bad={w.failed > 0}>{int(w.failed)}</td>
									<td class="num mono">{money(w.spent_usd)}</td>
									<td class="share">
										<span class="meter"><span style:width="{topSpend ? (w.spent_usd / topSpend) * 100 : 0}%"></span></span>
									</td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			{/if}
		</section>

		<section class="panel">
			<div class="panel-head"><h2>By model</h2></div>
			{#if !u}
				<div class="panel-body"><div class="skeleton" style="height: 80px"></div></div>
			{:else if u.by_model.length === 0}
				<div class="empty"><strong>No model calls in the last {u.days} days</strong>Decide and ai steps record every call they make.</div>
			{:else}
				<div class="table-wrap">
					<table class="table">
						<thead>
							<tr>
								<th>Provider</th>
								<th>Model</th>
								<th class="num">Calls</th>
								<th class="num">Tokens in</th>
								<th class="num">Tokens out</th>
								<th class="num">Cost</th>
								<th class="num">Avg time</th>
								<th class="num">Errors</th>
							</tr>
						</thead>
						<tbody>
							{#each u.by_model as m (`${m.provider}/${m.model}`)}
								<tr>
									<td class="mono">{m.provider ?? '—'}</td>
									<td class="mono">{m.model ?? '—'}</td>
									<td class="num mono">{int(m.calls)}</td>
									<td class="num mono" title={int(m.tokens_in)}>{compact(m.tokens_in)}</td>
									<td class="num mono" title={int(m.tokens_out)}>{compact(m.tokens_out)}</td>
									<td class="num mono">
										{money(m.cost_usd)}{#if m.cost_estimated}<span class="est" title="Includes estimated costs: the provider did not report an exact figure">est.</span>{/if}
									</td>
									<td class="num mono">{duration(m.avg_ms)}</td>
									<td class="num mono" class:bad={m.errors > 0}>{int(m.errors)}</td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			{/if}
		</section>
	{/if}
</div>

<style>
	.tiles {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(170px, 1fr));
		gap: 12px;
	}

	.tile {
		display: flex;
		flex-direction: column;
		gap: 3px;
		padding: 13px 14px;
		background: var(--surface);
		border: 1px solid var(--line);
		border-radius: var(--radius);
		min-height: 92px;
	}

	.tile .k {
		font-size: 11px;
		font-weight: 700;
		letter-spacing: 0.09em;
		text-transform: uppercase;
		color: var(--muted);
	}

	.tile .v {
		font-size: 25px;
		font-weight: 720;
		letter-spacing: -0.02em;
		line-height: 1.2;
		font-variant-numeric: tabular-nums;
	}

	.tile .v.tinted {
		color: var(--t);
	}

	.tile .s {
		font-size: 12px;
		color: var(--muted);
	}

	.cols {
		display: grid;
		grid-template-columns: minmax(0, 1.25fr) minmax(0, 1fr);
		gap: 16px;
		align-items: start;
	}

	.tasks {
		display: flex;
		flex-direction: column;
		gap: 10px;
	}

	.more {
		align-self: center;
	}

	.wf:hover {
		text-decoration: underline;
	}

	.bad {
		color: var(--fail);
		font-weight: 650;
	}

	.est {
		margin-left: 5px;
		font-size: 10.5px;
		color: var(--warn);
	}

	.share {
		width: 22%;
		min-width: 110px;
	}

	.meter {
		display: block;
		height: 6px;
		border-radius: 3px;
		background: var(--surface-3);
		overflow: hidden;
	}

	.meter span {
		display: block;
		height: 100%;
		border-radius: 3px;
		background: var(--accent);
	}

	@media (max-width: 1100px) {
		.cols {
			grid-template-columns: minmax(0, 1fr);
		}
	}
</style>
