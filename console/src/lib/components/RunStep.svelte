<script lang="ts">
	import { between, clockTime, compact, duration, int, isEmptyJson, money } from '#lib/format.ts';
	import { clock } from '#lib/session.svelte.ts';
	import type { AiCall, RunRecord, SpecStep, StepTime, StepView, ToolCall } from '#lib/types.ts';
	import Json from './Json.svelte';
	import Status from './Status.svelte';
	import StepConfig from './StepConfig.svelte';

	// What one step did in one run: its input and output, and every model
	// call and tool call it made.
	interface Props {
		stepKey: string;
		visits: StepView[];
		spec?: SpecStep;
		steps?: SpecStep[];
		times: StepTime[];
		aiCalls: AiCall[];
		toolCalls: ToolCall[];
		decisions?: NonNullable<RunRecord['decisions']>;
		/** False while the record is still loading or could not be loaded. */
		hasRecord: boolean;
	}

	let { stepKey, visits, spec, steps = [], times, aiCalls, toolCalls, decisions = [], hasRecord }: Props = $props();

	let picked = $state<number | null>(null);

	const latest = $derived(visits.at(-1));
	const visit = $derived(visits.find((v) => v.visit === picked) ?? latest);
	const time = $derived(times.find((t) => t.visit === visit?.visit));
	const models = $derived(aiCalls.filter((c) => c.visit === visit?.visit));
	const tools = $derived(toolCalls.filter((c) => c.visit === visit?.visit));
	const decided = $derived(decisions.filter((x) => x.step === stepKey && (x.visit === undefined || x.visit === visit?.visit)));
	const finished = $derived(visit ? !['runnable', 'running', 'waiting'].includes(visit.status) : true);
	const tokens = $derived(models.reduce((n, c) => n + (c.tokens_in ?? 0) + (c.tokens_out ?? 0), 0));
</script>

{#if !visit}
	<p class="muted">This run has not reached <b class="mono">{stepKey}</b>{spec ? '' : ', and the definition could not be loaded'}.</p>
	{#if spec}
		<h2>Configuration</h2>
		<StepConfig step={spec} {steps} />
	{/if}
{:else}
	<div class="body">
		{#if visits.length > 1}
			<div class="seg" role="group" aria-label="Visit">
				{#each visits as v (v.visit)}
					<button type="button" aria-pressed={v.visit === visit.visit} onclick={() => (picked = v.visit)}>visit {v.visit}</button>
				{/each}
			</div>
		{/if}

		<dl class="facts">
			<div>
				<dt>Status</dt>
				<dd><Status status={visit.status} /></dd>
			</div>
			<div>
				<dt>Attempts</dt>
				<dd class="mono">{visit.attempts}</dd>
			</div>
			<div>
				<dt>Duration</dt>
				<dd class="mono" title={time ? `Started ${clockTime(time.started_at)}` : ''}>
					{time ? duration(between(time.started_at, time.finished_at, finished ? undefined : clock.now)) : '—'}
				</dd>
			</div>
			<div>
				<dt>Spend</dt>
				<dd class="mono">{time ? money(time.spent_usd ?? 0) : '—'}</dd>
			</div>
			{#if tokens > 0}
				<div>
					<dt>Tokens</dt>
					<dd class="mono" title={int(tokens)}>{compact(tokens)}</dd>
				</div>
			{/if}
		</dl>

		{#if visit.label}
			<div class="chose k-decide">
				<span>Label chosen</span>
				<b>{visit.label}</b>
				{#if spec?.describe?.[visit.label]}<em>{spec.describe[visit.label]}</em>{/if}
			</div>
		{/if}
		{#if visit.outcome}
			<div class="chose k-human">
				<span>Outcome</span>
				<b>{visit.outcome}</b>
			</div>
		{/if}

		{#if visit.error !== undefined && visit.error !== null}
			<section>
				<h2 class="bad">Error</h2>
				<div class="errbox"><Json value={visit.error} text={typeof visit.error === 'string'} max={220} /></div>
			</section>
		{/if}

		{#if visit.data !== undefined && visit.data !== null}
			<section>
				<h2>Decision data</h2>
				<Json value={visit.data} max={220} />
			</section>
		{/if}
		{#each decided as x, i (i)}
			<section>
				<h2>Decision</h2>
				<Json value={x.data ?? x} max={220} />
			</section>
		{/each}

		<section>
			<h2>Input</h2>
			{#if time}
				{#if isEmptyJson(time.input)}<p class="muted">Empty.</p>{:else}<Json value={time.input} max={240} />{/if}
			{:else}
				<p class="muted">{hasRecord ? 'Not recorded.' : 'Loading the record…'}</p>
			{/if}
		</section>

		<section>
			<h2>Output</h2>
			{#if visit.output !== undefined && visit.output !== null}
				<Json value={visit.output} max={280} />
			{:else}
				<p class="muted">{finished ? 'None.' : 'Not yet.'}</p>
			{/if}
		</section>

		<section>
			<h2>Model calls <span class="n">{models.length}</span></h2>
			{#if models.length === 0}
				<p class="muted">{hasRecord ? 'This step called no model.' : 'Loading the record…'}</p>
			{/if}
			{#each models as c, i (i)}
				<article class="call" class:failed={!!c.error}>
					<header>
						<span class="who mono">
							{#if c.provider}<span class="muted">{c.provider}/</span>{/if}{c.model ?? 'unknown model'}
						</span>
						<span class="mono muted" title={clockTime(c.at)}>{duration(c.duration_ms)}</span>
					</header>
					<div class="tags">
						{#if c.template}<span class="tag mono" title="Template and version">{c.template}@{c.template_version ?? '?'}</span>{/if}
						{#if c.runner}<span class="tag" title="Runner">{c.runner}</span>{/if}
						<span class="tag" title="Attempt">attempt {c.attempt}</span>
						{#if c.prompt_hash}<span class="tag mono" title="Prompt hash: {c.prompt_hash}">#{c.prompt_hash.slice(0, 8)}</span>{/if}
					</div>
					<dl class="nums">
						<div>
							<dt>Tokens in</dt>
							<dd title={int(c.tokens_in)}>{compact(c.tokens_in)}</dd>
						</div>
						<div>
							<dt>Tokens out</dt>
							<dd title={int(c.tokens_out)}>{compact(c.tokens_out)}</dd>
						</div>
						<div>
							<dt>Cost</dt>
							<dd>
								{money(c.cost_usd)}{#if c.cost_estimated}<span class="est" title="Estimated: the provider did not report an exact cost">estimated</span>{/if}
							</dd>
						</div>
					</dl>
					{#if c.error}<p class="form-error mono">{c.error}</p>{/if}
					{#if c.prompt}
						<details>
							<summary>Prompt</summary>
							<Json value={c.prompt} text max={320} />
						</details>
					{/if}
					{#if c.response}
						<details>
							<summary>Response</summary>
							<Json value={c.response} text max={320} />
						</details>
					{/if}
					{#if c.data !== undefined && c.data !== null}
						<details>
							<summary>Data</summary>
							<Json value={c.data} max={240} />
						</details>
					{/if}
				</article>
			{/each}
		</section>

		<section>
			<h2>Tool calls <span class="n">{tools.length}</span></h2>
			{#if tools.length === 0}
				<p class="muted">{hasRecord ? 'This step called no tool.' : 'Loading the record…'}</p>
			{/if}
			{#each tools as c, i (i)}
				<article class="call" class:failed={c.status !== 'succeeded'}>
					<header>
						<span class="who mono">{c.tool}</span>
						<span class="mono muted" title={clockTime(c.at)}>{duration(c.duration_ms)}</span>
					</header>
					<div class="tags">
						<span class="tag" class:write={c.class !== 'read'} title="Tool class">{c.class}</span>
						<Status status={c.status} small />
					</div>
					{#if c.error}<p class="form-error mono">{c.error}</p>{/if}
					<details open={!isEmptyJson(c.args)}>
						<summary>Arguments</summary>
						<Json value={c.args} max={200} />
					</details>
					{#if c.result !== null && c.result !== undefined}
						<details>
							<summary>Result</summary>
							<Json value={c.result} max={260} />
						</details>
					{/if}
				</article>
			{/each}
		</section>

		{#if spec}
			<details class="cfg">
				<summary>Configuration of this step</summary>
				<StepConfig step={spec} {steps} />
			</details>
		{/if}
	</div>
{/if}

<style>
	.body {
		display: flex;
		flex-direction: column;
		gap: 14px;
	}

	section {
		display: flex;
		flex-direction: column;
		gap: 7px;
		min-width: 0;
	}

	h2 .n {
		color: var(--faint);
		margin-left: 4px;
	}

	h2.bad {
		color: var(--fail);
	}

	.facts,
	.nums {
		display: flex;
		flex-wrap: wrap;
		gap: 8px 22px;
		margin: 0;
	}

	.facts dt,
	.nums dt {
		font-size: 10.5px;
		font-weight: 700;
		letter-spacing: 0.08em;
		text-transform: uppercase;
		color: var(--muted);
		margin-bottom: 2px;
	}

	.facts dd,
	.nums dd {
		margin: 0;
		font-weight: 650;
		font-variant-numeric: tabular-nums;
	}

	.nums dd {
		font-family: var(--mono);
		font-size: 12.5px;
	}

	.chose {
		display: flex;
		align-items: baseline;
		flex-wrap: wrap;
		gap: 8px;
		padding: 9px 12px;
		border-radius: var(--radius-s);
		border: 1px solid color-mix(in srgb, var(--k) 40%, var(--line));
		background: color-mix(in srgb, var(--k) 10%, transparent);
	}

	.chose span {
		font-size: 11px;
		font-weight: 700;
		letter-spacing: 0.08em;
		text-transform: uppercase;
		color: var(--muted);
	}

	.chose b {
		font: 700 14px var(--mono);
		color: var(--k);
	}

	.chose em {
		font-style: normal;
		color: var(--muted);
	}

	.errbox :global(pre) {
		border-color: color-mix(in srgb, var(--fail) 45%, var(--line));
		background: color-mix(in srgb, var(--fail) 8%, var(--surface));
	}

	.call {
		display: flex;
		flex-direction: column;
		gap: 8px;
		padding: 10px 12px;
		border: 1px solid var(--line);
		border-radius: var(--radius-s);
		background: color-mix(in srgb, var(--surface-2) 45%, transparent);
		min-width: 0;
	}

	.call.failed {
		border-color: color-mix(in srgb, var(--fail) 45%, var(--line));
	}

	.call header {
		display: flex;
		justify-content: space-between;
		align-items: baseline;
		gap: 10px;
	}

	.who {
		font-weight: 700;
		font-size: 13px;
		overflow-wrap: anywhere;
	}

	.tags {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 5px;
	}

	.tag.write {
		color: var(--warn);
		border-color: color-mix(in srgb, var(--warn) 45%, var(--line));
	}

	.est {
		margin-left: 6px;
		font: 600 10.5px var(--sans);
		color: var(--warn);
	}

	details summary {
		cursor: pointer;
		font-size: 12px;
		font-weight: 650;
		color: var(--muted);
		padding: 2px 0;
		user-select: none;
	}

	details[open] summary {
		margin-bottom: 6px;
	}

	.cfg {
		border-top: 1px solid var(--line);
		padding-top: 12px;
	}
</style>
