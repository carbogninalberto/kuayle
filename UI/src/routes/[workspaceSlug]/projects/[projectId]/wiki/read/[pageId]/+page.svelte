<script lang="ts">
	import { page } from '$app/state';
	import InternalWikiReader from '$lib/features/wiki/InternalWikiReader.svelte';
	import { m } from '$lib/paraglide/messages.js';

	const slug = $derived(page.params.workspaceSlug ?? '');
	const projectId = $derived(page.params.projectId ?? '');
	const pageId = $derived(page.params.pageId ?? '');
</script>

<svelte:head><title>{m['wiki.title']()}</title></svelte:head>

<div class="flex h-full min-w-0 flex-col overflow-y-auto">
	{#key `${slug}/${projectId}/${pageId}`}
		<InternalWikiReader
			{slug}
			scope={{ kind: 'project', id: projectId }}
			rootId={pageId}
			editHref="/{slug}/projects/{projectId}/wiki?page={pageId}"
		/>
	{/key}
</div>
