<script lang="ts">
	import { page } from '$app/state';
	import { Box } from '@lucide/svelte';
	import WikiView from '$lib/features/wiki/WikiView.svelte';
	import { canWriteWiki } from '$lib/features/wiki/wiki-access';
	import { getProject } from '$lib/api/projects';
	import type { Project } from '$lib/types/project';
	import { m } from '$lib/paraglide/messages.js';

	const slug = $derived(page.params.workspaceSlug ?? '');
	const projectId = $derived(page.params.projectId ?? '');
	let project = $state<Project | null>(null);
	let canWrite = $state(false);

	$effect(() => {
		const s = slug;
		const id = projectId;
		project = null;
		canWrite = false;
		// The wiki itself reports a missing or inaccessible project.
		void getProject(s, id)
			.then((loaded) => {
				if (s === slug && id === projectId) project = loaded;
			})
			.catch(() => {});
		void canWriteWiki(s).then((allowed) => {
			if (s === slug) canWrite = allowed;
		});
	});
</script>

<svelte:head><title>{project ? `${project.name} · ${m['wiki.title']()}` : m['wiki.title']()}</title></svelte:head>

{#key `${slug}/${projectId}`}
	<!-- Uploads belong to the project's team, so they wait until it is known. -->
	<WikiView
		{slug}
		scope={{ kind: 'project', id: projectId }}
		{canWrite}
		emptyDescription={m['wiki.empty_desc_project']()}
		scopeLabel={project?.name ?? ''}
		uploadTeamId={project ? project.team_id : undefined}
	>
		{#snippet crumb()}
			{#if project}
				<a
					href="/{slug}/projects/{projectId}"
					class="flex min-w-0 items-center gap-1.5 text-[var(--color-text-tertiary)] transition-colors hover:text-[var(--color-text-primary)]"
				>
					<Box size={13} class="shrink-0" />
					<span class="truncate capitalize">{project.name}</span>
				</a>
			{/if}
		{/snippet}
	</WikiView>
{/key}
