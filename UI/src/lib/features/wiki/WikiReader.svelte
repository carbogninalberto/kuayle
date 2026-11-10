<script lang="ts">
	import type { Snippet } from 'svelte';
	import { Button } from '$lib/components/ui/button/index.js';
	import { sanitizeHtml } from '$lib/security/sanitize';
	import type { ReaderPage } from '$lib/types/wiki';
	import { m, getLocale } from '$lib/features/i18n/messages';
	import { FileText } from '@lucide/svelte';
	import { buildTree, type TreeNode } from './wiki-tree';
	import { wikiOutline } from './wiki-content';

	// A published page read-only: its pages on the left when several are
	// published, the text in the middle and its outline on the right. Used by
	// public links and by the internal reader; no editor chrome.
	let {
		pages,
		selectedId,
		hrefFor,
		header
	}: {
		pages: ReaderPage[];
		selectedId: string;
		hrefFor: (id: string) => string;
		header?: Snippet;
	} = $props();

	type Item = ReaderPage & { kind: 'page' };
	const tree = $derived(buildTree(pages.map((p): Item => ({ ...p, kind: 'page' }))));
	const current = $derived(pages.find((p) => p.id === selectedId) ?? pages[0]);
	const outline = $derived(wikiOutline(current?.content));
	let article = $state<HTMLElement | null>(null);

	function jumpTo(index: number) {
		article?.querySelectorAll('h1, h2, h3, h4, h5, h6')[index]?.scrollIntoView({ behavior: 'smooth', block: 'start' });
	}
</script>

{#snippet branch(nodes: TreeNode<Item>[])}
	<ul class="grid gap-px">
		{#each nodes as node (node.item.id)}
			<li>
				<a
					href={hrefFor(node.item.id)}
					aria-current={node.item.id === current?.id ? 'page' : undefined}
					class="flex items-center gap-2 rounded-md py-1.5 pr-2 text-sm {node.item.id === current?.id
						? 'bg-[var(--color-bg-hover)] text-[var(--color-text-primary)]'
						: 'text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]'}"
					style="padding-left: {0.5 + node.depth * 0.75}rem"
				>
					<FileText size={14} class="shrink-0 text-[var(--color-text-tertiary)]" />
					<span class="truncate">{node.item.title}</span>
				</a>
				{#if node.children.length > 0}{@render branch(node.children)}{/if}
			</li>
		{/each}
	</ul>
{/snippet}

<div class="flex min-h-0 flex-1 flex-col lg:flex-row">
	{#if pages.length > 1}
		<nav
			aria-label={m['wiki.pages']()}
			class="shrink-0 border-b border-[var(--app-border)] p-2 lg:w-60 lg:overflow-y-auto lg:border-r lg:border-b-0"
		>
			{@render branch(tree)}
		</nav>
	{/if}
	<main class="min-w-0 flex-1 lg:overflow-y-auto">
		{#if current}
			<article bind:this={article} class="mx-auto max-w-[760px] px-4 py-6 sm:px-10 sm:py-8">
				{@render header?.()}
				<h1 class="text-2xl font-semibold break-words text-[var(--color-text-primary)]">{current.title}</h1>
				<p class="mt-1 text-xs text-[var(--color-text-tertiary)]">
					{m['wiki.reader.updated']({
						date: new Date(current.updated_at).toLocaleDateString(getLocale(), { dateStyle: 'medium' })
					})}
				</p>
				<div
					class="prose prose-sm mt-6 max-w-none break-words text-[var(--color-text-primary)] dark:prose-invert [&_img]:max-w-full [&_pre]:whitespace-pre-wrap"
				>
					{@html sanitizeHtml(current.content).replace(/<input\b/g, '<input disabled')}
				</div>
			</article>
		{/if}
	</main>
	{#if outline.length > 0}
		<aside
			aria-label={m['wiki.outline']()}
			class="hidden w-60 shrink-0 overflow-y-auto border-l border-[var(--app-border)] p-3 xl:block"
		>
			<h2 class="px-2 pb-1 text-[11px] font-medium tracking-wider text-[var(--color-text-tertiary)] uppercase">
				{m['wiki.outline']()}
			</h2>
			<ul>
				{#each outline as item (item.index)}
					<li>
						<Button
							variant="plain"
							onclick={() => jumpTo(item.index)}
							class="block w-full truncate rounded-md px-2 py-1 text-left text-sm text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]"
							style="padding-left: {0.5 + (item.level - 1) * 0.75}rem">{item.text}</Button
						>
					</li>
				{/each}
			</ul>
		</aside>
	{/if}
</div>
