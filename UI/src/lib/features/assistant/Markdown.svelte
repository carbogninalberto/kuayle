<script lang="ts">
	import { renderMarkdown } from '$lib/markdown';
	// Model and agent text is untrusted: renderMarkdown sanitizes it and images
	// are not loaded.
	let { source, class: className = '' }: { source: string; class?: string } = $props();
</script>

<div class="assistant-markdown min-w-0 break-words {className}">
	{@html renderMarkdown(source, undefined, { allowImages: false })}
</div>

<style>
	.assistant-markdown :global(p) {
		margin-block: 0.4rem;
		line-height: 1.75;
	}
	.assistant-markdown :global(pre) {
		max-width: 100%;
		overflow-x: auto;
		border: 1px solid var(--border);
		border-radius: 0.5rem;
		background: var(--muted);
		padding: 1rem;
		margin-block: 0.75rem;
	}
	.assistant-markdown :global(code) {
		font-size: 0.85em;
	}
	.assistant-markdown :global(a) {
		text-decoration: underline;
		text-underline-offset: 3px;
	}
	.assistant-markdown :global(ul),
	.assistant-markdown :global(ol) {
		padding-left: 1.5rem;
		margin-block: 0.5rem;
	}
	.assistant-markdown :global(ul) {
		list-style: disc;
	}
	.assistant-markdown :global(ol) {
		list-style: decimal;
	}
	.assistant-markdown :global(h1),
	.assistant-markdown :global(h2),
	.assistant-markdown :global(h3) {
		font-weight: 600;
		margin-block: 1rem 0.5rem;
	}
	.assistant-markdown :global(table) {
		display: block;
		width: max-content;
		max-width: 100%;
		overflow-x: auto;
		border-collapse: collapse;
		margin-block: 0.75rem;
		font-size: 0.9em;
	}
	.assistant-markdown :global(th),
	.assistant-markdown :global(td) {
		min-width: 6rem;
		padding: 0.4rem 0.6rem;
		border: 1px solid var(--border);
		text-align: left;
		vertical-align: top;
		white-space: normal;
		overflow-wrap: normal;
	}
	.assistant-markdown :global(th) {
		font-weight: 600;
		background: var(--muted);
	}
	.assistant-markdown :global(th[align='left']),
	.assistant-markdown :global(td[align='left']) {
		text-align: left;
	}
	.assistant-markdown :global(th[align='center']),
	.assistant-markdown :global(td[align='center']) {
		text-align: center;
	}
	.assistant-markdown :global(th[align='right']),
	.assistant-markdown :global(td[align='right']) {
		text-align: right;
	}
</style>
