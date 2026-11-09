<script lang="ts">
	import { page } from '$app/state';
	import { SquareUser } from '@lucide/svelte';
	import { sidebarState } from '$lib/features/layout/sidebar.state.svelte';
	import WikiView from '$lib/features/wiki/WikiView.svelte';
	import { canWriteWiki } from '$lib/features/wiki/wiki-access';
	import { m } from '$lib/paraglide/messages.js';

	const slug = $derived(page.params.workspaceSlug ?? '');
	const teamId = $derived(page.params.teamId ?? '');
	const team = $derived(sidebarState.getTeam(teamId));
	let canWrite = $state(false);

	$effect(() => {
		const s = slug;
		canWrite = false;
		void canWriteWiki(s).then((allowed) => {
			if (s === slug) canWrite = allowed;
		});
	});
</script>

<svelte:head><title>{team ? `${team.name} · ${m['wiki.title']()}` : m['wiki.title']()}</title></svelte:head>

{#key `${slug}/${teamId}`}
	<WikiView
		{slug}
		scope={{ kind: 'team', id: teamId }}
		{canWrite}
		emptyDescription={m['wiki.empty_desc_team']()}
		scopeLabel={team?.name ?? ''}
		uploadTeamId={teamId}
	>
		{#snippet crumb()}
			{#if team}
				<a
					href="/{slug}/teams/{teamId}"
					class="flex min-w-0 items-center gap-1.5 text-[var(--color-text-tertiary)] transition-colors hover:text-[var(--color-text-primary)]"
				>
					<SquareUser size={13} class="shrink-0" style="color: {sidebarState.getTeamColor(teamId)}" />
					<span class="truncate">{team.name}</span>
				</a>
			{/if}
		{/snippet}
	</WikiView>
{/key}
