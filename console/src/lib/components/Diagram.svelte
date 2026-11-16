<script lang="ts">
	import { tone } from '#lib/format.ts';
	import { edgeKey, layout, summary } from '#lib/graph.ts';
	import type { SpecStep, StepOverlay } from '#lib/types.ts';
	import KindIcon from './KindIcon.svelte';
	import Status from './Status.svelte';

	interface Props {
		steps: SpecStep[];
		selected?: string | null;
		onselect?: (id: string) => void;
		/** Live status per step id. When given, the diagram shows a run. */
		overlay?: Record<string, StepOverlay>;
		/** Edges the run actually took, as edgeKey(from, to). */
		traversed?: Set<string>;
	}

	let { steps, selected = null, onselect, overlay, traversed }: Props = $props();

	const g = $derived(layout(steps));
	const kindOf = $derived(new Map(steps.map((s) => [s.id, s.kind])));
	const inRun = $derived(overlay !== undefined);
	const first = $derived(g.nodes[0]);
</script>

{#if steps.length === 0}
	<div class="empty"><strong>No steps</strong>This version defines no steps.</div>
{:else}
	<div class="scroll">
		<div class="diagram" class:in-run={inRun} style:width="{g.width}px" style:height="{g.height + 26}px">
			<svg width={g.width} height={g.height + 26} aria-hidden="true">
				{#if first}
					<circle class="start" cx={g.start.x} cy={g.start.y + 6} r="5" />
					<path class="edge spine" class:hot={inRun && overlay?.[first.step.id]} d="M{g.start.x} {g.start.y + 11}V{first.y - 1}" />
					<polygon class="head spine" class:hot={inRun && overlay?.[first.step.id]} points="{g.start.x},{first.y} {g.start.x - 4.5},{first.y - 8} {g.start.x + 4.5},{first.y - 8}" />
					<text class="caption" x={g.start.x + 12} y={g.start.y + 10}>start</text>
				{/if}
				{#each g.edges as e (e.id)}
					{@const hot = inRun && traversed?.has(edgeKey(e.from, e.to))}
					<path class="edge {e.shape} k-{kindOf.get(e.from)}" class:hot class:conditional={e.conditional} d={e.d} />
					<polygon class="head {e.shape} k-{kindOf.get(e.from)}" class:hot points={e.head} />
				{/each}
				{#each g.nodes as n (n.step.id)}
					{#if n.terminal}
						{@const done = inRun && overlay?.[n.step.id]?.status === 'succeeded'}
						<path class="edge spine" class:hot={done} d="M{g.start.x} {n.y + n.h}v9" />
						<circle class="end" class:hot={done} cx={g.start.x} cy={n.y + n.h + 15} r="6" />
						<circle class="end-dot" class:hot={done} cx={g.start.x} cy={n.y + n.h + 15} r="2.6" />
						<text class="caption" x={g.start.x + 12} y={n.y + n.h + 19}>end</text>
					{/if}
				{/each}
			</svg>

			{#each g.edges as e (e.id)}
				{#if e.label}
					<span
						class="elabel {e.shape} k-{kindOf.get(e.from)}"
						class:hot={inRun && traversed?.has(edgeKey(e.from, e.to))}
						style:left="{e.labelX}px"
						style:top="{e.labelY}px"
						title={e.shape === 'arc' ? `when ${e.fullLabel} → go to ${e.to}` : 'When no rule above matches'}
					>
						{e.label}
					</span>
				{/if}
			{/each}

			{#each g.nodes as n (n.step.id)}
				{@const o = overlay?.[n.step.id]}
				<button
					type="button"
					class="node k-{n.step.kind} {o ? 't-' + tone(o.status) : ''}"
					class:selected={selected === n.step.id}
					class:visited={!!o}
					class:unvisited={inRun && !o}
					class:live={o?.status === 'running'}
					style:left="{n.x}px"
					style:top="{n.y}px"
					style:width="{n.w}px"
					style:height="{n.h}px"
					aria-pressed={selected === n.step.id}
					onclick={() => onselect?.(n.step.id)}
				>
					<span class="head-row">
						<span class="kind"><KindIcon kind={n.step.kind} size={13} />{n.step.kind}</span>
						{#if o}
							<Status status={o.status} small />
						{:else if n.terminal}
							<span class="endtag">ends run</span>
						{/if}
					</span>
					<span class="id">
						{n.step.id}
						{#if o && o.visits > 1}<span class="visits" title="Visited {o.visits} times">×{o.visits}</span>{/if}
					</span>
					<span class="sum">
						{#if o?.note}<span class="note">→ {o.note}</span>{:else}{summary(n.step)}{/if}
					</span>
					{#if n.markers.length}
						<span class="markers">
							{#each n.markers as m (m.text)}
								<span class="marker" class:warn={m.text.startsWith('on failure')} title={m.title}>{m.text}</span>
							{/each}
						</span>
					{/if}
				</button>
			{/each}
		</div>
	</div>
	{#if g.missing.length}
		<p class="missing">
			A rule points at {g.missing.length === 1 ? 'a step' : 'steps'} this version does not define:
			<span class="mono">{g.missing.join(', ')}</span>
		</p>
	{/if}
{/if}

<style>
	.scroll {
		overflow-x: auto;
		padding: 4px;
	}

	.diagram {
		position: relative;
		margin: 0 auto;
	}

	svg {
		position: absolute;
		inset: 0;
		overflow: visible;
	}

	.edge {
		fill: none;
		stroke: var(--line-strong);
		stroke-width: 1.6;
	}

	.edge.arc {
		stroke: color-mix(in srgb, var(--k) 62%, var(--line));
	}

	.edge.arc.conditional {
		stroke-dasharray: 5 4;
	}

	.head {
		fill: var(--line-strong);
	}

	.head.arc {
		fill: color-mix(in srgb, var(--k) 70%, var(--line));
	}

	.start {
		fill: var(--muted);
	}

	.end {
		fill: none;
		stroke: var(--muted);
		stroke-width: 1.6;
	}

	.end-dot {
		fill: var(--muted);
	}

	.caption {
		fill: var(--faint);
		font: 600 10.5px var(--sans);
		letter-spacing: 0.08em;
		text-transform: uppercase;
	}

	/* In a run, the path taken is lit and the rest recedes. */
	.in-run .edge,
	.in-run .head {
		opacity: 0.45;
	}

	.in-run .edge.hot {
		stroke: var(--accent);
		stroke-width: 2.2;
		stroke-dasharray: none;
		opacity: 1;
	}

	.in-run .head.hot {
		fill: var(--accent);
		opacity: 1;
	}

	.end.hot {
		stroke: var(--ok);
	}

	.end-dot.hot {
		fill: var(--ok);
	}

	.elabel {
		position: absolute;
		transform: translateY(-50%);
		font: 600 11px/1.5 var(--mono);
		white-space: nowrap;
	}

	.elabel.arc {
		padding: 1px 7px;
		border-radius: 999px;
		color: var(--k);
		background: color-mix(in srgb, var(--k) 12%, var(--surface));
		border: 1px solid color-mix(in srgb, var(--k) 45%, var(--line));
	}

	.elabel.spine {
		color: var(--faint);
		font-family: var(--sans);
		font-style: italic;
	}

	.in-run .elabel.arc:not(.hot) {
		opacity: 0.6;
	}

	.in-run .elabel.arc.hot {
		color: var(--accent);
		border-color: var(--accent);
		background: color-mix(in srgb, var(--accent) 14%, var(--surface));
	}

	.node {
		position: absolute;
		display: flex;
		flex-direction: column;
		align-items: stretch;
		gap: 1px;
		text-align: left;
		padding: 8px 12px 8px 15px;
		background: var(--surface);
		border: 1px solid color-mix(in srgb, var(--k) 38%, var(--line));
		border-radius: 10px;
		overflow: hidden;
		transition:
			border-color 0.12s,
			box-shadow 0.12s,
			transform 0.12s;
	}

	.node::before {
		content: '';
		position: absolute;
		inset: 0 auto 0 0;
		width: 4px;
		background: var(--k);
	}

	.node:hover {
		border-color: var(--k);
		transform: translateX(1px);
	}

	.node.selected {
		border-color: var(--k);
		background: color-mix(in srgb, var(--k) 9%, var(--surface));
		box-shadow: 0 0 0 3px color-mix(in srgb, var(--k) 26%, transparent);
	}

	.node.unvisited {
		opacity: 0.55;
	}

	.node.visited {
		border-color: color-mix(in srgb, var(--t) 60%, var(--line));
	}

	.node.live {
		box-shadow: 0 0 0 3px color-mix(in srgb, var(--t) 30%, transparent);
		animation: glow 1.4s ease-in-out infinite;
	}

	@keyframes glow {
		50% {
			box-shadow: 0 0 0 6px color-mix(in srgb, var(--t) 10%, transparent);
		}
	}

	.head-row {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 8px;
		min-height: 18px;
	}

	.kind {
		display: inline-flex;
		align-items: center;
		gap: 5px;
		color: var(--k);
		font-size: 10.5px;
		font-weight: 720;
		letter-spacing: 0.08em;
		text-transform: uppercase;
	}

	.endtag {
		font-size: 10.5px;
		color: var(--faint);
		font-weight: 600;
	}

	.id {
		font: 700 13.5px/1.35 var(--mono);
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	.visits {
		color: var(--muted);
		font-weight: 600;
		font-size: 11.5px;
		margin-left: 4px;
	}

	.sum {
		font: 11.5px/1.4 var(--mono);
		color: var(--muted);
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	.note {
		color: var(--text);
		font-weight: 650;
	}

	.markers {
		display: flex;
		gap: 5px;
		margin-top: 4px;
		overflow: hidden;
	}

	.marker {
		font-size: 10.5px;
		font-weight: 620;
		line-height: 1.5;
		padding: 0 6px;
		border-radius: 4px;
		color: var(--muted);
		background: var(--surface-3);
		white-space: nowrap;
	}

	.marker.warn {
		color: var(--warn);
		background: color-mix(in srgb, var(--warn) 14%, transparent);
	}

	.missing {
		margin: 8px 4px 0;
		color: var(--warn);
		font-size: 12.5px;
	}
</style>
