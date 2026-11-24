<script lang="ts">
	import { api } from '#lib/api.ts';
	import ErrorNote from '#lib/components/ErrorNote.svelte';
	import Json from '#lib/components/Json.svelte';
	import Time from '#lib/components/Time.svelte';
	import { shortId } from '#lib/format.ts';
	import { useQuery } from '#lib/query.svelte.ts';

	const triggers = useQuery(() => api.triggers(), { keep: true, refresh: () => 15000 });
	const events = useQuery(() => api.events(100), { keep: true, refresh: () => 3000 });

	// Triggers grouped by the event that sets them off.
	const schedules = $derived((triggers.data ?? []).filter((t) => t.schedule));

	const byEvent = $derived.by(() => {
		const groups = new Map<string, NonNullable<typeof triggers.data>>();
		for (const t of triggers.data ?? []) {
			if (!t.event) continue; // schedules are listed in their own table
			groups.set(t.event, [...(groups.get(t.event) ?? []), t]);
		}
		return [...groups.entries()];
	});
</script>

<svelte:head><title>Events · Weir console</title></svelte:head>

<div class="page">
	<div class="page-head">
		<div>
			<h1>Events</h1>
			<p class="sub">
				An application reports what happened with <code class="mono">POST /v1/events</code>. Weir starts a run of every workflow
				whose trigger matches. The application never names a workflow.
			</p>
		</div>
	</div>

	<section class="panel">
		<div class="panel-head">
			<h2>Triggers <span class="n">{(triggers.data ?? []).filter((t) => t.event).length}</span></h2>
			<span class="muted small">Which events start which workflows, from each workflow's live version</span>
		</div>
		{#if triggers.error && !triggers.data}
			<div class="panel-body"><ErrorNote error={triggers.error} what="triggers" /></div>
		{:else if !triggers.data}
			<div class="panel-body"><div class="skeleton" style="height: 50px"></div></div>
		{:else if byEvent.length === 0}
			<div class="empty">
				<strong>No workflow starts on an event</strong>
				Add a <code class="mono">triggers:</code> block to a workflow, with the <code class="mono">event</code> it should start on.
			</div>
		{:else}
			<div class="table-wrap">
				<table class="table">
					<thead>
						<tr><th>When this happens</th><th>This starts</th><th>Only if</th></tr>
					</thead>
					<tbody>
						{#each byEvent as [event, rows] (event)}
							{#each rows as t, i (t.namespace + '/' + t.name + i)}
								<tr>
									<td class="mono">{#if i === 0}<b>{event}</b>{/if}</td>
									<td class="mono">
										<a href="/workflows/{t.namespace}/{t.name}">{t.namespace}/<b>{t.name}</b></a>
										<span class="tag">v{t.version}</span>
									</td>
									<td class="mono">{t.when ?? 'always'}</td>
								</tr>
							{/each}
						{/each}
					</tbody>
				</table>
			</div>
		{/if}
	</section>

	{#if schedules.length > 0}
		<section class="panel">
			<div class="panel-head">
				<h2>Schedules <span class="n">{schedules.length}</span></h2>
				<span class="muted small">Workflows that start by themselves. Times are in UTC</span>
			</div>
			<div class="table-wrap">
				<table class="table">
					<thead>
						<tr><th>Schedule</th><th>This starts</th><th>Next due</th><th>Last ran</th></tr>
					</thead>
					<tbody>
						{#each schedules as t (t.namespace + '/' + t.name + t.schedule)}
							<tr>
								<td class="mono"><b>{t.schedule}</b></td>
								<td class="mono">
									<a href="/workflows/{t.namespace}/{t.name}">{t.namespace}/<b>{t.name}</b></a>
									<span class="tag">v{t.version}</span>
								</td>
								<td>{#if t.next_at}<Time at={t.next_at} />{:else}<span class="muted">not set</span>{/if}</td>
								<td>
									{#if t.last_at}<Time at={t.last_at} />{:else}<span class="muted">never</span>{/if}
									{#if t.last_error}<div class="tag write" title={t.last_error}>could not start</div>{/if}
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
			<h2>Received <span class="n">{events.data?.length ?? 0}</span></h2>
			<span class="muted small">Newest first</span>
		</div>
		{#if events.error && !events.data}
			<div class="panel-body"><ErrorNote error={events.error} what="events" /></div>
		{:else if !events.data}
			<div class="panel-body"><div class="skeleton" style="height: 80px"></div></div>
		{:else if events.data.length === 0}
			<div class="empty"><strong>No events yet</strong>Nothing has been reported to this workspace.</div>
		{:else}
			<div class="table-wrap">
				<table class="table">
					<thead>
						<tr><th>Event</th><th>From</th><th>Received</th><th>Started or resumed</th><th>Data</th></tr>
					</thead>
					<tbody>
						{#each events.data as e (e.id)}
							<tr>
								<td class="mono"><b>{e.type}</b>{#if e.key}<div class="muted small">{e.key}</div>{/if}</td>
								<td>{e.sent_by}{#if e.source}<div class="muted small mono">{e.source}</div>{/if}</td>
								<td><Time at={e.received_at} /></td>
								<td>
									{#each e.runs ?? [] as r (r.namespace + '/' + r.name)}
										<div class="mono">
											{#if r.run_id}
												<a href="/runs/{r.run_id}">{r.namespace}/{r.name} · {shortId(r.run_id)}</a>
											{:else}
												<span class="tag write" title={r.error}>{r.namespace}/{r.name} could not start</span>
											{/if}
										</div>
									{/each}
									{#each e.resumed ?? [] as r (r.run_id + '/' + r.step)}
										<div class="mono">
											{#if r.error}
												<span class="tag write" title={r.error}>{r.step} could not be checked</span>
											{:else}
												<a href="/runs/{r.run_id}">resumed {r.step} · {shortId(r.run_id)}</a>
											{/if}
										</div>
									{/each}
									{#if !e.runs?.length && !e.resumed?.length}
										<span class="muted">nothing: no trigger matched and no step was waiting</span>
									{/if}
								</td>
								<td class="data"><Json value={e.data} max={90} /></td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		{/if}
	</section>
</div>

<style>
	.page-head code {
		font-size: 12px;
	}

	td.data {
		max-width: 380px;
	}

	td a {
		color: inherit;
	}
</style>
