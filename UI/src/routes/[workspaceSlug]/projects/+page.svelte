<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { listProjects, createProject } from '$lib/api/projects';
	import type { Project } from '$lib/types/project';
	import EmptyState from '$lib/components/shared/EmptyState.svelte';
	import LoadingState from '$lib/components/shared/LoadingState.svelte';
	import CreateProjectDialog from '$lib/features/projects/CreateProjectDialog.svelte';
	import { toast } from 'svelte-sonner';
	import { Plus } from 'lucide-svelte';

	const slug = $derived(page.params.workspaceSlug ?? '');
	let projects = $state<Project[]>([]);
	let loading = $state(true);
	let showCreateProject = $state(false);

	onMount(async () => {
		try {
			projects = await listProjects(slug);
		} finally {
			loading = false;
		}
	});

	async function handleCreate(data: { name: string; description?: string }) {
		try {
			const project = await createProject(slug, data);
			projects = [...projects, project];
			toast.success('Project created');
		} catch (err: any) {
			toast.error(err?.error?.message || 'Failed to create project');
		}
	}
</script>

<div class="h-full">
	<div
		class="flex h-[49px] items-center justify-between border-b border-[var(--app-border)] px-6"
	>
		<h1 class="text-sm font-medium text-[var(--color-text-primary)]">Projects</h1>
		<button
			onclick={() => (showCreateProject = true)}
			class="flex items-center gap-1 rounded-md bg-[var(--app-accent)] px-3 py-1.5 text-sm text-white hover:bg-[var(--app-accent-hover)]"
		>
			<Plus size={14} />
			New Project
		</button>
	</div>

	{#if loading}
		<LoadingState />
	{:else if projects.length === 0}
		<EmptyState
			title="No projects yet"
			description="Create a project to organize your issues"
			action={{ label: 'New Project', onclick: () => (showCreateProject = true) }}
		/>
	{:else}
		<div class="divide-y divide-[var(--app-border)]">
			{#each projects as project}
				<a
					href="/{slug}/projects/{project.id}"
					class="flex items-center gap-3 px-6 py-3 hover:bg-[var(--color-bg-hover)]"
				>
					<span class="text-sm font-medium text-[var(--color-text-primary)]"
						>{project.name}</span
					>
					<span
						class="rounded-full bg-[var(--color-bg-tertiary)] px-2 py-0.5 text-xs text-[var(--color-text-secondary)]"
						>{project.status}</span
					>
				</a>
			{/each}
		</div>
	{/if}
</div>

<CreateProjectDialog
	bind:open={showCreateProject}
	onsubmit={handleCreate}
/>
