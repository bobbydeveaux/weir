<script lang="ts">
	// Pretty-printed JSON with light colouring. A string value may be passed
	// with text={true} to show it as plain text instead.
	let { value, text = false, max = 360 }: { value: unknown; text?: boolean; max?: number } = $props();

	interface Part {
		text: string;
		cls: string;
	}

	const TOKEN = /("(?:\\.|[^"\\])*")(\s*:)?|\b(true|false|null)\b|-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?/g;

	function colour(source: string): Part[] {
		const parts: Part[] = [];
		let last = 0;
		for (const m of source.matchAll(TOKEN)) {
			const at = m.index ?? 0;
			if (at > last) parts.push({ text: source.slice(last, at), cls: 'punct' });
			if (m[1] && m[2]) parts.push({ text: m[1], cls: 'key' }, { text: m[2], cls: 'punct' });
			else if (m[1]) parts.push({ text: m[1], cls: 'str' });
			else if (m[3]) parts.push({ text: m[0], cls: 'lit' });
			else parts.push({ text: m[0], cls: 'num' });
			last = at + m[0].length;
		}
		if (last < source.length) parts.push({ text: source.slice(last), cls: 'punct' });
		return parts;
	}

	const parts = $derived.by<Part[]>(() => {
		if (value === undefined) return [{ text: '—', cls: 'punct' }];
		if (text && typeof value === 'string') return [{ text: value, cls: 'plain' }];
		let source: string;
		try {
			source = JSON.stringify(value, null, 2) ?? String(value);
		} catch {
			source = String(value);
		}
		return colour(source);
	});
</script>

<pre style:max-height="{max}px" class:wrap={text}><code>{#each parts as part, i (i)}<span class={part.cls}>{part.text}</span>{/each}</code></pre>

<style>
	pre {
		margin: 0;
		padding: 10px 12px;
		overflow: auto;
		font: 12.5px/1.6 var(--mono);
		background: var(--surface-2);
		border: 1px solid var(--line);
		border-radius: var(--radius-s);
		tab-size: 2;
	}

	.wrap {
		white-space: pre-wrap;
		overflow-wrap: anywhere;
	}

	.key {
		color: var(--code-key);
	}

	.str {
		color: var(--code-str);
	}

	.num {
		color: var(--code-num);
	}

	.lit {
		color: var(--code-lit);
	}

	.punct {
		color: var(--muted);
	}

	.plain {
		color: var(--text);
	}
</style>
