<script lang="ts">
	import { api } from '#lib/api.ts';
	import ErrorNote from '#lib/components/ErrorNote.svelte';
	import TaskCard from '#lib/components/TaskCard.svelte';
	import { useQuery } from '#lib/query.svelte.ts';
	import { session, stale } from '#lib/session.svelte.ts';

	const tasks = useQuery(
		() => {
			void stale.tasks;
			return api.tasks();
		},
		{ keep: true, refresh: () => 5000 }
	);

	let role = $state('');
	const roles = $derived([...new Set((tasks.data ?? []).map((t) => t.role).filter((r): r is string => !!r))].sort());
	const shown = $derived((tasks.data ?? []).filter((t) => !role || t.role === role));
</script>

<svelte:head><title>Tasks · Weir console</title></svelte:head>

<div class="page">
	<div class="page-head">
		<div>
			<h1>Tasks</h1>
			<p class="sub">
				Human steps waiting for a decision, oldest first. The engine checks who may decide; you are acting as
				{#if session.actor}<b class="mono">{session.actor}</b>{:else}<span class="warn">nobody yet (set "Acting as" above)</span>{/if}.
			</p>
		</div>
		{#if roles.length > 1}
			<label class="field">
				<span>Role</span>
				<select class="select" bind:value={role}>
					<option value="">All roles</option>
					{#each roles as r (r)}<option value={r}>{r}</option>{/each}
				</select>
			</label>
		{/if}
	</div>

	{#if tasks.error && !tasks.data}
		<ErrorNote error={tasks.error} what="tasks" />
	{:else if !tasks.data}
		<div class="panel panel-body"><div class="skeleton" style="height: 160px"></div></div>
	{:else if tasks.data.length === 0}
		<div class="panel empty">
			<strong>Nothing is waiting on a person</strong>
			When a run reaches a human step, the decision it needs appears here.
		</div>
	{:else}
		<div class="list">
			{#each shown as task (task.id)}
				<TaskCard {task} ondecided={() => stale.tasks++} />
			{:else}
				<div class="panel empty"><strong>No tasks for role {role}</strong></div>
			{/each}
		</div>
	{/if}
</div>

<style>
	.list {
		display: flex;
		flex-direction: column;
		gap: 12px;
		max-width: 980px;
	}

	.sub b {
		color: var(--text);
	}

	.warn {
		color: var(--warn);
	}
</style>
