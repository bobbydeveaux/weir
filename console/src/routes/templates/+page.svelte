<script lang="ts">
	import { api } from '#lib/api.ts';
	import ErrorNote from '#lib/components/ErrorNote.svelte';
	import Json from '#lib/components/Json.svelte';
	import Time from '#lib/components/Time.svelte';
	import { useQuery } from '#lib/query.svelte.ts';
	import type { TemplateBody } from '#lib/types.ts';

	const list = useQuery(() => api.templates(), { keep: true });

	let picked = $state<string | null>(null);
	let version = $state<number | null>(null);

	const key = $derived(picked && list.data?.some((t) => t.key === picked) ? picked : (list.data?.[0]?.key ?? null));
	const detail = useQuery(() => (key ? api.template(key) : null));

	const versions = $derived(detail.data?.key === key ? (detail.data?.versions ?? []) : []);
	const shown = $derived(versions.find((v) => v.version === version) ?? versions[0]);

	function choose(k: string) {
		picked = k;
		version = null;
	}

	// The fields the engine defines come first; anything else is shown as it is.
	const extras = (body: TemplateBody) => {
		const { system: _s, user: _u, model: _m, ...rest } = body as TemplateBody & Record<string, unknown>;
		return rest;
	};
</script>

<svelte:head><title>Templates · Weir console</title></svelte:head>

<div class="page">
	<div class="page-head">
		<div>
			<h1>Templates</h1>
			<p class="sub">Versioned prompts. A step names one as <code class="mono">key@version</code>, so a published version never changes under a workflow.</p>
		</div>
	</div>

	{#if list.error && !list.data}
		<ErrorNote error={list.error} what="templates" />
	{:else if !list.data}
		<div class="panel panel-body"><div class="skeleton" style="height: 200px"></div></div>
	{:else if list.data.length === 0}
		<div class="panel empty">
			<strong>No templates yet</strong>
			Publish one with <code class="mono">POST /v1/templates/&lbrace;key&rbrace;</code> and a body of <code class="mono">system</code>, <code class="mono">user</code> and
			<code class="mono">model</code>.
		</div>
	{:else}
		<div class="split">
			<nav class="panel keys" aria-label="Templates">
				{#each list.data as t (t.key)}
					<button type="button" class:on={t.key === key} onclick={() => choose(t.key)}>
						<span class="mono k">{t.key}</span>
						<span class="meta">
							<span class="tag">v{t.version}</span>
							<span>{t.versions} {t.versions === 1 ? 'version' : 'versions'}</span>
							<span>· <Time at={t.published_at} /></span>
						</span>
					</button>
				{/each}
			</nav>

			<section class="panel">
				{#if detail.error && !detail.data}
					<div class="panel-body"><ErrorNote error={detail.error} what="this template" /></div>
				{:else if !shown}
					<div class="panel-body"><div class="skeleton" style="height: 200px"></div></div>
				{:else}
					<div class="panel-head">
						<div class="row">
							<h3 class="mono">{key}@{shown.version}</h3>
							{#if shown.version === versions[0]?.version}<span class="tag live">latest</span>{/if}
						</div>
						<div class="seg" role="group" aria-label="Version">
							{#each versions as v (v.version)}
								<button type="button" aria-pressed={v.version === shown.version} onclick={() => (version = v.version)}>v{v.version}</button>
							{/each}
						</div>
					</div>
					<div class="panel-body body">
						<dl class="kv">
							<dt>Published by</dt>
							<dd class="mono">{shown.published_by}</dd>
							<dt>Published</dt>
							<dd><Time at={shown.published_at} /></dd>
							<dt>Model</dt>
							<dd>
								{#if shown.body.model}<span class="mono">{shown.body.model}</span>{:else}<span class="muted">chosen by the step's tier</span>{/if}
							</dd>
							<dt>Body hash</dt>
							<dd class="mono muted hash" title={shown.body_hash}>{shown.body_hash}</dd>
						</dl>

						{#if shown.body.system}
							<div>
								<h2>System</h2>
								<Json value={shown.body.system} text max={300} />
							</div>
						{/if}
						<div>
							<h2>User</h2>
							<Json value={shown.body.user ?? ''} text max={420} />
						</div>
						{#if Object.keys(extras(shown.body)).length}
							<div>
								<h2>Other fields</h2>
								<Json value={extras(shown.body)} max={240} />
							</div>
						{/if}
					</div>
				{/if}
			</section>
		</div>
	{/if}
</div>

<style>
	.sub code,
	.empty code {
		padding: 1px 5px;
		background: var(--surface-2);
		border-radius: 4px;
	}

	.split {
		display: grid;
		grid-template-columns: minmax(240px, 320px) minmax(0, 1fr);
		gap: 16px;
		align-items: start;
	}

	.keys {
		display: flex;
		flex-direction: column;
		padding: 6px;
		gap: 2px;
		max-height: calc(100vh - var(--topbar) - 140px);
		overflow-y: auto;
	}

	.keys button {
		display: flex;
		flex-direction: column;
		gap: 3px;
		text-align: left;
		padding: 8px 10px;
		border: 0;
		border-radius: 7px;
		background: none;
	}

	.keys button:hover {
		background: var(--surface-2);
	}

	.keys button.on {
		background: var(--surface-2);
		box-shadow: inset 2.5px 0 0 var(--accent);
	}

	.k {
		font-weight: 700;
		font-size: 13px;
		overflow-wrap: anywhere;
	}

	.meta {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 6px;
		font-size: 12px;
		color: var(--muted);
	}

	.body {
		display: flex;
		flex-direction: column;
		gap: 14px;
	}

	.body h2 {
		margin-bottom: 6px;
	}

	.hash {
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	.seg {
		max-width: 50%;
		overflow-x: auto;
	}

	@media (max-width: 900px) {
		.split {
			grid-template-columns: minmax(0, 1fr);
		}

		.keys {
			max-height: 240px;
		}
	}
</style>
