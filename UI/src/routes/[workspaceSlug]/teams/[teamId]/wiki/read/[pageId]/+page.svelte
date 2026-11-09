<script lang="ts">
	import { page } from '$app/state';
	import InternalWikiReader from '$lib/features/wiki/InternalWikiReader.svelte';
	import { sidebarState } from '$lib/features/layout/sidebar.state.svelte';
	import { m } from '$lib/paraglide/messages.js';

	const slug = $derived(page.params.workspaceSlug ?? '');
	const teamId = $derived(page.params.teamId ?? '');
	const pageId = $derived(page.params.pageId ?? '');
	const team = $derived(sidebarState.getTeam(teamId));
</script>

<svelte:head><title>{team ? `${team.name} · ${m['wiki.title']()}` : m['wiki.title']()}</title></svelte:head>

<div class="flex h-full min-w-0 flex-col overflow-y-auto">
	{#key `${slug}/${teamId}/${pageId}`}
		<InternalWikiReader
			{slug}
			scope={{ kind: 'team', id: teamId }}
			rootId={pageId}
			editHref="/{slug}/teams/{teamId}/wiki?page={pageId}"
		/>
	{/key}
</div>
