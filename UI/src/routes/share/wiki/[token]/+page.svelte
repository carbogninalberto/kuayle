<script lang="ts">
	import { page } from '$app/state';
	import { getPublicWiki } from '$lib/api/public';
	import type { PublicWiki } from '$lib/types/wiki';
	import WikiReader from '$lib/features/wiki/WikiReader.svelte';
	import EmptyState from '$lib/components/shared/EmptyState.svelte';
	import { Skeleton } from '$lib/components/ui/skeleton/index.js';
	import { m } from '$lib/features/i18n/messages';
	import { BookOpen } from '@lucide/svelte';

	// A wiki page anyone with the link can read. The API returns only the
	// published page and the pages published with it.
	const token = $derived(page.params.token ?? '');
	const selected = $derived(page.url.searchParams.get('page'));
	let wiki = $state<PublicWiki | null>(null);
	let failed = $state(false);

	$effect(() => {
		const current = token;
		wiki = null;
		failed = false;
		getPublicWiki(current)
			.then((loaded) => {
				if (current === token) wiki = loaded;
			})
			.catch(() => {
				if (current === token) failed = true;
			});
	});

	const title = $derived(wiki?.pages.find((p) => p.id === (selected ?? wiki?.root_id))?.title);
</script>

<svelte:head>
	<title>{title ? `${title} · ${wiki?.workspace_name}` : m['wiki.title']()}</title>
	<meta name="robots" content="noindex" />
</svelte:head>

<div class="flex min-h-[calc(100vh-3rem)] flex-col">
	<header class="flex h-12 shrink-0 items-center gap-2 border-b border-[var(--app-border)] px-4 text-sm">
		<BookOpen size={14} class="text-[var(--color-text-tertiary)]" />
		<span class="truncate text-[var(--color-text-secondary)]">{wiki?.workspace_name ?? ''}</span>
	</header>
	{#if failed}
		<EmptyState title={m['wiki.reader.unavailable']()} description={m['wiki.reader.unavailable_desc']()} />
	{:else if !wiki}
		<div role="status" aria-label={m['common.loading']()} class="mx-auto w-full max-w-[760px] space-y-3 px-4 py-8">
			<Skeleton class="h-8 w-2/3" />
			<Skeleton class="h-4 w-full" />
			<Skeleton class="h-4 w-5/6" />
		</div>
	{:else}
		<WikiReader pages={wiki.pages} selectedId={selected ?? wiki.root_id} hrefFor={(id) => `?page=${id}`} />
	{/if}
</div>
