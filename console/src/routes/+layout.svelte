<script lang="ts">
	import '../app.css';
	import { page } from '$app/state';
	import { api } from '#lib/api.ts';
	import { useQuery } from '#lib/query.svelte.ts';
	import { health, session, setActor, setKey, setWorkspace, stale, tick } from '#lib/session.svelte.ts';
	import type { Snippet } from 'svelte';

	let { children }: { children: Snippet } = $props();

	const workspaces = useQuery(() => {
		void stale.workspaces;
		return api.workspaces();
	}, { keep: true });

	// The count of open tasks sits in the nav, so it is kept fresh from here.
	const tasks = useQuery(
		() => {
			void stale.tasks;
			return api.tasks();
		},
		{ keep: true, refresh: () => 10000 }
	);

	// A remembered workspace that no longer exists would fail every request.
	$effect(() => {
		const list = workspaces.data;
		if (!list || list.length === 0) return;
		if (!list.some((w) => w.slug === session.workspace)) {
			setWorkspace(list.find((w) => w.slug === 'default')?.slug ?? list[0].slug);
		}
	});

	$effect(() => {
		const id = setInterval(tick, 5000);
		return () => clearInterval(id);
	});

	// Someone looking at an empty workspace is usually in the wrong one.
	const elsewhere = $derived.by(() => {
		const list = workspaces.data ?? [];
		const here = list.find((w) => w.slug === session.workspace);
		return here && here.workflows === 0 ? list.filter((w) => w.workflows > 0) : [];
	});

	// Behind a sign-in proxy the server already knows who is asking, so the
	// console shows that and asks for neither a key nor a name.
	const me = useQuery(() => api.me(), { keep: true });
	const signedIn = $derived(me.data?.signed_in ? me.data.name : '');
	$effect(() => {
		if (signedIn && session.actor !== signedIn) setActor(signedIn);
	});

	let actorDraft = $state(session.actor);
	let keyDraft = $state(session.key);
	// The server is asking for a key when a read comes back 401.
	const needsKey = $derived(workspaces.error?.status === 401);

	const nav = [
		{ href: '/', label: 'Overview', icon: 'M2.5 9.5 8 3l5.5 6.5M4 8.5v5h8v-5' },
		{ href: '/workflows', label: 'Workflows', icon: 'M3 2.5h4v3.5H3zM9 10h4v3.5H9zM5 6v2.5a1.5 1.5 0 0 0 1.5 1.5H9' },
		{ href: '/runs', label: 'Runs', icon: 'M4.5 3v10l8-5z' },
		{ href: '/events', label: 'Events', icon: 'M9 1.5 3.5 9H8l-1 5.5L12.5 7H8z' },
		{ href: '/tasks', label: 'Tasks', icon: 'M2.5 3.5h11v9h-11zM5 8l2 2 4-4' },
		{ href: '/templates', label: 'Templates', icon: 'M4 2.5h5.5L12.5 5.5v8H4zM6 8h4.5M6 10.5h4.5' },
		{ href: '/settings', label: 'Connections & registry', icon: 'M6 2.5v3M10 2.5v3M4 5.5h8V8a4 4 0 0 1-8 0zM8 12v2' }
	];

	const here = $derived(page.url.pathname);
	const isCurrent = (href: string) => (href === '/' ? here === '/' : here === href || here.startsWith(href + '/'));
	const openTasks = $derived(tasks.data?.length ?? 0);
	const known = $derived(workspaces.data ?? []);
</script>

<svelte:head>
	<title>Weir console</title>
	<meta name="theme-color" content="#071a22" />
</svelte:head>

<div class="shell">
	<aside class="rail">
		<a class="brand" href="/" aria-label="Weir console, overview">
			<svg viewBox="0 0 32 32" width="24" height="24" aria-hidden="true">
				<path d="M4 11h24M4 21h24" stroke="var(--current)" stroke-width="3.5" stroke-linecap="round" />
				<rect x="14" y="4" width="4" height="24" rx="2" fill="var(--foam)" />
			</svg>
			<span>weir</span>
			<small>console</small>
		</a>

		<nav aria-label="Console">
			{#each nav as item (item.href)}
				<a href={item.href} aria-current={isCurrent(item.href) ? 'page' : undefined}>
					<svg viewBox="0 0 16 16" width="16" height="16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
						<path d={item.icon} />
					</svg>
					<span>{item.label}</span>
					{#if item.href === '/tasks' && openTasks > 0}
						<b class="badge" title="{openTasks} open {openTasks === 1 ? 'task' : 'tasks'}">{openTasks}</b>
					{/if}
				</a>
			{/each}
		</nav>

		<div class="rail-foot">
			<span class="dot" class:down={health.unreachable}></span>
			<span>{health.unreachable ? 'API unreachable' : 'API connected'}</span>
		</div>
	</aside>

	<div class="main">
		<header class="topbar">
			<label class="ctl">
				<span>Workspace</span>
				<select class="select" value={session.workspace} onchange={(e) => setWorkspace(e.currentTarget.value)}>
					{#if !known.some((w) => w.slug === session.workspace)}
						<option value={session.workspace}>{session.workspace}</option>
					{/if}
					{#each known as w (w.slug)}
						<option value={w.slug}>{w.name && w.name !== w.slug ? `${w.name} (${w.slug})` : w.slug}</option>
					{/each}
				</select>
			</label>
			{#if signedIn}
				<span class="ctl"><span>Signed in as</span><strong>{signedIn}</strong></span>
			{:else}
			<label class="ctl">
				<span>API key</span>
				<input
					class="input"
					class:needed={needsKey}
					type="password"
					placeholder={needsKey ? 'this server needs a key' : 'if the server asks for one'}
					bind:value={keyDraft}
					onchange={() => setKey(keyDraft)}
					onblur={() => setKey(keyDraft)}
					autocomplete="off"
					spellcheck="false"
				/>
			</label>
			<label class="ctl">
				<span>Acting as</span>
				<input
					class="input"
					class:needed={!session.actor}
					placeholder="your name, for writes"
					bind:value={actorDraft}
					onchange={() => setActor(actorDraft)}
					onblur={() => setActor(actorDraft)}
					autocomplete="off"
					spellcheck="false"
				/>
			</label>
			{/if}
		</header>

		{#if elsewhere.length > 0}
			<div class="banner quiet">
				<div>
					<strong>The {session.workspace} workspace has no workflows.</strong>
					{#each elsewhere as w (w.slug)}
						<button class="link" onclick={() => setWorkspace(w.slug)}>
							{w.name || w.slug} has {w.workflows} workflow{w.workflows === 1 ? '' : 's'} and {w.runs} run{w.runs === 1 ? '' : 's'}
						</button>
					{/each}
				</div>
			</div>
		{/if}

		{#if health.unreachable}
			<div class="banner" role="alert">
				<svg viewBox="0 0 16 16" width="18" height="18" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
					<path d="M8 1.8 15 14H1zM8 6.5v3.5M8 12h.01" />
				</svg>
				<div>
					<strong>Cannot reach the Weir API.</strong>
					The console tried <code>{health.url}</code>{health.detail ? ` and got: ${health.detail}` : ''}.
					{#if import.meta.env.DEV}
						The dev server proxies <code>/v1</code> to <code>{__WEIR_PROXY_TARGET__}</code>: start the API there, or restart with
						<code>WEIR_API_URL</code> set to where it is.
					{:else}
						The console expects the API on this origin under <code>/v1</code>.
					{/if}
					It keeps trying; this banner goes when the API answers.
				</div>
			</div>
		{/if}

		<main>
			{@render children()}
		</main>
	</div>
</div>

<style>
	.shell {
		display: grid;
		grid-template-columns: var(--rail) minmax(0, 1fr);
		min-height: 100vh;
	}

	.rail {
		position: sticky;
		top: 0;
		height: 100vh;
		display: flex;
		flex-direction: column;
		background: var(--deep);
		color: var(--foam);
		border-right: 1px solid var(--deep-line);
		padding: 14px 10px;
		gap: 18px;
	}

	.brand {
		display: flex;
		align-items: center;
		gap: 9px;
		padding: 2px 8px;
		font-weight: 800;
		font-size: 20px;
		letter-spacing: -0.03em;
	}

	.brand small {
		font-size: 10.5px;
		font-weight: 700;
		letter-spacing: 0.12em;
		text-transform: uppercase;
		color: var(--current);
		margin-top: 5px;
	}

	nav {
		display: flex;
		flex-direction: column;
		gap: 2px;
	}

	nav a {
		display: flex;
		align-items: center;
		gap: 10px;
		padding: 7px 9px;
		border-radius: 8px;
		color: var(--foam-muted);
		font-weight: 600;
		line-height: 1.3;
	}

	nav a:hover {
		color: var(--foam);
		background: var(--deep-2);
	}

	nav a[aria-current='page'] {
		color: var(--foam);
		background: var(--deep-2);
		box-shadow: inset 2.5px 0 0 var(--current);
	}

	nav a[aria-current='page'] svg {
		color: var(--current);
	}

	nav svg {
		flex: none;
	}

	.badge {
		margin-left: auto;
		min-width: 20px;
		padding: 0 6px;
		border-radius: 999px;
		text-align: center;
		font-size: 11.5px;
		line-height: 1.6;
		font-variant-numeric: tabular-nums;
		background: #f2b23e;
		color: #2b1d00;
	}

	.rail-foot {
		margin-top: auto;
		display: flex;
		align-items: center;
		gap: 8px;
		padding: 6px 9px;
		font-size: 12px;
		color: var(--foam-muted);
	}

	.dot {
		width: 8px;
		height: 8px;
		border-radius: 50%;
		background: var(--current);
	}

	.dot.down {
		background: #ff8672;
		animation: pulse 1.1s ease-in-out infinite;
	}

	.main {
		display: flex;
		flex-direction: column;
		min-width: 0;
	}

	.topbar {
		position: sticky;
		top: 0;
		z-index: 5;
		display: flex;
		align-items: center;
		justify-content: flex-end;
		gap: 18px;
		height: var(--topbar);
		padding: 0 24px;
		background: color-mix(in srgb, var(--bg) 88%, transparent);
		backdrop-filter: blur(10px);
		border-bottom: 1px solid var(--line);
	}

	.ctl {
		display: flex;
		align-items: center;
		gap: 8px;
		min-width: 0;
	}

	.ctl > span {
		font-size: 11px;
		font-weight: 700;
		letter-spacing: 0.08em;
		text-transform: uppercase;
		color: var(--muted);
		white-space: nowrap;
	}

	.ctl .select,
	.ctl .input {
		padding: 4px 9px;
		width: 190px;
	}

	.input.needed {
		border-color: color-mix(in srgb, var(--warn) 60%, var(--line));
	}

	.banner {
		display: flex;
		gap: 12px;
		align-items: flex-start;
		margin: 16px 24px 0;
		padding: 12px 14px;
		border-radius: var(--radius);
		border: 1px solid color-mix(in srgb, var(--fail) 55%, var(--line));
		background: color-mix(in srgb, var(--fail) 10%, var(--surface));
		line-height: 1.55;
	}

	.banner.quiet {
		border-color: var(--line);
		background: var(--surface);
	}

	.banner.quiet strong {
		color: inherit;
		margin-right: 10px;
	}

	.banner .link {
		font: inherit;
		color: var(--accent, #2dd4bf);
		background: none;
		border: 0;
		padding: 0;
		margin-right: 14px;
		cursor: pointer;
		text-decoration: underline;
	}

	.banner svg {
		color: var(--fail);
		flex: none;
		margin-top: 1px;
	}

	.banner strong {
		color: var(--fail);
	}

	.banner code {
		font: 12px var(--mono);
		padding: 1px 5px;
		border-radius: 4px;
		background: color-mix(in srgb, var(--fail) 12%, transparent);
		overflow-wrap: anywhere;
	}

	main {
		flex: 1;
		min-width: 0;
	}

	@media (max-width: 860px) {
		.shell {
			grid-template-columns: minmax(0, 1fr);
		}

		.rail {
			position: static;
			height: auto;
			flex-direction: row;
			align-items: center;
			gap: 10px;
			padding: 8px 12px;
			overflow-x: auto;
			border-right: 0;
			border-bottom: 1px solid var(--deep-line);
		}

		.brand small,
		.rail-foot span:last-child {
			display: none;
		}

		nav {
			flex-direction: row;
		}

		nav a {
			white-space: nowrap;
		}

		nav a[aria-current='page'] {
			box-shadow: inset 0 -2.5px 0 var(--current);
		}

		.rail-foot {
			margin: 0 0 0 auto;
		}

		.topbar {
			position: static;
			height: auto;
			padding: 8px 16px;
			flex-wrap: wrap;
			justify-content: flex-start;
			gap: 8px 14px;
		}

		.ctl .select,
		.ctl .input {
			width: 150px;
		}

		.banner {
			margin: 12px 16px 0;
		}
	}
</style>
