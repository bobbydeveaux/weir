import type { RunStatus, StepKind, StepStatus } from './types';

const DASH = '—';

/** Money to four decimal places: model calls cost fractions of a cent. */
export function money(v: number | null | undefined): string {
	if (v === null || v === undefined || Number.isNaN(v)) return DASH;
	return '$' + v.toLocaleString('en-US', { minimumFractionDigits: 4, maximumFractionDigits: 4 });
}

const compactFmt = new Intl.NumberFormat('en-US', { notation: 'compact', maximumFractionDigits: 1 });
const intFmt = new Intl.NumberFormat('en-US');

/** Large counts compactly: 1,234,567 becomes 1.2M. */
export function compact(v: number | null | undefined): string {
	if (v === null || v === undefined) return DASH;
	return Math.abs(v) < 10000 ? intFmt.format(v) : compactFmt.format(v);
}

export function int(v: number | null | undefined): string {
	return v === null || v === undefined ? DASH : intFmt.format(v);
}

const absFmt = new Intl.DateTimeFormat(undefined, {
	year: 'numeric',
	month: 'short',
	day: 'numeric',
	hour: '2-digit',
	minute: '2-digit',
	second: '2-digit',
	timeZoneName: 'short'
});

export function absTime(iso: string | null | undefined): string {
	if (!iso) return '';
	const d = new Date(iso);
	return Number.isNaN(d.getTime()) ? String(iso) : absFmt.format(d);
}

const clockFmt = new Intl.DateTimeFormat(undefined, { hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false });

/** Time of day, to the millisecond, for logs. */
export function clockTime(iso: string | null | undefined): string {
	if (!iso) return DASH;
	const d = new Date(iso);
	if (Number.isNaN(d.getTime())) return DASH;
	return clockFmt.format(d) + '.' + String(d.getMilliseconds()).padStart(3, '0');
}

export function relTime(iso: string | null | undefined, now: number = Date.now()): string {
	if (!iso) return DASH;
	const t = new Date(iso).getTime();
	if (Number.isNaN(t)) return DASH;
	const s = Math.round((now - t) / 1000);
	const future = s < 0;
	const a = Math.abs(s);
	let text: string;
	if (a < 5) return 'just now';
	if (a < 60) text = `${a}s`;
	else if (a < 3600) text = `${Math.floor(a / 60)}m`;
	else if (a < 86400) text = `${Math.floor(a / 3600)}h`;
	else if (a < 86400 * 30) text = `${Math.floor(a / 86400)}d`;
	else if (a < 86400 * 365) text = `${Math.floor(a / (86400 * 30))}mo`;
	else text = `${Math.floor(a / (86400 * 365))}y`;
	return future ? `in ${text}` : `${text} ago`;
}

export function duration(ms: number | null | undefined): string {
	if (ms === null || ms === undefined || Number.isNaN(ms)) return DASH;
	if (ms < 0) ms = 0;
	if (ms < 1000) return `${Math.round(ms)}ms`;
	const s = ms / 1000;
	if (s < 60) return `${s.toFixed(s < 10 ? 2 : 1)}s`;
	const m = Math.floor(s / 60);
	if (m < 60) return `${m}m ${Math.floor(s % 60)}s`;
	const h = Math.floor(m / 60);
	if (h < 24) return `${h}h ${m % 60}m`;
	return `${Math.floor(h / 24)}d ${h % 24}h`;
}

/** Milliseconds between two times; to is "now" when missing. */
export function between(from: string | null | undefined, to: string | null | undefined, now: number = Date.now()): number | null {
	if (!from) return null;
	const a = new Date(from).getTime();
	const b = to ? new Date(to).getTime() : now;
	if (Number.isNaN(a) || Number.isNaN(b)) return null;
	return Math.max(0, b - a);
}

export function shortId(id: string): string {
	return id.length > 12 ? id.slice(0, 8) : id;
}

export function isActive(status: RunStatus | string): boolean {
	return status === 'running' || status === 'waiting' || status === 'blocked' || status === 'compensating';
}

export type Tone = 'ok' | 'warn' | 'fail' | 'info' | 'idle';

export function tone(status: RunStatus | StepStatus | string): Tone {
	switch (status) {
		case 'succeeded':
		case 'done':
			return 'ok';
		case 'running':
		case 'compensating':
		case 'runnable':
			return 'info';
		case 'waiting':
		case 'blocked':
		case 'pending':
			return 'warn';
		case 'failed':
		case 'cancelled':
		case 'denied':
			return 'fail';
		default:
			return 'idle';
	}
}

export const KIND_LABEL: Record<StepKind, string> = {
	decide: 'decide',
	ai: 'ai',
	deterministic: 'deterministic',
	human: 'human',
	external: 'external'
};

export const KIND_BLURB: Record<StepKind, string> = {
	decide: 'A cheap classifier picks one label',
	ai: 'An agent with tools and a schema for its output',
	deterministic: 'Code: a registered handler',
	human: 'A person approves or rejects',
	external: 'A third-party call and its callback'
};

/** True for a plain object (not an array, not null). */
export function isRecord(v: unknown): v is Record<string, unknown> {
	return typeof v === 'object' && v !== null && !Array.isArray(v);
}

export function isEmptyJson(v: unknown): boolean {
	if (v === null || v === undefined) return true;
	if (Array.isArray(v)) return v.length === 0;
	if (isRecord(v)) return Object.keys(v).length === 0;
	return false;
}
