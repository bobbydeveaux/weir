<script lang="ts">
	import { between, duration, money, shortId } from '#lib/format.ts';
	import { clock } from '#lib/session.svelte.ts';
	import type { RunSummary } from '#lib/types.ts';
	import Status from './Status.svelte';
	import Time from './Time.svelte';

	let { runs, showWorkflow = true }: { runs: RunSummary[]; showWorkflow?: boolean } = $props();
</script>

<div class="table-wrap">
	<table class="table">
		<thead>
			<tr>
				<th>Run</th>
				{#if showWorkflow}<th>Workflow</th>{/if}
				<th>Status</th>
				<th>Step</th>
				<th>Progress</th>
				<th>Requester</th>
				<th class="num">Spend</th>
				<th>Started</th>
				<th class="num">Duration</th>
			</tr>
		</thead>
		<tbody>
			{#each runs as r (r.id)}
				{@const pct = r.steps_total ? Math.min(100, (r.steps_done / r.steps_total) * 100) : 0}
				<tr>
					<td><a class="link mono" href="/runs/{r.id}" title={r.id}>{shortId(r.id)}</a></td>
					{#if showWorkflow}
						<td>
							<a class="wf mono" href="/workflows/{r.namespace}/{r.name}">
								<span class="muted">{r.namespace}/</span>{r.name}
							</a>
							<span class="tag">v{r.version}</span>
						</td>
					{/if}
					<td><Status status={r.status} /></td>
					<td class="mono">
						{r.current_step ?? '—'}
						{#if !showWorkflow}<span class="tag">v{r.version}</span>{/if}
					</td>
					<td>
						<span class="progress" title="{r.steps_done} of {r.steps_total} steps succeeded">
							<span class="track"><span class="fill" style:width="{pct}%"></span></span>
							<span class="mono muted">{r.steps_done}/{r.steps_total}</span>
						</span>
					</td>
					<td class="mono">{r.requester}</td>
					<td class="num mono">
						{money(r.spent_usd)}
						{#if r.budget_usd}<span class="faint" title="Budget"> / {money(r.budget_usd)}</span>{/if}
					</td>
					<td><Time at={r.created_at} /></td>
					<td class="num mono">{duration(between(r.created_at, r.finished_at, clock.now))}</td>
				</tr>
			{/each}
		</tbody>
	</table>
</div>

<style>
	.wf:hover {
		text-decoration: underline;
	}

	.tag {
		margin-left: 6px;
	}

	.progress {
		display: inline-flex;
		align-items: center;
		gap: 8px;
		white-space: nowrap;
	}

	.track {
		width: 64px;
		height: 5px;
		border-radius: 3px;
		background: var(--surface-3);
		overflow: hidden;
	}

	.fill {
		display: block;
		height: 100%;
		border-radius: 3px;
		background: var(--accent);
	}
</style>
