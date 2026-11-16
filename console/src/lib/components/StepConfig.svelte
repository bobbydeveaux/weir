<script lang="ts">
	import { KIND_BLURB, isEmptyJson, money } from '#lib/format.ts';
	import type { SpecStep } from '#lib/types.ts';
	import Json from './Json.svelte';
	import KindChip from './KindChip.svelte';

	// One step's configuration, as written in the definition, laid out to be read.
	let { step, steps = [] }: { step: SpecStep; steps?: SpecStep[] } = $props();

	const index = $derived(steps.findIndex((s) => s.id === step.id));
	const hasDefaultRule = $derived((step.next ?? []).some((n) => !n.when));
	const fallthrough = $derived.by(() => {
		if (hasDefaultRule) return null;
		if (step.end || index === steps.length - 1 || index < 0) return 'end the run';
		return steps[index + 1].id;
	});
	const inputs = $derived(Object.entries(step.input ?? {}));
	const toolGroups = $derived([
		{ name: 'Read', list: step.tools?.read ?? [] },
		{ name: 'Write', list: step.tools?.write ?? [] },
		{ name: 'Approve', list: step.tools?.approve ?? [] }
	]);
	const show = (v: unknown) => (typeof v === 'string' ? v : JSON.stringify(v));
</script>

<div class="config k-{step.kind}">
	<p class="blurb">{KIND_BLURB[step.kind] ?? ''}</p>

	{#if step.kind === 'decide'}
		<section>
			<h2>Classifier</h2>
			<dl class="kv">
				<dt>Template</dt>
				<dd class="mono">{step.template ?? '—'}</dd>
				<dt>Provider</dt>
				<dd>
					{#if step.provider}<span class="mono">{step.provider}</span>{:else}<span class="muted">server default</span>{/if}
				</dd>
				{#if step.tier}
					<dt>Tier</dt>
					<dd class="mono">{step.tier}</dd>
				{/if}
				<dt>Confidence</dt>
				<dd>
					{#if step.confidence}
						<span class="mono">≥ {step.confidence}</span>
						<span class="muted">· below this, the default applies</span>
					{:else}
						<span class="muted">no threshold</span>
					{/if}
				</dd>
				<dt>Default</dt>
				<dd>
					{#if step.default}<span class="label is-default">{step.default}</span>{:else}<span class="muted">none</span>{/if}
				</dd>
			</dl>
		</section>
		<section>
			<h2>Labels <span class="n">{step.labels?.length ?? 0}</span></h2>
			<ul class="labels">
				{#each step.labels ?? [] as l (l)}
					<li>
						<span class="label" class:is-default={l === step.default}>{l}</span>
						<span class="describe">
							{step.describe?.[l] ?? ''}
							{#if l === step.default}<em>default</em>{/if}
						</span>
					</li>
				{/each}
			</ul>
		</section>
	{:else if step.kind === 'ai'}
		<section>
			<h2>Agent</h2>
			<dl class="kv">
				<dt>Prompt</dt>
				<dd class="mono">{step.prompt ?? '—'}</dd>
				<dt>Tier</dt>
				<dd>
					{#if step.tier}<span class="mono">{step.tier}</span>{:else}<span class="muted">default</span>{/if}
				</dd>
				<dt>Budget</dt>
				<dd>
					{#if step.budget?.usd || step.budget?.minutes}
						{#if step.budget.usd}<span class="mono">{money(step.budget.usd)}</span>{/if}
						{#if step.budget.minutes}<span class="mono">{step.budget.minutes} min</span>{/if}
					{:else}
						<span class="muted">the run's budget</span>
					{/if}
				</dd>
			</dl>
		</section>
		<section>
			<h2>Tools</h2>
			{#if step.tools?.read?.length || step.tools?.write?.length || step.tools?.approve?.length}
				<dl class="kv">
					{#each toolGroups as group (group.name)}
						{#if group.list.length}
							<dt>{group.name}</dt>
							<dd class="chips">
								{#each group.list as t (t)}<span class="tool" class:write={group.name !== 'Read'}>{t}</span>{/each}
							</dd>
						{/if}
					{/each}
				</dl>
			{:else}
				<p class="muted">No tools: the agent only reads its input and writes its output.</p>
			{/if}
		</section>
	{:else if step.kind === 'deterministic'}
		<section>
			<h2>Code</h2>
			<dl class="kv">
				<dt>Handler</dt>
				<dd class="mono">{step.handler ?? '—'}</dd>
				{#if step.compensate}
					<dt>Compensate</dt>
					<dd><span class="mono">{step.compensate}</span> <span class="muted">· runs to undo this step</span></dd>
				{/if}
			</dl>
		</section>
	{:else if step.kind === 'human'}
		<section>
			<h2>Who decides</h2>
			<dl class="kv">
				{#if step.assign?.role}
					<dt>Role</dt>
					<dd class="mono">{step.assign.role}</dd>
				{/if}
				{#if step.assign?.user}
					<dt>User</dt>
					<dd class="mono">{step.assign.user}</dd>
				{/if}
				{#if step.assign?.not}
					<dt>Not</dt>
					<dd><span class="mono">{step.assign.not}</span> <span class="muted">· may not decide</span></dd>
				{/if}
				{#if !step.assign?.role && !step.assign?.user}
					<dt>Assign</dt>
					<dd class="muted">anyone</dd>
				{/if}
				<dt>Outcomes</dt>
				<dd class="chips">
					{#each step.outcomes ?? [] as o (o)}<span class="label">{o}</span>{:else}<span class="muted">the engine's defaults</span>{/each}
				</dd>
			</dl>
		</section>
		<section>
			<h2>Escalation</h2>
			{#if step.escalate?.length}
				<ol class="rules">
					{#each step.escalate as e, i (i)}
						<li>after <span class="mono">{e.after}</span> → role <span class="mono">{e.role}</span></li>
					{/each}
				</ol>
			{:else}
				<p class="muted">Never escalates.</p>
			{/if}
		</section>
	{:else if step.kind === 'external'}
		<section>
			<h2>Third party</h2>
			<dl class="kv">
				<dt>Connection</dt>
				<dd class="mono">{step.connection ?? '—'}</dd>
				<dt>Call</dt>
				<dd class="mono">
					{#if step.call}<span class="method">{step.call.method ?? 'POST'}</span> {step.call.path}{:else}—{/if}
				</dd>
				<dt>Resume</dt>
				<dd>
					{#if step.resume}<span class="mono">{step.resume}</span>{:else}<span class="muted">not set</span>{/if}
				</dd>
				{#if step.poll}
					<dt>Poll</dt>
					<dd>
						{#if step.poll.every}every <span class="mono">{step.poll.every}</span>{/if}
						until <span class="mono">{step.poll.until}</span>
					</dd>
				{/if}
			</dl>
			{#if step.call?.body && !isEmptyJson(step.call.body)}
				<h3 class="sub">Request body</h3>
				<Json value={step.call.body} max={220} />
			{/if}
		</section>
	{/if}

	<section>
		<h2>Input mapping</h2>
		{#if inputs.length}
			<dl class="kv map">
				{#each inputs as [key, expr] (key)}
					<dt class="mono">{key}</dt>
					<dd class="mono"><span class="arrow">←</span> {show(expr)}</dd>
				{/each}
			</dl>
		{:else}
			<p class="muted">None: the step is given the run's input.</p>
		{/if}
	</section>

	{#if step.output?.schema}
		<section>
			<h2>Output schema</h2>
			<Json value={step.output.schema} max={260} />
		</section>
	{/if}

	<section>
		<h2>Then</h2>
		<ol class="rules">
			{#each step.next ?? [] as rule, i (i)}
				<li>
					{#if rule.when}when <span class="mono cond">{rule.when}</span>{:else}always{/if}
					→ <KindChip kind={steps.find((s) => s.id === rule.go)?.kind ?? 'unknown'} label={rule.go} />
				</li>
			{/each}
			{#if fallthrough}
				<li>
					{#if step.next?.length}otherwise{:else}always{/if}
					→
					{#if fallthrough === 'end the run'}
						<span class="muted">end the run{step.end ? ' (end: true)' : ''}</span>
					{:else}
						<KindChip kind={steps[index + 1].kind} label={fallthrough} />
					{/if}
				</li>
			{/if}
		</ol>
	</section>

	<section>
		<h2>Reliability</h2>
		<dl class="kv">
			<dt>Retry</dt>
			<dd>
				{#if step.retry?.attempts || step.retry?.backoff}
					{#if step.retry.attempts}<span class="mono">{step.retry.attempts}</span> attempts{/if}
					{#if step.retry.backoff}· backoff <span class="mono">{step.retry.backoff}</span>{/if}
				{:else}
					<span class="muted">engine default</span>
				{/if}
			</dd>
			<dt>Timeout</dt>
			<dd>
				{#if step.timeout}<span class="mono">{step.timeout}</span>{:else}<span class="muted">none</span>{/if}
			</dd>
			<dt>On failure</dt>
			<dd>
				{#if step.on_failure}<span class="mono">{step.on_failure}</span>{:else}<span class="muted">block (default)</span>{/if}
			</dd>
			{#if step.compensate && step.kind !== 'deterministic'}
				<dt>Compensate</dt>
				<dd class="mono">{step.compensate}</dd>
			{/if}
		</dl>
	</section>

	{#if step.hooks !== undefined || step.sandbox !== undefined}
		<section>
			<h2>Accepted, not yet enforced</h2>
			<Json value={{ ...(step.hooks !== undefined ? { hooks: step.hooks } : {}), ...(step.sandbox !== undefined ? { sandbox: step.sandbox } : {}) }} max={200} />
		</section>
	{/if}
</div>

<style>
	.config {
		display: flex;
		flex-direction: column;
		gap: 16px;
	}

	.blurb {
		color: var(--muted);
	}

	section {
		display: flex;
		flex-direction: column;
		gap: 8px;
	}

	h2 .n {
		color: var(--faint);
		margin-left: 4px;
	}

	.sub {
		font-size: 12px;
		color: var(--muted);
		margin-top: 4px;
	}

	.labels {
		list-style: none;
		margin: 0;
		padding: 0;
		display: grid;
		gap: 6px;
	}

	.labels li {
		display: grid;
		grid-template-columns: minmax(70px, max-content) minmax(0, 1fr);
		gap: 10px;
		align-items: baseline;
	}

	.label {
		display: inline-block;
		font: 650 11.5px/1.6 var(--mono);
		padding: 0 7px;
		border-radius: 5px;
		color: var(--k);
		background: color-mix(in srgb, var(--k) 13%, transparent);
		border: 1px solid color-mix(in srgb, var(--k) 34%, transparent);
		justify-self: start;
	}

	.label.is-default {
		border-style: dashed;
	}

	.describe {
		color: var(--muted);
	}

	.describe em {
		font-style: normal;
		font-size: 11px;
		font-weight: 650;
		color: var(--faint);
		margin-left: 4px;
		text-transform: uppercase;
		letter-spacing: 0.06em;
	}

	.chips {
		display: flex;
		flex-wrap: wrap;
		gap: 5px;
	}

	.tool {
		font: 600 11.5px/1.6 var(--mono);
		padding: 0 7px;
		border-radius: 5px;
		border: 1px solid var(--line-strong);
		color: var(--text);
	}

	.tool.write {
		color: var(--warn);
		border-color: color-mix(in srgb, var(--warn) 45%, var(--line));
	}

	.method {
		color: var(--k);
		font-weight: 700;
	}

	.map dt {
		color: var(--text);
		font-weight: 650;
	}

	.arrow {
		color: var(--faint);
	}

	.rules {
		margin: 0;
		padding: 0;
		list-style: none;
		display: grid;
		gap: 6px;
	}

	.rules li {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 6px;
	}

	.cond {
		padding: 0 6px;
		border-radius: 4px;
		background: var(--surface-2);
		border: 1px solid var(--line);
	}
</style>
