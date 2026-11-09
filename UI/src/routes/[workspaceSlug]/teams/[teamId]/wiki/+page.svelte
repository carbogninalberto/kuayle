<script lang="ts">
	import { page } from '$app/state';
	import { BookOpen, ChevronRight, SquareUser } from '@lucide/svelte';
	import SidebarToggle from '$lib/components/layout/SidebarToggle.svelte';
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

<div class="flex h-full min-w-0 flex-col">
	<div class="flex h-[49px] shrink-0 items-center border-b border-[var(--app-border)] px-3 sm:px-4">
		<div class="flex min-w-0 items-center gap-3">
			<SidebarToggle />
			<nav class="flex min-w-0 items-center gap-1.5 text-sm">
				{#if team}
					<a
						href="/{slug}/teams/{teamId}"
						class="flex min-w-0 items-center gap-1.5 text-[var(--color-text-tertiary)] hover:text-[var(--color-text-secondary)]"
					>
						<SquareUser size={14} class="shrink-0" style="color: {sidebarState.getTeamColor(teamId)}" />
						<span class="truncate">{team.name}</span>
					</a>
					<ChevronRight size={12} class="shrink-0 text-[var(--color-text-tertiary)]" />
				{/if}
				<h1 class="flex items-center gap-1.5 font-medium text-[var(--color-text-primary)]">
					<BookOpen size={14} class="shrink-0" />
					{m['wiki.title']()}
				</h1>
			</nav>
		</div>
	</div>
	{#key `${slug}/${teamId}`}
		<WikiView {slug} scope={{ kind: 'team', id: teamId }} {canWrite} emptyDescription={m['wiki.empty_desc_team']()} />
	{/key}
</div>
