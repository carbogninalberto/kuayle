<script lang="ts">
	import { SvelteSet } from 'svelte/reactivity';
	import { renderMarkdown } from '$lib/markdown';
	import { m } from '$lib/features/i18n/messages';
	import ChatChart from './ChatChart.svelte';
	import ChatHtmlWidget from './ChatHtmlWidget.svelte';
	import { richSegments } from './rich-blocks';

	// Model and agent text is untrusted: renderMarkdown sanitizes it and images
	// are not loaded. Rich fences are opt-in for the assistant chat, where a
	// chart is drawn from validated JSON and an html widget stays sandboxed.
	let {
		source,
		class: className = '',
		richBlocks = false
	}: { source: string; class?: string; richBlocks?: boolean } = $props();

	const failed = new SvelteSet<number>();
	const segments = $derived(richBlocks ? richSegments(source) : []);

	$effect(() => {
		source;
		richBlocks;
		failed.clear();
	});
</script>

{#if richBlocks}
	<div class="assistant-markdown min-w-0 break-words {className}">
		{#each segments as segment, index (index)}
			{#if segment.kind === 'markdown'}
				{@html renderMarkdown(segment.source, undefined, { allowImages: false })}
			{:else if failed.has(index)}
				<div class="my-3">
					<p class="mb-1.5 text-xs text-muted-foreground"
						>{segment.kind === 'chart'
							? m['assistant.rich_chart_failed']()
							: m['assistant.rich_widget_failed']()}</p
					>
					{@html renderMarkdown(segment.fence, undefined, { allowImages: false })}
				</div>
			{:else if segment.kind === 'chart'}
				<div class="my-3">
					<svelte:boundary onerror={() => failed.add(index)}>
						<ChatChart spec={segment.spec} onfail={() => failed.add(index)} />
					</svelte:boundary>
				</div>
			{:else}
				<div class="my-3">
					<svelte:boundary onerror={() => failed.add(index)}>
						<ChatHtmlWidget html={segment.html} onfail={() => failed.add(index)} />
					</svelte:boundary>
				</div>
			{/if}
		{/each}
	</div>
{:else}
	<div class="assistant-markdown min-w-0 break-words {className}">
		{@html renderMarkdown(source, undefined, { allowImages: false })}
	</div>
{/if}

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
		max-width: 100%;
		overflow-x: auto;
	}
	.assistant-markdown :global(th),
	.assistant-markdown :global(td) {
		padding: 0.4rem;
		border: 1px solid var(--border);
	}
</style>
