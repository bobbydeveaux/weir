<script lang="ts">
	// A tiny YAML colouriser, the same idea as the one on the brochure site,
	// with line numbers and a tint for each step kind.
	let { code }: { code: string } = $props();

	interface Part {
		text: string;
		cls: string;
	}

	const KINDS = ['decide', 'ai', 'deterministic', 'human', 'external'];

	function colour(line: string): Part[] {
		if (/^\s*#/.test(line)) return [{ text: line, cls: 'comment' }];
		const hash = line.search(/\s#/);
		const body = hash >= 0 ? line.slice(0, hash) : line;
		const comment = hash >= 0 ? line.slice(hash) : '';
		const parts: Part[] = [];
		const m = body.match(/^(\s*(?:- )?)([A-Za-z_][\w.-]*)(:)(.*)$/);
		if (m) {
			const value = m[4].trim();
			const cls = m[2] === 'kind' && KINDS.includes(value) ? `kind k-${value}` : 'val';
			parts.push({ text: m[1], cls: 'punct' }, { text: m[2], cls: 'key' }, { text: m[3], cls: 'punct' }, { text: m[4], cls });
		} else {
			parts.push({ text: body, cls: 'val' });
		}
		if (comment) parts.push({ text: comment, cls: 'comment' });
		return parts;
	}

	const lines = $derived(code.replace(/\n$/, '').split('\n').map(colour));
</script>

<pre><code>{#each lines as parts, i (i)}<span class="line"><span class="no">{i + 1}</span>{#each parts as part, j (j)}<span class={part.cls}>{part.text}</span>{/each}{'\n'}</span>{/each}</code></pre>

<style>
	pre {
		margin: 0;
		padding: 14px 0;
		overflow-x: auto;
		font: 12.5px/1.7 var(--mono);
		tab-size: 2;
	}

	.line {
		display: block;
		padding-right: 18px;
	}

	.line:hover {
		background: color-mix(in srgb, var(--surface-2) 70%, transparent);
	}

	.no {
		display: inline-block;
		width: 46px;
		padding-right: 14px;
		text-align: right;
		color: var(--faint);
		user-select: none;
	}

	.key {
		color: var(--code-key);
	}

	.punct {
		color: var(--faint);
	}

	.val {
		color: var(--code-str);
	}

	.kind {
		color: var(--k);
		font-weight: 700;
	}

	.comment {
		color: var(--code-comment);
		font-style: italic;
	}
</style>
