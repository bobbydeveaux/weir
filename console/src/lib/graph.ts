// Lays a workflow out as a diagram: steps down a spine in the order they are
// written, a straight edge for "carry on to the next step", and an arc on the
// right for every rule that jumps somewhere else.

import type { SpecStep } from './types';

export const NODE_W = 272;
const NODE_H = 70;
const MARKER_ROW = 22;
const GAP = 34;
const PAD = 18;
const START_H = 40;
const PORT_FIRST = 46;
const PORT_STEP = 24;
const IN_PORT = 18;
const LANE_FIRST = 22;
const LANE_STEP = 18;
const LABEL_MAX = 34;
const R = 8;

export interface NodeBox {
	step: SpecStep;
	index: number;
	x: number;
	y: number;
	w: number;
	h: number;
	/** The run stops here when no rule sends it elsewhere. */
	terminal: boolean;
	markers: { text: string; title: string }[];
}

export interface Edge {
	id: string;
	from: string;
	to: string;
	/** spine: straight down to the next step. arc: a jump drawn on the right. */
	shape: 'spine' | 'arc';
	conditional: boolean;
	d: string;
	/** Arrowhead polygon points. */
	head: string;
	label?: string;
	fullLabel?: string;
	labelX: number;
	labelY: number;
}

export interface Layout {
	width: number;
	height: number;
	nodes: NodeBox[];
	edges: Edge[];
	start: { x: number; y: number };
	/** Step ids named by a rule but missing from the definition. */
	missing: string[];
}

interface Arc {
	from: number;
	to: number;
	port: number;
	when?: string;
	lane: number;
}

function clip(text: string): string {
	return text.length > LABEL_MAX ? text.slice(0, LABEL_MAX - 1) + '…' : text;
}

/** A rough width for a label set in 11px monospace, padding included. */
function labelWidth(text: string): number {
	return Math.ceil(text.length * 6.7) + 16;
}

export function edgeKey(from: string, to: string): string {
	return from + '>' + to;
}

export function layout(steps: SpecStep[]): Layout {
	const index = new Map(steps.map((s, i) => [s.id, i]));
	const missing: string[] = [];
	const arcs: Arc[] = [];
	const spine: { from: number; conditional: boolean }[] = [];
	const outPorts: number[] = steps.map(() => 0);
	const terminal: boolean[] = steps.map(() => false);

	steps.forEach((step, i) => {
		let fallsThrough = true;
		let hasConditions = false;
		for (const rule of step.next ?? []) {
			const target = index.get(rule.go);
			if (target === undefined) {
				if (!missing.includes(rule.go)) missing.push(rule.go);
				if (!rule.when) fallsThrough = false;
				if (!rule.when) break;
				continue;
			}
			if (rule.when) {
				hasConditions = true;
				arcs.push({ from: i, to: target, port: outPorts[i]++, when: rule.when, lane: 0 });
				continue;
			}
			// An unconditional rule always wins, so nothing after it can run.
			fallsThrough = false;
			if (target === i + 1) spine.push({ from: i, conditional: hasConditions });
			else arcs.push({ from: i, to: target, port: outPorts[i]++, lane: 0 });
			break;
		}
		if (fallsThrough) {
			if (step.end || i === steps.length - 1) terminal[i] = true;
			else spine.push({ from: i, conditional: hasConditions });
		}
	});

	// Boxes.
	const x = PAD;
	let y = PAD + START_H;
	const nodes: NodeBox[] = steps.map((step, i) => {
		const markers: NodeBox['markers'] = [];
		if (step.on_failure) markers.push({ text: `on failure: ${step.on_failure}`, title: `If this step fails for good, the run will ${step.on_failure}` });
		if (step.compensate) markers.push({ text: `undo: ${step.compensate}`, title: `Compensated by the handler ${step.compensate} if a later step fails` });
		if (step.retry?.attempts) markers.push({ text: `retry ×${step.retry.attempts}`, title: `Tried up to ${step.retry.attempts} times${step.retry.backoff ? ', backoff ' + step.retry.backoff : ''}` });
		if (step.timeout) markers.push({ text: `timeout ${step.timeout}`, title: `Times out after ${step.timeout}` });
		const base = NODE_H + (markers.length ? MARKER_ROW : 0);
		const forPorts = outPorts[i] ? PORT_FIRST + (outPorts[i] - 1) * PORT_STEP + 20 : 0;
		const h = Math.max(base, forPorts);
		const box: NodeBox = { step, index: i, x, y, w: NODE_W, h, terminal: terminal[i], markers };
		y += h + GAP;
		return box;
	});
	const bottom = nodes.length ? y - GAP : y;

	// Lanes: shorter arcs sit closer to the spine, and arcs that overlap
	// vertically never share a lane.
	const portY = (a: Arc) => nodes[a.from].y + PORT_FIRST + a.port * PORT_STEP;
	const inY = (a: Arc) => nodes[a.to].y + IN_PORT;
	const span = (a: Arc): [number, number] => [Math.min(portY(a), inY(a)), Math.max(portY(a), inY(a))];
	const order = [...arcs].sort((a, b) => span(a)[1] - span(a)[0] - (span(b)[1] - span(b)[0]));
	const lanes: [number, number][][] = [];
	for (const arc of order) {
		const [lo, hi] = span(arc);
		let lane = 0;
		while (lanes[lane]?.some(([a, b]) => lo <= b + 10 && hi >= a - 10)) lane++;
		(lanes[lane] ??= []).push([lo, hi]);
		arc.lane = lane;
	}

	const right = x + NODE_W;
	const labelZone = arcs.reduce((w, a) => (a.when ? Math.max(w, labelWidth(clip(a.when)) + 10) : w), 0);
	const laneX = (lane: number) => right + labelZone + LANE_FIRST + lane * LANE_STEP;

	const edges: Edge[] = [];
	const cx = x + 36;
	for (const s of spine) {
		const a = nodes[s.from];
		const b = nodes[s.from + 1];
		const y1 = a.y + a.h;
		const y2 = b.y;
		edges.push({
			id: `spine-${s.from}`,
			from: a.step.id,
			to: b.step.id,
			shape: 'spine',
			conditional: false,
			d: `M${cx} ${y1}V${y2 - 1}`,
			head: `${cx},${y2} ${cx - 4.5},${y2 - 8} ${cx + 4.5},${y2 - 8}`,
			label: s.conditional ? 'otherwise' : undefined,
			labelX: cx + 10,
			labelY: (y1 + y2) / 2
		});
	}
	for (const arc of arcs) {
		const y1 = portY(arc);
		const y2 = inY(arc);
		const lx = laneX(arc.lane);
		const dir = y2 >= y1 ? 1 : -1;
		const r = Math.min(R, Math.abs(y2 - y1) / 2);
		const d =
			`M${right} ${y1}H${lx - r}Q${lx} ${y1} ${lx} ${y1 + dir * r}` +
			`V${y2 - dir * r}Q${lx} ${y2} ${lx - r} ${y2}H${right + 1}`;
		const full = arc.when;
		edges.push({
			id: `arc-${arc.from}-${arc.port}`,
			from: steps[arc.from].id,
			to: steps[arc.to].id,
			shape: 'arc',
			conditional: !!arc.when,
			d,
			head: `${right},${y2} ${right + 8},${y2 - 4.5} ${right + 8},${y2 + 4.5}`,
			label: full ? clip(full) : undefined,
			fullLabel: full,
			labelX: right + 8,
			labelY: y1
		});
	}

	const width = (arcs.length ? laneX(lanes.length - 1) : right) + PAD + 6;
	return {
		width,
		height: bottom + PAD,
		nodes,
		edges,
		start: { x: cx, y: PAD },
		missing
	};
}

/** One line that says what a step does, for the node's second row. */
export function summary(step: SpecStep): string {
	switch (step.kind) {
		case 'decide':
			return [step.template, step.labels?.length ? `${step.labels.length} labels` : ''].filter(Boolean).join(' · ') || 'classifier';
		case 'ai':
			return [step.prompt, step.tier].filter(Boolean).join(' · ') || 'agent';
		case 'deterministic':
			return step.handler || 'handler';
		case 'human':
			return step.assign?.role ? `role ${step.assign.role}` : step.assign?.user ? `user ${step.assign.user}` : 'approval';
		case 'external':
			return [step.connection, step.call ? `${step.call.method ?? 'POST'} ${step.call.path}` : ''].filter(Boolean).join(' · ') || 'external call';
		default:
			return '';
	}
}
