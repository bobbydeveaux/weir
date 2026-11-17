<script lang="ts">
	import { api, toApiError } from '#lib/api.ts';
	import ErrorNote from '#lib/components/ErrorNote.svelte';
	import Json from '#lib/components/Json.svelte';
	import Time from '#lib/components/Time.svelte';
	import { useQuery } from '#lib/query.svelte.ts';
	import { session, setWorkspace, stale } from '#lib/session.svelte.ts';

	const connections = useQuery(() => api.connections(), { keep: true });
	const registry = useQuery(() => api.registry(), { keep: true });
	const workspaces = useQuery(
		() => {
			void stale.workspaces;
			return api.workspaces();
		},
		{ keep: true }
	);

	const handlers = $derived(registry.data?.handlers ?? []);
	const tools = $derived(registry.data?.tools ?? []);
	const providers = $derived(registry.data?.decide_providers ?? []);

	let slug = $state('');
	let name = $state('');
	let busy = $state(false);
	let error = $state('');
	let created = $state('');

	const slugOk = $derived(/^[a-z][a-z0-9-]*$/.test(slug));

	async function create(event: SubmitEvent) {
		event.preventDefault();
		busy = true;
		error = '';
		created = '';
		try {
			const out = await api.createWorkspace(slug, name.trim() || slug);
			created = out.slug;
			slug = '';
			name = '';
			stale.workspaces++;
		} catch (e) {
			error = toApiError(e).message;
		} finally {
			busy = false;
		}
	}
</script>

<svelte:head><title>Connections & registry · Weir console</title></svelte:head>

<div class="page">
	<div class="page-head">
		<div>
			<h1>Connections & registry</h1>
			<p class="sub">What this workspace can reach, what the server can run, and the workspaces themselves.</p>
		</div>
	</div>

	<section class="panel">
		<div class="panel-head">
			<h2>Connections</h2>
			<span class="muted small">Addresses for external steps and MCP servers that bring tools, in workspace <b class="mono">{session.workspace}</b>. Weir holds the name of a secret, never its value.</span>
		</div>
		{#if connections.error && !connections.data}
			<div class="panel-body"><ErrorNote error={connections.error} what="connections" /></div>
		{:else if !connections.data}
			<div class="panel-body"><div class="skeleton" style="height: 60px"></div></div>
		{:else if connections.data.length === 0}
			<div class="empty">
				<strong>No connections</strong>
				Add one with <code class="mono">PUT /v1/connections/&lbrace;name&rbrace;</code> and a <code class="mono">base_url</code>.
				For an MCP server, add <code class="mono">"kind": "mcp"</code> and its tools join this workspace.
			</div>
		{:else}
			<div class="table-wrap">
				<table class="table">
					<thead>
						<tr>
							<th>Name</th>
							<th>Kind</th>
							<th>Address</th>
							<th>Tools</th>
							<th>Signs in with</th>
							<th>Created</th>
						</tr>
					</thead>
					<tbody>
						{#each connections.data as c (c.name)}
							<tr>
								<td class="mono"><b class="k-external ext">{c.name}</b></td>
								<td><span class="tag" class:live={c.kind === 'mcp'}>{c.kind === 'mcp' ? 'MCP' : 'HTTP'}</span></td>
								<td class="mono">{c.base_url}</td>
								<td>
									{#if c.kind === 'mcp'}
										{#each c.tools ?? [] as t (t.name)}
											<span class="tag mono" title={t.description}>{c.name}.{t.name} · {t.class}</span>{' '}
										{:else}
											<span class="muted small">none offered</span>
										{/each}
										{#if c.synced_at}<div class="muted small">asked <Time at={c.synced_at} /></div>{/if}
									{:else}
										<span class="muted small">—</span>
									{/if}
								</td>
								<td>
									{#if c.auth && c.auth.kind !== 'none'}
										<span class="tag live">{c.auth.kind}</span>
										<div class="muted small mono">
											{c.auth.secret}{#if c.auth.header} as {c.auth.header}{/if}{#if c.auth.username} for {c.auth.username}{/if}
										</div>
									{:else}
										<span class="tag">none</span>
									{/if}
									{#if c.webhook}
										<div class="muted small" title="Give this address to the sender. Requests are checked against {c.webhook.secret}.">
											receives <b>{c.webhook.kind}</b> webhooks at
											<span class="mono">/v1/webhooks/{session.workspace}/{c.name}</span>
										</div>
									{/if}
								</td>
								<td><Time at={c.created_at} /></td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		{/if}
	</section>

	{#if registry.error && !registry.data}
		<ErrorNote error={registry.error} what="the registry" />
	{:else}
		<div class="cols">
			<section class="panel">
				<div class="panel-head">
					<h2>Handlers <span class="n">{handlers.length}</span></h2>
					<span class="muted small">Code a deterministic step may name</span>
				</div>
				<div class="panel-body">
					{#if !registry.data}
						<div class="skeleton" style="height: 40px"></div>
					{:else if handlers.length === 0}
						<p class="muted">No handlers are registered on this server.</p>
					{:else}
						<ul class="names k-deterministic">
							{#each handlers as h (h)}<li class="mono">{h}</li>{/each}
						</ul>
					{/if}
				</div>
			</section>

			<section class="panel">
				<div class="panel-head">
					<h2>Decide providers <span class="n">{providers.length}</span></h2>
					<span class="muted small">What may answer a decide step</span>
				</div>
				<div class="panel-body">
					{#if !registry.data}
						<div class="skeleton" style="height: 40px"></div>
					{:else if providers.length === 0}
						<p class="muted">None.</p>
					{:else}
						<ul class="names k-decide">
							{#each providers as p (p)}<li class="mono">{p}</li>{/each}
						</ul>
					{/if}
				</div>
			</section>
		</div>

		<section class="panel">
			<div class="panel-head">
				<h2>Tools <span class="n">{tools.length}</span></h2>
				<span class="muted small">What an ai step may be allowed to call</span>
			</div>
			{#if !registry.data}
				<div class="panel-body"><div class="skeleton" style="height: 60px"></div></div>
			{:else if tools.length === 0}
				<div class="empty"><strong>No tools are registered</strong>Agents on this server can only read their input and write their output.</div>
			{:else}
				<ul class="tools">
					{#each tools as t (t.name)}
						<li>
							<div class="tool-head">
								<b class="mono">{t.name}</b>
								<span class="tag" class:write={t.class !== 'read'}>{t.class}</span>
							</div>
							<p class="muted small">
								{#if t.connection}from the <b class="mono">{t.connection}</b> connection, this workspace only{:else}built into this server, offered to every workspace{/if}
							</p>
							<p class="muted">{t.description || 'No description.'}</p>
							{#if t.input_schema && Object.keys(t.input_schema).length}
								<details>
									<summary>Input schema</summary>
									<Json value={t.input_schema} max={240} />
								</details>
							{/if}
						</li>
					{/each}
				</ul>
			{/if}
		</section>
	{/if}

	<section class="panel">
		<div class="panel-head">
			<h2>Workspaces</h2>
			<span class="muted small">Every request is scoped to one. Nothing is shared between them.</span>
		</div>
		{#if workspaces.error && !workspaces.data}
			<div class="panel-body"><ErrorNote error={workspaces.error} what="workspaces" /></div>
		{:else if !workspaces.data}
			<div class="panel-body"><div class="skeleton" style="height: 60px"></div></div>
		{:else}
			<div class="table-wrap">
				<table class="table">
					<thead>
						<tr>
							<th>Slug</th>
							<th>Name</th>
							<th>Created</th>
							<th></th>
						</tr>
					</thead>
					<tbody>
						{#each workspaces.data as w (w.slug)}
							<tr>
								<td class="mono"><b>{w.slug}</b></td>
								<td>{w.name}</td>
								<td><Time at={w.created_at} /></td>
								<td class="num">
									{#if w.slug === session.workspace}
										<span class="tag live">current</span>
									{:else}
										<button type="button" class="btn small" onclick={() => setWorkspace(w.slug)}>Switch to</button>
									{/if}
								</td>
							</tr>
						{:else}
							<tr><td colspan="4" class="muted">No workspaces.</td></tr>
						{/each}
					</tbody>
				</table>
			</div>
		{/if}

		<form class="create" onsubmit={create}>
			<h3>Create a workspace</h3>
			<div class="row">
				<label class="field">
					<span>Slug</span>
					<input class="input mono" bind:value={slug} placeholder="acme" required autocomplete="off" spellcheck="false" />
				</label>
				<label class="field grow">
					<span>Name</span>
					<input class="input" bind:value={name} placeholder="Acme Ltd" autocomplete="off" />
				</label>
				<button type="submit" class="btn primary" disabled={busy || !slug}>{busy ? 'Creating…' : 'Create workspace'}</button>
			</div>
			{#if slug && !slugOk}
				<p class="hint">Slugs are usually lower-case letters, digits and hyphens, starting with a letter. The server has the final say.</p>
			{/if}
			{#if !session.actor}<p class="hint">Set "Acting as" at the top of the page first.</p>{/if}
			{#if error}<p class="form-error" role="alert">{error}</p>{/if}
			{#if created}
				<p class="form-ok">
					Created <b class="mono">{created}</b>.
					<button type="button" class="btn small" onclick={() => setWorkspace(created)}>Switch to it</button>
				</p>
			{/if}
		</form>
	</section>
</div>

<style>
	.small {
		font-size: 12px;
	}

	.small b {
		color: var(--text);
	}

	h2 .n {
		color: var(--faint);
		margin-left: 4px;
	}

	.cols {
		display: grid;
		grid-template-columns: repeat(2, minmax(0, 1fr));
		gap: 16px;
	}

	.ext {
		color: var(--k);
	}

	.names {
		list-style: none;
		margin: 0;
		padding: 0;
		display: flex;
		flex-wrap: wrap;
		gap: 6px;
	}

	.names li {
		padding: 2px 9px;
		border-radius: 6px;
		font-weight: 600;
		color: var(--k);
		background: color-mix(in srgb, var(--k) 11%, transparent);
		border: 1px solid color-mix(in srgb, var(--k) 32%, transparent);
	}

	.tools {
		list-style: none;
		margin: 0;
		padding: 0;
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(min(100%, 340px), 1fr));
	}

	.tools li {
		display: flex;
		flex-direction: column;
		gap: 5px;
		padding: 12px 14px;
		border-bottom: 1px solid var(--line);
		border-right: 1px solid var(--line);
		min-width: 0;
	}

	.tool-head {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 8px;
	}

	.tag.write {
		color: var(--warn);
		border-color: color-mix(in srgb, var(--warn) 45%, var(--line));
	}

	details summary {
		cursor: pointer;
		font-size: 12px;
		font-weight: 650;
		color: var(--muted);
		margin-bottom: 6px;
	}

	.create {
		display: flex;
		flex-direction: column;
		gap: 10px;
		padding: 14px;
		border-top: 1px solid var(--line);
	}

	.create .row {
		align-items: flex-end;
	}

	.grow {
		flex: 1 1 220px;
	}

	.hint {
		color: var(--warn);
		font-size: 12.5px;
	}

	.empty code {
		padding: 1px 5px;
		background: var(--surface-2);
		border-radius: 4px;
	}

	@media (max-width: 860px) {
		.cols {
			grid-template-columns: minmax(0, 1fr);
		}
	}
</style>
