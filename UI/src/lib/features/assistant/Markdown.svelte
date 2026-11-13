<script lang="ts">
	import { SvelteSet } from 'svelte/reactivity';
	import { renderMarkdown } from '$lib/markdown';
	import { m } from '$lib/features/i18n/messages';
	import ChatHtmlWidget from './ChatHtmlWidget.svelte';
	import { richSegments, type RichSegment } from './rich-blocks';

	// ChatChart pulls in all of echarts. Most Markdown callers never render a
	// chart, so the module is fetched only when a chart segment appears.
	let chartLoadFailed = $state(false);
	let chartModule: Promise<typeof import('./ChatChart.svelte')> | undefined;
	const loadChart = () =>
		(chartModule ??= import('./ChatChart.svelte').catch((error: unknown) => {
			chartLoadFailed = true;
			throw error;
		}));

	// Model and agent text is untrusted: renderMarkdown sanitizes it and images
	// are not loaded. Rich fences are opt-in for the assistant chat, where a
	// chart is drawn from validated JSON and an html widget stays sandboxed.
	let {
		source,
		class: className = '',
		richBlocks = false
	}: { source: string; class?: string; richBlocks?: boolean } = $props();

	const failed = new SvelteSet<string>();
	const segments = $derived(richBlocks ? richSegments(source) : []);

	// Key by content so changed text replaces a widget or chart instead of
	// reusing it with a stale srcdoc/spec; the index keeps equal fences unique.
	const keyOf = (segment: RichSegment, index: number) =>
		`${index}:${segment.kind}:${segment.kind === 'markdown' ? segment.source : segment.fence}`;

	$effect(() => {
		source;
		richBlocks;
		failed.clear();
	});
</script>

{#if richBlocks}
	<div class="assistant-markdown min-w-0 break-words {className}">
		<!-- Each Markdown segment renders on its own, so reference-style links and
		     footnotes defined on the other side of a rich fence do not resolve. -->
		{#each segments as segment, index (keyOf(segment, index))}
			{#if segment.kind === 'markdown'}
				{@html renderMarkdown(segment.source, undefined, { allowImages: false })}
			{:else if failed.has(keyOf(segment, index)) || (segment.kind === 'chart' && chartLoadFailed)}
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
					<svelte:boundary onerror={() => failed.add(keyOf(segment, index))}>
						{#await loadChart()}
							<!-- Same height as ChatChart, so the lazy import does not shift the thread. -->
							<div class="h-64 w-full"></div>
						{:then { default: Chart }}
							<Chart spec={segment.spec} onfail={() => failed.add(keyOf(segment, index))} />
						{:catch}
							<!-- chartLoadFailed already switched this segment to the code fallback. -->
							<div class="h-64 w-full"></div>
						{/await}
					</svelte:boundary>
				</div>
			{:else}
				<div class="my-3">
					<svelte:boundary onerror={() => failed.add(keyOf(segment, index))}>
						<ChatHtmlWidget html={segment.html} onfail={() => failed.add(keyOf(segment, index))} />
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
