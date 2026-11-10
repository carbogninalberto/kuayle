<script lang="ts">
	import { page as appPage } from '$app/state';
	import EmptyState from '$lib/components/shared/EmptyState.svelte';
	import { Skeleton } from '$lib/components/ui/skeleton/index.js';
	import { getWikiPage, getWikiPublication, getWikiTree, listWikiPages } from '$lib/api/wiki';
	import type { ReaderPage, WikiScope } from '$lib/types/wiki';
	import { m } from '$lib/features/i18n/messages';
	import WikiReader from './WikiReader.svelte';
	import { subtreeIds } from './wiki-tree';
	import { wikiMentionIds } from './wiki-references';
	import { wikiReferences } from './wiki-reference-store';

	// The internal reader of a published page, for members who can see the
	// wiki: the same read-only layout as a public link. Access is the wiki's
	// own; publishing only offers this cleaner view and its link.
	let { slug, scope, rootId, editHref }: { slug: string; scope: WikiScope; rootId: string; editHref: string } =
		$props();

	let pages = $state<ReaderPage[]>([]);
	let readerState = $state<'loading' | 'ready' | 'unpublished' | 'failed'>('loading');
	const selected = $derived(appPage.url.searchParams.get('page') ?? rootId);
	// Titles of the mentioned pages this member can see, for the reader to
	// print; pages they cannot see stay placeholders.
	let references = $state(new Map<string, string>());

	$effect(() => {
		const root = rootId;
		readerState = 'loading';
		void (async () => {
			try {
				const [publication, summaries, tree] = await Promise.all([
					getWikiPublication(slug, scope, root),
					listWikiPages(slug, scope),
					getWikiTree(slug, scope)
				]);
				if (publication.mode === 'off') {
					readerState = 'unpublished';
					return;
				}
				const included = publication.children ? subtreeIds(tree, root) : new Set([root]);
				const placed = new Map(tree.map((node) => [node.id, node]));
				pages = summaries
					.filter((summary) => included.has(summary.id))
					.map((summary) => ({
						id: summary.id,
						parent_id: summary.id === root ? null : (placed.get(summary.id)?.parent_id ?? null),
						title: summary.title,
						content: '',
						position: placed.get(summary.id)?.position ?? 0,
						updated_at: summary.updated_at
					}));
				readerState = 'ready';
			} catch {
				readerState = 'failed';
			}
		})();
	});

	// Page text loads when a page is opened.
	$effect(() => {
		const id = selected;
		if (readerState !== 'ready' || !pages.some((p) => p.id === id) || pages.find((p) => p.id === id)?.content) return;
		void getWikiPage(slug, scope, id)
			.then((loaded) => {
				pages = pages.map((p) => (p.id === id ? { ...p, content: loaded.content, title: loaded.title } : p));
				for (const mentioned of wikiMentionIds(loaded.content))
					void wikiReferences.resolve(slug, mentioned).then((reference) => {
						if (reference.accessible) references = new Map(references).set(reference.id, reference.title ?? '');
					});
			})
			.catch(() => {});
	});
</script>

{#if readerState === 'loading'}
	<div role="status" aria-label={m['common.loading']()} class="mx-auto w-full max-w-[760px] space-y-3 px-4 py-8">
		<Skeleton class="h-8 w-2/3" />
		<Skeleton class="h-4 w-full" />
	</div>
{:else if readerState === 'unpublished'}
	<EmptyState
		title={m['wiki.reader.not_published']()}
		action={{ label: m['wiki.reader.open_editor'](), onclick: () => (location.href = editHref) }}
	/>
{:else if readerState === 'failed'}
	<EmptyState title={m['wiki.reader.unavailable']()} />
{:else}
	<WikiReader
		{pages}
		selectedId={pages.some((p) => p.id === selected) ? selected : rootId}
		hrefFor={(id) => `?page=${id}`}
		{references}
	/>
{/if}
