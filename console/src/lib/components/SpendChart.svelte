<script lang="ts">
	import { int, money } from '#lib/format.ts';
	import type { UsageByDay } from '#lib/types.ts';

	// Spend per day as bars: one series, one axis, a tooltip on hover or focus.
	let { byDay, days }: { byDay: UsageByDay[]; days: number } = $props();

	const W = 720;
	const H = 170;
	const TOP = 12;
	const BOTTOM = 22;
	const LEFT = 58;
	const RIGHT = 8;

	interface Bar {
		day: string;
		runs: number;
		spent: number;
	}

	// Every day in the range gets a slot, so quiet days read as gaps and not
	// as a compressed axis. Days are the server's (UTC) calendar days.
	const bars = $derived.by<Bar[]>(() => {
		const have = new Map(byDay.map((d) => [d.day, d]));
		const out: Bar[] = [];
		const today = new Date();
		const end = Date.UTC(today.getUTCFullYear(), today.getUTCMonth(), today.getUTCDate());
		for (let i = days; i >= 0; i--) {
			const day = new Date(end - i * 86400000).toISOString().slice(0, 10);
			const row = have.get(day);
			have.delete(day);
			out.push({ day, runs: row?.runs ?? 0, spent: row?.spent_usd ?? 0 });
		}
		// Anything the server counted outside that window still belongs on the chart.
		for (const row of have.values()) out.push({ day: row.day, runs: row.runs, spent: row.spent_usd });
		return out.sort((a, b) => a.day.localeCompare(b.day));
	});

	const max = $derived(Math.max(...bars.map((b) => b.spent), 0));
	const top = $derived(niceMax(max));
	const ticks = $derived([0, top / 2, top]);
	const slot = $derived((W - LEFT - RIGHT) / Math.max(bars.length, 1));
	const barW = $derived(Math.max(2, Math.min(26, slot - 2)));
	const y = (v: number) => TOP + (H - TOP - BOTTOM) * (1 - (top ? v / top : 0));
	const xAxisLabels = $derived.by(() => {
		const n = bars.length;
		if (n === 0) return [];
		const picks = n <= 8 ? bars.map((_, i) => i) : [0, Math.floor(n / 3), Math.floor((2 * n) / 3), n - 1];
		return [...new Set(picks)];
	});

	let hover = $state<number | null>(null);
	const active = $derived(hover !== null ? bars[hover] : null);

	function niceMax(v: number): number {
		if (v <= 0) return 0;
		const pow = Math.pow(10, Math.floor(Math.log10(v)));
		for (const m of [1, 2, 2.5, 5, 10]) if (m * pow >= v) return m * pow;
		return 10 * pow;
	}

	const dayFmt = new Intl.DateTimeFormat(undefined, { month: 'short', day: 'numeric', timeZone: 'UTC' });
	const fullFmt = new Intl.DateTimeFormat(undefined, { weekday: 'short', month: 'short', day: 'numeric', year: 'numeric', timeZone: 'UTC' });
	const label = (day: string, fmt = dayFmt) => fmt.format(new Date(day + 'T00:00:00Z'));

	/** A bar with rounded top corners, anchored flat to the baseline. */
	function bar(i: number, v: number): string {
		const x = LEFT + i * slot + (slot - barW) / 2;
		const y0 = y(0);
		const h = Math.max(v > 0 ? 2 : 0, y0 - y(v));
		const r = Math.min(3, barW / 2, h);
		return `M${x} ${y0}V${y0 - h + r}Q${x} ${y0 - h} ${x + r} ${y0 - h}H${x + barW - r}Q${x + barW} ${y0 - h} ${x + barW} ${y0 - h + r}V${y0}Z`;
	}
</script>

{#if max <= 0}
	<div class="empty">
		<strong>No spend in the last {days} days</strong>
		{bars.some((b) => b.runs > 0) ? 'Runs happened, but none of them cost anything.' : 'Nothing has run in this window.'}
	</div>
{:else}
	<figure>
		<div class="readout" aria-live="polite">
			{#if active}
				<b>{label(active.day, fullFmt)}</b>
				<span>{money(active.spent)}</span>
				<span class="muted">{int(active.runs)} {active.runs === 1 ? 'run' : 'runs'}</span>
			{:else}
				<span class="muted">Hover a bar for the day's spend and runs</span>
			{/if}
		</div>
		<svg viewBox="0 0 {W} {H}" role="img" aria-label="Spend per day over the last {days} days" onmouseleave={() => (hover = null)}>
			{#each ticks as t, i (i)}
				<line class="grid" class:base={i === 0} x1={LEFT} x2={W - RIGHT} y1={y(t)} y2={y(t)} />
				<text class="tick" x={LEFT - 8} y={y(t) + 3.5} text-anchor="end">{money(t)}</text>
			{/each}
			{#each bars as b, i (b.day)}
				<path class="bar" class:on={hover === i} class:off={hover !== null && hover !== i} d={bar(i, b.spent)} />
				<rect
					class="hit"
					x={LEFT + i * slot}
					y={TOP}
					width={slot}
					height={H - TOP - BOTTOM}
					role="graphics-symbol"
					aria-label="{label(b.day, fullFmt)}: {money(b.spent)}, {b.runs} runs"
					tabindex="-1"
					onmouseenter={() => (hover = i)}
					onfocus={() => (hover = i)}
				>
					<title>{label(b.day, fullFmt)} — {money(b.spent)} · {b.runs} runs</title>
				</rect>
			{/each}
			{#each xAxisLabels as i (i)}
				<text class="tick" x={LEFT + i * slot + slot / 2} y={H - 6} text-anchor={i === 0 ? 'start' : i === bars.length - 1 ? 'end' : 'middle'}>
					{label(bars[i].day)}
				</text>
			{/each}
		</svg>
	</figure>
{/if}

<style>
	figure {
		margin: 0;
	}

	.readout {
		display: flex;
		gap: 12px;
		align-items: baseline;
		min-height: 22px;
		font-size: 12.5px;
		font-variant-numeric: tabular-nums;
	}

	.readout b {
		font-weight: 660;
	}

	svg {
		display: block;
		width: 100%;
		height: auto;
		max-height: 220px;
	}

	.grid {
		stroke: var(--line);
		stroke-width: 1;
		stroke-dasharray: 2 4;
	}

	.grid.base {
		stroke: var(--line-strong);
		stroke-dasharray: none;
	}

	.tick {
		fill: var(--muted);
		font: 10.5px var(--sans);
		font-variant-numeric: tabular-nums;
	}

	.bar {
		fill: var(--accent);
		transition: opacity 0.1s;
	}

	.bar.off {
		opacity: 0.45;
	}

	.hit {
		fill: transparent;
		outline: none;
	}
</style>
