<script lang="ts">
	import { goto } from '$app/navigation';
	import { api, toApiError } from '#lib/api.ts';
	import { session } from '#lib/session.svelte.ts';
	import type { DefinitionAlias } from '#lib/types.ts';

	interface Props {
		open: boolean;
		namespace: string;
		name: string;
		aliases?: DefinitionAlias[];
	}

	let { open = $bindable(), namespace, name, aliases = [] }: Props = $props();

	let dialog = $state<HTMLDialogElement>();
	let input = $state('{\n  \n}');
	let alias = $state('live');
	let idempotencyKey = $state('');
	let busy = $state(false);
	let error = $state('');

	const parsed = $derived.by<{ ok: true; value: unknown } | { ok: false; message: string }>(() => {
		if (!input.trim()) return { ok: true, value: {} };
		try {
			return { ok: true, value: JSON.parse(input) };
		} catch (e) {
			return { ok: false, message: e instanceof Error ? e.message : 'not valid JSON' };
		}
	});

	$effect(() => {
		if (!dialog) return;
		if (open && !dialog.open) {
			error = '';
			dialog.showModal();
		} else if (!open && dialog.open) {
			dialog.close();
		}
	});

	async function start(event: SubmitEvent) {
		event.preventDefault();
		if (!parsed.ok) return;
		busy = true;
		error = '';
		try {
			const out = await api.startRun({
				namespace,
				name,
				alias: alias && alias !== 'live' ? alias : undefined,
				input: parsed.value,
				idempotency_key: idempotencyKey.trim() || undefined
			});
			open = false;
			await goto(`/runs/${out.id}`);
		} catch (e) {
			error = toApiError(e).message;
		} finally {
			busy = false;
		}
	}
</script>

<dialog bind:this={dialog} onclose={() => (open = false)} aria-labelledby="start-title">
	<form onsubmit={start}>
		<header>
			<div>
				<h3 id="start-title">Start a run</h3>
				<p class="mono muted">{namespace}/{name}</p>
			</div>
			<button type="button" class="btn small" onclick={() => (open = false)} aria-label="Close">Close</button>
		</header>

		<label class="field">
			<span>Input (JSON)</span>
			<textarea class="textarea" rows="11" bind:value={input} spellcheck="false" aria-invalid={!parsed.ok}></textarea>
		</label>
		{#if !parsed.ok}
			<p class="form-error">Not valid JSON: {parsed.message}</p>
		{/if}

		<div class="grid">
			<label class="field">
				<span>Version</span>
				<select class="select" bind:value={alias}>
					{#each aliases as a (a.name)}
						<option value={a.name}>{a.name} (v{a.version})</option>
					{:else}
						<option value="live">live</option>
					{/each}
				</select>
			</label>
			<label class="field">
				<span>Idempotency key (optional)</span>
				<input class="input mono" bind:value={idempotencyKey} placeholder="e.g. ticket-4821" />
			</label>
		</div>

		{#if error}<p class="form-error" role="alert">{error}</p>{/if}

		<footer>
			<span class="muted">
				{#if session.actor}Requested by <b class="mono">{session.actor}</b>{:else}<span class="warn">Set "Acting as" at the top of the page first.</span>{/if}
			</span>
			<button type="submit" class="btn primary" disabled={busy || !parsed.ok}>{busy ? 'Starting…' : 'Start run'}</button>
		</footer>
	</form>
</dialog>

<style>
	dialog {
		width: min(620px, calc(100vw - 32px));
		padding: 0;
		border: 1px solid var(--line-strong);
		border-radius: 14px;
		background: var(--surface);
		color: var(--text);
		box-shadow: var(--shadow);
	}

	dialog::backdrop {
		background: rgb(4 14 18 / 0.62);
		backdrop-filter: blur(2px);
	}

	form {
		display: flex;
		flex-direction: column;
		gap: 12px;
		padding: 18px;
	}

	header,
	footer {
		display: flex;
		justify-content: space-between;
		align-items: center;
		gap: 12px;
	}

	h3 {
		font-size: 16px;
	}

	.grid {
		display: grid;
		grid-template-columns: minmax(0, 1fr) minmax(0, 1.4fr);
		gap: 12px;
	}

	footer {
		margin-top: 4px;
		font-size: 12.5px;
	}

	footer b {
		color: var(--text);
	}

	.warn {
		color: var(--warn);
	}

	@media (max-width: 520px) {
		.grid {
			grid-template-columns: minmax(0, 1fr);
		}
	}
</style>
