<script lang="ts">
	import { api, toApiError } from '#lib/api.ts';
	import { shortId } from '#lib/format.ts';
	import { session } from '#lib/session.svelte.ts';
	import type { Task } from '#lib/types.ts';
	import KindChip from './KindChip.svelte';
	import Json from './Json.svelte';
	import Readable from './Readable.svelte';
	import Time from './Time.svelte';

	// One waiting human step: what is being asked, and the buttons to answer it.
	interface Props {
		task: Task;
		ondecided?: () => void;
		/** Leave out the link to the run, for use on the run's own page. */
		inline?: boolean;
	}

	let { task, ondecided, inline = false }: Props = $props();

	let note = $state('');
	let busy = $state('');
	let error = $state('');
	let done = $state('');
	let raw = $state(false);

	const outcomes = $derived(task.outcomes ?? []);

	function style(outcome: string): string {
		if (/^(approve|approved|accept|accepted|yes|ok)$/i.test(outcome)) return 'primary';
		if (/^(reject|rejected|deny|denied|decline|declined|no)$/i.test(outcome)) return 'danger';
		return '';
	}

	async function decide(outcome: string) {
		busy = outcome;
		error = '';
		try {
			await api.decideTask(task.id, outcome, note.trim());
			done = outcome;
			ondecided?.();
		} catch (e) {
			error = toApiError(e).message;
		} finally {
			busy = '';
		}
	}
</script>

<article class="task k-human">
	<header>
		<div class="what">
			<KindChip kind="human" label={task.step} />
			<a class="wf" href="/workflows/{task.namespace}/{task.name}">{task.namespace}/<b>{task.name}</b></a>
			{#if task.escalation_level > 0}
				<span class="tag esc" title="This task has been escalated {task.escalation_level} time(s)">escalated ×{task.escalation_level}</span>
			{/if}
		</div>
		<div class="meta">
			<span>
				for
				{#if task.user}<b class="mono">{task.user}</b>{:else if task.role}role <b class="mono">{task.role}</b>{:else}anyone{/if}
			</span>
			<span>waiting since <Time at={task.created_at} /></span>
			{#if !inline}
				<a class="link mono" href="/runs/{task.run_id}">run {shortId(task.run_id)}</a>
			{/if}
		</div>
	</header>

	<div class="asked">
		<div class="asked-head">
			<h2>What you are being asked</h2>
			<button type="button" class="btn small" onclick={() => (raw = !raw)}>{raw ? 'Readable' : 'JSON'}</button>
		</div>
		{#if raw}
			<Json value={task.input} max={320} />
		{:else}
			<div class="readable"><Readable value={task.input} /></div>
		{/if}
	</div>

	{#if done}
		<p class="form-ok">Recorded: {done}. The run is moving on.</p>
	{:else}
		<footer>
			<label class="field grow">
				<span>Note (optional)</span>
				<input class="input" bind:value={note} placeholder="Why you decided this way" maxlength="2000" />
			</label>
			<div class="actions">
				{#each outcomes as outcome (outcome)}
					<button type="button" class="btn {style(outcome)}" disabled={!!busy} onclick={() => decide(outcome)}>
						{busy === outcome ? 'Sending…' : outcome}
					</button>
				{:else}
					<span class="muted">This task lists no outcomes.</span>
				{/each}
			</div>
		</footer>
		{#if !session.actor}
			<p class="hint">Set "Acting as" at the top of the page to decide.</p>
		{/if}
		{#if error}<p class="form-error" role="alert">{error}</p>{/if}
	{/if}
</article>

<style>
	.task {
		display: flex;
		flex-direction: column;
		gap: 12px;
		padding: 14px;
		background: var(--surface);
		border: 1px solid var(--line);
		border-left: 4px solid var(--k);
		border-radius: var(--radius);
		min-width: 0;
	}

	header {
		display: flex;
		justify-content: space-between;
		align-items: center;
		gap: 8px 16px;
		flex-wrap: wrap;
	}

	.what,
	.meta {
		display: flex;
		align-items: center;
		gap: 10px;
		flex-wrap: wrap;
		min-width: 0;
	}

	.meta {
		color: var(--muted);
		font-size: 12.5px;
	}

	.meta b {
		color: var(--text);
		font-weight: 650;
	}

	.wf {
		font-family: var(--mono);
		font-size: 12.5px;
		color: var(--muted);
	}

	.wf b {
		color: var(--text);
	}

	.wf:hover b {
		text-decoration: underline;
	}

	.esc {
		color: var(--warn);
		border-color: color-mix(in srgb, var(--warn) 50%, var(--line));
	}

	.asked-head {
		display: flex;
		justify-content: space-between;
		align-items: center;
		margin-bottom: 8px;
	}

	.readable {
		padding: 12px;
		background: var(--surface-2);
		border: 1px solid var(--line);
		border-radius: var(--radius-s);
		max-height: 320px;
		overflow: auto;
	}

	footer {
		display: flex;
		align-items: flex-end;
		gap: 12px;
		flex-wrap: wrap;
	}

	.grow {
		flex: 1 1 240px;
	}

	.actions {
		display: flex;
		gap: 8px;
		flex-wrap: wrap;
	}

	.actions .btn {
		text-transform: capitalize;
		min-width: 92px;
	}

	.hint {
		color: var(--warn);
		font-size: 12.5px;
	}
</style>
