<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { listProjects, createProject } from '$lib/api/projects';
	import type { Project } from '$lib/types/project';
	import EmptyState from '$lib/components/shared/EmptyState.svelte';
	import LoadingState from '$lib/components/shared/LoadingState.svelte';
	import { toast } from 'svelte-sonner';
	import { Plus } from 'lucide-svelte';

	const slug = $derived(page.params.workspaceSlug ?? '');
	let projects = $state<Project[]>([]);
	let loading = $state(true);
	let showForm = $state(false);
	let newName = $state('');

	onMount(async () => {
		try {
			projects = await listProjects(slug);
		} finally {
			loading = false;
		}
	});

	async function handleCreate(e: Event) {
		e.preventDefault();
		if (!newName.trim()) return;
		try {
			const project = await createProject(slug, { name: newName });
			projects = [...projects, project];
			newName = '';
			showForm = false;
		} catch (err: any) {
			toast.error(err?.error?.message || 'Failed to create project');
		}
	}
</script>

<div class="h-full">
	<div
		class="flex items-center justify-between border-b border-[var(--app-border)] px-6 py-3"
	>
		<h1 class="text-sm font-medium text-[var(--color-text-primary)]">Projects</h1>
		<button
			onclick={() => (showForm = true)}
			class="flex items-center gap-1 rounded-md bg-[var(--app-accent)] px-3 py-1.5 text-sm text-white hover:bg-[var(--app-accent-hover)]"
		>
			<Plus size={14} />
			New Project
		</button>
	</div>

	{#if showForm}
		<form
			onsubmit={handleCreate}
			class="flex gap-2 border-b border-[var(--app-border)] px-6 py-3"
		>
			<input
				type="text"
				bind:value={newName}
				placeholder="Project name"
				autofocus
				class="flex-1 rounded border border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-3 py-1.5 text-sm text-[var(--color-text-primary)] outline-none"
			/>
			<button
				type="submit"
				class="rounded bg-[var(--app-accent)] px-3 py-1.5 text-sm text-white">Create</button
			>
			<button
				type="button"
				onclick={() => (showForm = false)}
				class="rounded border border-[var(--app-border)] px-3 py-1.5 text-sm text-[var(--color-text-secondary)]"
				>Cancel</button
			>
		</form>
	{/if}

	{#if loading}
		<LoadingState />
	{:else if projects.length === 0}
		<EmptyState
			title="No projects yet"
			description="Create a project to organize your issues"
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
