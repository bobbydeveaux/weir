<script lang="ts">
	import { page } from '$app/state';
	import { api, toApiError } from '#lib/api.ts';
	import Diagram from '#lib/components/Diagram.svelte';
	import ErrorNote from '#lib/components/ErrorNote.svelte';
	import Json from '#lib/components/Json.svelte';
	import KindChip from '#lib/components/KindChip.svelte';
	import RunStep from '#lib/components/RunStep.svelte';
	import Status from '#lib/components/Status.svelte';
	import TaskCard from '#lib/components/TaskCard.svelte';
	import Time from '#lib/components/Time.svelte';
	import { between, clockTime, duration, isActive, isEmptyJson, money, shortId, tone } from '#lib/format.ts';
	import { edgeKey } from '#lib/graph.ts';
	import { useQuery } from '#lib/query.svelte.ts';
	import { useStream } from '#lib/stream.svelte.ts';
	import { clock, session, stale } from '#lib/session.svelte.ts';
	import type { RunFile, SpecStep, StepOverlay, StepView } from '#lib/types.ts';
	import { untrack } from 'svelte';

	// The stream brings news at once; this poll is only a safety net.
	const POLL_MS = 10000;
	const id = $derived(page.params.id ?? '');

	const run = useQuery(() => api.run(id), { refresh: (r) => (r && isActive(r.status) ? POLL_MS : 0) });
	const record = useQuery(() => api.runRecord(id), { refresh: () => (run.data && isActive(run.data.status) ? POLL_MS : 0) });
	const files = useQuery(() => api.runFiles(id), { refresh: () => (run.data && isActive(run.data.status) ? POLL_MS : 0) });
	let fileError = $state('');
	async function save(file: RunFile) {
		fileError = '';
		try {
			await api.downloadFile(id, file);
		} catch (e) {
			fileError = toApiError(e).message;
		}
	}
	function size(n: number): string {
		return n < 1024 ? `${n} B` : n < 1 << 20 ? `${(n / 1024).toFixed(1)} KB` : `${(n / (1 << 20)).toFixed(1)} MB`;
	}
	const tasks = useQuery(() => api.tasks(), { refresh: () => (run.data && isActive(run.data.status) ? POLL_MS : 0) });

	useStream(
		() => (id ? `/runs/${encodeURIComponent(id)}/stream` : null),
		() => {
			run.reload();
			record.reload();
			tasks.reload();
		}
	);

	// A string, so that the definition is fetched once and not on every poll.
	const workflow = $derived(record.data ? `${record.data.namespace}/${record.data.name}/${record.data.version}` : '');
	const def = useQuery(
		() => {
			if (!workflow) return null;
			const [namespace, name, version] = workflow.split('/');
			return api.definition(namespace, name, Number(version));
		},
		{ keep: true }
	);

	// When the status changes, fetch the rest once more: the last poll of the
	// record may have left just before the run finished.
	let seen = '';
	$effect(() => {
		const status = run.data?.status ?? '';
		const changed = seen !== '' && status !== '' && status !== seen;
		seen = status;
		if (changed) {
			untrack(() => {
				record.reload();
				tasks.reload();
				stale.tasks++;
			});
		}
	});

	const r = $derived(run.data);
	const rec = $derived(record.data);
	const active = $derived(!!r && isActive(r.status));

	const visits = $derived.by(() => {
		const map = new Map<string, StepView[]>();
		for (const s of r?.steps ?? []) {
			const list = map.get(s.key);
			if (list) list.push(s);
			else map.set(s.key, [s]);
		}
		for (const list of map.values()) list.sort((a, b) => a.visit - b.visit);
		return map;
	});

	// The definition's steps; failing that, the steps the run has visited.
	const steps = $derived.by<SpecStep[]>(() => {
		if (def.data?.spec?.steps?.length) return def.data.spec.steps;
		return [...visits.entries()].map(([key, list]) => ({ id: key, kind: list[0].kind }));
	});

	const overlay = $derived.by(() => {
		const out: Record<string, StepOverlay> = {};
		for (const [key, list] of visits) {
			const last = list[list.length - 1];
			out[key] = { status: last.status, visits: list.length, note: last.label || last.outcome || undefined };
		}
		return out;
	});

	const traversed = $derived.by(() => {
		const set = new Set<string>();
		const order = r?.steps ?? [];
		for (let i = 1; i < order.length; i++) set.add(edgeKey(order[i - 1].key, order[i].key));
		return set;
	});

	let picked = $state<string | null>(null);
	// Another run: follow that run, not the step chosen on the last one.
	$effect(() => {
		void id;
		picked = null;
		seen = '';
	});
	const current = $derived(r?.steps.at(-1)?.key ?? steps[0]?.id ?? null);
	const selected = $derived(picked ?? current);
	const selectedSpec = $derived(def.data?.spec?.steps?.find((s) => s.id === selected));
	const selectedKind = $derived(selectedSpec?.kind ?? visits.get(selected ?? '')?.[0]?.kind ?? 'unknown');

	const mine = $derived((tasks.data ?? []).filter((t) => t.run_id === id));

	// Timeline: one bar per visit, on the run's own clock.
	const t0 = $derived(rec ? new Date(rec.created_at).getTime() : 0);
	const t1 = $derived.by(() => {
		if (!rec) return 0;
		if (rec.finished_at) return new Date(rec.finished_at).getTime();
		if (active) return clock.now;
		return Math.max(t0, ...rec.step_times.map((s) => new Date(s.finished_at ?? s.started_at).getTime()));
	});
	const span = $derived(Math.max(1, t1 - t0));
	const bars = $derived(
		(rec?.step_times ?? []).map((s) => {
			const start = new Date(s.started_at).getTime();
			const end = s.finished_at ? new Date(s.finished_at).getTime() : t1;
			const view = visits.get(s.step)?.find((v) => v.visit === s.visit);
			return {
				...s,
				kind: view?.kind ?? steps.find((x) => x.id === s.step)?.kind ?? 'unknown',
				status: view?.status ?? 'unknown',
				left: Math.min(100, Math.max(0, ((start - t0) / span) * 100)),
				width: Math.max(0.6, Math.min(100, ((end - start) / span) * 100)),
				ms: Math.max(0, end - start),
				open: !s.finished_at
			};
		})
	);

	const eventAt = $derived(new Map((rec?.event_times ?? []).map((e) => [e.seq, e.at])));

	function eventTone(type: string): string {
		if (/succeeded$/.test(type)) return 'ok';
		if (/(failed|blocked|abandoned|expired)$/.test(type)) return 'fail';
		if (/(waiting|retrying|requeued|deferred|escalated|compensating)$/.test(type)) return 'warn';
		if (/(started|resumed|ready)$/.test(type)) return 'info';
		return 'idle';
	}

	// Resume.
	let budget = $state('');
	let resuming = $state(false);
	let resumeError = $state('');

	// Cancel: for a run that cannot be put right.
	let cancelling = $state(false);

	async function cancelRun() {
		const reason = prompt('Cancel this run? It cannot be resumed afterwards. Why is it being cancelled?');
		if (reason === null) return;
		cancelling = true;
		resumeError = '';
		try {
			await api.cancelRun(id, reason.trim());
			run.reload();
			record.reload();
		} catch (e) {
			resumeError = toApiError(e).message;
		} finally {
			cancelling = false;
		}
	}

	async function resume() {
		resuming = true;
		resumeError = '';
		try {
			await api.resumeRun(id, Number(budget) || undefined);
			budget = '';
			run.reload();
			record.reload();
		} catch (e) {
			resumeError = toApiError(e).message;
		} finally {
			resuming = false;
		}
	}

	function decided() {
		stale.tasks++;
		run.reload();
		tasks.reload();
	}
</script>

<svelte:head><title>Run {shortId(id)} · Weir console</title></svelte:head>

<div class="page">
	<div class="page-head">
		<div>
			<div class="crumbs"><a href="/runs">Runs</a><span>/</span><span class="mono">{id}</span></div>
			<h1 class="title">
				<span>Run <span class="mono">{shortId(id)}</span></span>
				{#if r}<Status status={r.status} />{/if}
				{#if active}<span class="livetag"><i></i>live</span>{/if}
			</h1>
			<p class="sub">
				{#if rec}
					<a class="link mono" href="/workflows/{rec.namespace}/{rec.name}?version={rec.version}">{rec.namespace}/{rec.name}</a>
					<span class="tag">v{rec.version}</span>
					· requested by <b class="mono">{rec.requester}</b> · started <Time at={rec.created_at} /> · took
					<b class="mono">{duration(between(rec.created_at, rec.finished_at, clock.now))}</b>
					·
				{/if}
				{#if r}
					spent <b class="mono">{money(r.spent_usd)}</b>{#if rec?.budget_usd}<span> of a {money(rec.budget_usd)} budget</span>{/if}
				{/if}
			</p>
		</div>
	</div>

	{#if run.error && !r}
		<ErrorNote error={run.error} what="this run" />
		{#if run.error.status === 404 || run.error.status === 400}
			<p class="muted">Runs belong to one workspace. This one is not in <b class="mono">{session.workspace}</b>: try another workspace, or go back to <a class="link" href="/runs">all runs</a>.</p>
		{/if}
	{:else if !r}
		<div class="panel panel-body"><div class="skeleton" style="height: 360px"></div></div>
	{:else}
		{#if r.status === 'blocked'}
			<div class="callout t-warn">
				<div>
					<strong>This run is blocked.</strong>
					A step failed and the run is held where it was. Resuming tries again from that step and keeps every result so far.
					If it cannot be put right, cancel it: it then counts as failed.
					{#if resumeError}<p class="form-error" role="alert">{resumeError}</p>{/if}
				</div>
				<div class="row">
					<label class="field">
						<span>New budget, USD (optional)</span>
						<input class="input mono" type="number" min="0" step="0.01" placeholder={rec?.budget_usd ? String(rec.budget_usd) : 'unchanged'} bind:value={budget} />
					</label>
					<button type="button" class="btn primary" disabled={resuming} onclick={resume}>{resuming ? 'Resuming…' : 'Resume run'}</button>
					<button type="button" class="btn" disabled={cancelling} onclick={cancelRun}>{cancelling ? 'Cancelling…' : 'Cancel run'}</button>
				</div>
			</div>
		{/if}

		{#if r.error !== undefined && r.error !== null}
			<div class="callout t-fail">
				<div class="grow">
					<strong>The run reported an error.</strong>
					<Json value={r.error} text={typeof r.error === 'string'} max={180} />
				</div>
			</div>
		{/if}

		{#each mine as task (task.id)}
			<section class="waiting">
				<h2>This run is waiting for a person</h2>
				<TaskCard {task} inline ondecided={decided} />
			</section>
		{/each}

		{#if record.error && !rec}
			<ErrorNote error={record.error} what="the run's record (model calls, tool calls and timings)" />
		{/if}

		<div class="split">
			<section class="panel canvas">
				<div class="panel-head">
					<h2>Steps</h2>
					<span class="muted small">
						{#if def.error && !def.data}Definition unavailable: showing the steps visited so far{:else}Lit edges are the path this run took{/if}
					</span>
				</div>
				<div class="panel-body">
					<Diagram {steps} {selected} {overlay} {traversed} onselect={(key) => (picked = key)} />
				</div>
			</section>

			<aside class="panel side">
				{#if selected}
					<div class="panel-head">
						<div class="side-title">
							<KindChip kind={selectedKind} />
							<h3 class="mono">{selected}</h3>
						</div>
						{#if picked && picked !== current}
							<button type="button" class="btn small" onclick={() => (picked = null)}>Follow run</button>
						{/if}
					</div>
					<div class="panel-body">
						{#key selected}
							<RunStep
								stepKey={selected}
								visits={visits.get(selected) ?? []}
								spec={selectedSpec}
								steps={def.data?.spec?.steps ?? []}
								times={(rec?.step_times ?? []).filter((t) => t.step === selected)}
								aiCalls={(rec?.ai_calls ?? []).filter((c) => c.step === selected)}
								toolCalls={(rec?.tool_calls ?? []).filter((c) => c.step === selected)}
								decisions={rec?.decisions ?? []}
								hasRecord={!!rec}
							/>
						{/key}
					</div>
				{:else}
					<div class="empty"><strong>No steps yet</strong>The run has not started its first step.</div>
				{/if}
			</aside>
		</div>

		<section class="panel">
			<div class="panel-head">
				<h2>Timeline</h2>
				{#if rec}<span class="muted small mono">{duration(span)} {active ? 'so far' : 'in all'}</span>{/if}
			</div>
			{#if !rec}
				<div class="panel-body">
					{#if record.error}<p class="muted">Timings come from the run's record, which could not be loaded.</p>{:else}<div class="skeleton" style="height: 70px"></div>{/if}
				</div>
			{:else if bars.length === 0}
				<div class="empty"><strong>No steps have started</strong></div>
			{:else}
				<ol class="timeline">
					{#each bars as b (b.step + '#' + b.visit)}
						<li>
							<button type="button" class="tl-row k-{b.kind}" class:on={selected === b.step} onclick={() => (picked = b.step)}>
								<span class="tl-name mono">
									{b.step}{#if b.visit > 1}<span class="muted"> ·{b.visit}</span>{/if}
								</span>
								<span class="tl-track">
									<span
										class="tl-bar t-{tone(b.status)}"
										class:open={b.open}
										style:left="{b.left}%"
										style:width="{b.width}%"
										title="{b.step}: {b.status}, started {clockTime(b.started_at)}"
									></span>
								</span>
								<span class="tl-dur mono">{duration(b.ms)}</span>
								<span class="tl-cost mono muted">{b.spent_usd ? money(b.spent_usd) : ''}</span>
							</button>
						</li>
					{/each}
				</ol>
			{/if}
		</section>

		<div class="io">
			<section class="panel">
				<div class="panel-head"><h2>Run input</h2></div>
				<div class="panel-body">
					{#if isEmptyJson(r.input)}<p class="muted">Empty.</p>{:else}<Json value={r.input} max={320} />{/if}
				</div>
			</section>
			<section class="panel">
				<div class="panel-head"><h2>Run output</h2></div>
				<div class="panel-body">
					{#if r.output !== undefined && r.output !== null}
						<Json value={r.output} max={320} />
					{:else}
						<p class="muted">{active ? 'Not yet: the run is still going.' : 'None.'}</p>
					{/if}
				</div>
			</section>
		</div>

		{#if files.data && files.data.length > 0}
			<section class="panel">
				<div class="panel-head">
					<h2>Files</h2>
					<span class="muted">{files.data.length} written, changed or deleted by this run's steps</span>
				</div>
				<div class="panel-body">
					{#if fileError}<p class="muted" role="alert">{fileError}</p>{/if}
					<table class="table">
						<thead>
							<tr><th>Step</th><th>Path</th><th>Type</th><th class="num">Size</th><th></th></tr>
						</thead>
						<tbody>
							{#each files.data as f (f.step + '/' + f.path)}
								<tr>
									<td class="mono">{f.step}</td>
									<td class="mono">{f.path}</td>
									<td class="muted">{f.deleted ? '' : f.content_type.split(';')[0]}</td>
									<td class="num">{f.deleted ? '' : size(f.size)}</td>
									<td class="num">
										{#if f.deleted}<span class="muted">Deleted</span>{:else}<button type="button" class="link plain" onclick={() => save(f)}>Download</button>{/if}
									</td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			</section>
		{/if}

		<section class="panel">
			<div class="panel-head">
				<h2>Event log</h2>
				<span class="muted small">{r.events.length} {r.events.length === 1 ? 'event' : 'events'}</span>
			</div>
			{#if r.events.length === 0}
				<div class="empty"><strong>No events</strong></div>
			{:else}
				<div class="table-wrap">
					<table class="table events">
						<thead>
							<tr>
								<th class="num">#</th>
								<th>Time</th>
								<th class="num">+ start</th>
								<th>Event</th>
								<th>Step</th>
								<th>Actor</th>
								<th>Data</th>
							</tr>
						</thead>
						<tbody>
							{#each r.events as e (e.seq)}
								{@const at = eventAt.get(e.seq)}
								<tr>
									<td class="num mono muted">{e.seq}</td>
									<td class="mono" title={at ?? ''}>{clockTime(at)}</td>
									<td class="num mono muted">{at && t0 ? '+' + duration(new Date(at).getTime() - t0) : ''}</td>
									<td><span class="etype t-{eventTone(e.type)} mono">{e.type}</span></td>
									<td class="mono">
										{#if e.step}
											<button type="button" class="steplink" onclick={() => (picked = e.step ?? null)}>{e.step}</button>
										{/if}
									</td>
									<td class="mono">{e.actor}</td>
									<td class="mono data">{isEmptyJson(e.data) ? '' : JSON.stringify(e.data)}</td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			{/if}
		</section>

		{#if run.error}
			<ErrorNote error={run.error} what="the latest state of this run" />
		{/if}
	{/if}
</div>

<style>
	.plain {
		background: none;
		border: 0;
		padding: 0;
		font: inherit;
		cursor: pointer;
	}

	.title {
		display: flex;
		align-items: center;
		gap: 12px;
		flex-wrap: wrap;
	}

	.sub b {
		color: var(--text);
		font-weight: 650;
	}

	.small {
		font-size: 12px;
	}

	.livetag {
		display: inline-flex;
		align-items: center;
		gap: 6px;
		font-size: 12px;
		font-weight: 650;
		color: var(--accent);
	}

	.livetag i {
		width: 7px;
		height: 7px;
		border-radius: 50%;
		background: currentColor;
		animation: pulse 1.1s ease-in-out infinite;
	}

	.callout {
		display: flex;
		justify-content: space-between;
		align-items: flex-end;
		gap: 16px;
		flex-wrap: wrap;
		padding: 12px 14px;
		border-radius: var(--radius);
		border: 1px solid color-mix(in srgb, var(--t) 50%, var(--line));
		background: color-mix(in srgb, var(--t) 8%, var(--surface));
	}

	.callout > div:first-child {
		flex: 1 1 320px;
		min-width: 0;
	}

	.callout strong {
		color: var(--t);
	}

	.callout .grow {
		display: flex;
		flex-direction: column;
		gap: 8px;
	}

	.callout .row {
		align-items: flex-end;
	}

	.waiting {
		display: flex;
		flex-direction: column;
		gap: 8px;
	}

	.split {
		display: grid;
		grid-template-columns: minmax(0, 1fr) minmax(360px, 470px);
		gap: 16px;
		align-items: start;
	}

	.canvas .panel-body {
		background-image: radial-gradient(color-mix(in srgb, var(--line-strong) 55%, transparent) 1px, transparent 1px);
		background-size: 18px 18px;
		border-radius: 0 0 var(--radius) var(--radius);
		padding: 18px 10px;
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

	.timeline {
		list-style: none;
		margin: 0;
		padding: 8px 6px;
	}

	.tl-row {
		display: grid;
		grid-template-columns: minmax(90px, 180px) minmax(0, 1fr) 78px 76px;
		align-items: center;
		gap: 12px;
		width: 100%;
		padding: 5px 8px;
		border: 0;
		border-radius: 6px;
		background: none;
		text-align: left;
	}

	.tl-row:hover,
	.tl-row.on {
		background: var(--surface-2);
	}

	.tl-name {
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
		font-weight: 650;
		padding-left: 9px;
		border-left: 3px solid var(--k, var(--line-strong));
	}

	.tl-track {
		position: relative;
		height: 10px;
		border-radius: 5px;
		background: var(--surface-3);
	}

	.tl-bar {
		position: absolute;
		top: 0;
		bottom: 0;
		min-width: 4px;
		border-radius: 5px;
		background: var(--t);
	}

	.tl-bar.open {
		animation: pulse 1.3s ease-in-out infinite;
	}

	.tl-dur,
	.tl-cost {
		text-align: right;
		white-space: nowrap;
	}

	.io {
		display: grid;
		grid-template-columns: repeat(2, minmax(0, 1fr));
		gap: 16px;
	}

	.events td {
		padding-top: 5px;
		padding-bottom: 5px;
		vertical-align: top;
	}

	.etype {
		color: var(--t);
		font-weight: 650;
		white-space: nowrap;
	}

	.etype.t-idle {
		color: var(--text);
	}

	.steplink {
		border: 0;
		background: none;
		padding: 0;
		font: inherit;
		color: var(--accent);
	}

	.steplink:hover {
		text-decoration: underline;
	}

	.data {
		color: var(--muted);
		font-size: 12px;
		overflow-wrap: anywhere;
		min-width: 200px;
	}

	@media (max-width: 1080px) {
		.split,
		.io {
			grid-template-columns: minmax(0, 1fr);
		}

		.side {
			position: static;
			max-height: none;
		}
	}

	@media (max-width: 640px) {
		.tl-row {
			grid-template-columns: minmax(70px, 110px) minmax(0, 1fr) 64px;
		}

		.tl-cost {
			display: none;
		}
	}
</style>
