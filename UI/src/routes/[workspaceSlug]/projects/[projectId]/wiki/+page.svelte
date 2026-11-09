<script lang="ts">
	import { page } from '$app/state';
	import { BookOpen, Box, ChevronRight } from '@lucide/svelte';
	import SidebarToggle from '$lib/components/layout/SidebarToggle.svelte';
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

<div class="flex h-full min-w-0 flex-col">
	<div class="flex h-[49px] shrink-0 items-center border-b border-[var(--app-border)] px-3 sm:px-4">
		<div class="flex min-w-0 items-center gap-3">
			<SidebarToggle />
			<nav class="flex min-w-0 items-center gap-1.5 text-sm">
				{#if project}
					<a
						href="/{slug}/projects/{projectId}"
						class="flex min-w-0 items-center gap-1.5 text-[var(--color-text-tertiary)] hover:text-[var(--color-text-secondary)]"
					>
						<Box size={14} class="shrink-0" />
						<span class="truncate capitalize">{project.name}</span>
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
	{#key `${slug}/${projectId}`}
		<WikiView {slug} scope={{ kind: 'project', id: projectId }} {canWrite} emptyDescription={m['wiki.empty_desc_project']()} />
	{/key}
</div>
