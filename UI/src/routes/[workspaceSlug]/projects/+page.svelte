<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { listProjects, createProject } from '$lib/api/projects';
	import { listTeams } from '$lib/api/teams';
	import type { Project, ProjectStatus } from '$lib/types/project';
	import type { Team } from '$lib/types/team';
	import EmptyState from '$lib/components/shared/EmptyState.svelte';
	import CreateProjectDialog from '$lib/features/projects/CreateProjectDialog.svelte';
	import { Badge } from '$lib/components/ui/badge';
	import { toast } from 'svelte-sonner';
	import { Plus } from 'lucide-svelte';

	const slug = $derived(page.params.workspaceSlug ?? '');
	let projects = $state<Project[]>([]);
	let teams = $state<Team[]>([]);
	let loading = $state(true);
	let showCreateProject = $state(false);

	const STATUS_LABELS: Record<ProjectStatus, string> = {
		planned: 'Planned',
		in_progress: 'In Progress',
		completed: 'Completed',
		cancelled: 'Cancelled'
	};

	onMount(async () => {
		try {
			[projects, teams] = await Promise.all([listProjects(slug), listTeams(slug)]);
		} finally {
			loading = false;
		}
	});

	async function handleCreate(data: { name: string; description?: string; team_id?: string }) {
		try {
			const project = await createProject(slug, data);
			projects = [...projects, project];
			toast.success('Project created');
		} catch (err: any) {
			toast.error(err?.error?.message || 'Failed to create project');
		}
	}

	function progressPercentage(project: Project): number {
		if (!project.progress || project.progress.total === 0) return 0;
		return Math.round(((project.progress.completed + project.progress.cancelled) / project.progress.total) * 100);
	}

	function statusVariant(status: ProjectStatus): 'default' | 'secondary' | 'outline' | 'destructive' {
		switch (status) {
			case 'in_progress': return 'default';
			case 'completed': return 'secondary';
			case 'cancelled': return 'destructive';
			default: return 'outline';
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
			class="rounded-md p-1 text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]"
			title="New Project"
		>
			<Plus size={16} />
		</button>
	</div>

	{#if !loading && projects.length === 0}
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
					class="flex items-center gap-4 px-6 py-3 hover:bg-[var(--color-bg-hover)]"
				>
					<div class="flex-1 min-w-0">
						<div class="flex items-center gap-2">
							<span class="text-sm font-medium text-[var(--color-text-primary)]">{project.name}</span>
							<Badge variant={statusVariant(project.status)} class="text-[10px]">
								{STATUS_LABELS[project.status]}
							</Badge>
							{#if project.team_id}
								{@const team = teams.find(t => t.id === project.team_id)}
								{#if team}
									<span class="text-[10px] text-[var(--color-text-tertiary)]">{team.name}</span>
								{/if}
							{/if}
						</div>
						{#if project.description}
							<p class="mt-0.5 truncate text-xs text-[var(--color-text-tertiary)]">{project.description}</p>
						{/if}
					</div>
					{#if project.progress && project.progress.total > 0}
						<div class="flex items-center gap-2 shrink-0">
							<div class="relative h-1.5 w-24 overflow-hidden rounded-full bg-[var(--color-bg-tertiary)]">
								<div
									class="absolute left-0 top-0 h-full rounded-full bg-[var(--color-success)]"
									style="width: {project.progress.total > 0 ? (project.progress.completed / project.progress.total) * 100 : 0}%"
								></div>
							</div>
							<span class="text-xs tabular-nums text-[var(--color-text-tertiary)]">
								{progressPercentage(project)}%
							</span>
						</div>
					{/if}
				</a>
			{/each}
		</div>
	{/if}
</div>

<CreateProjectDialog
	bind:open={showCreateProject}
	{teams}
	onsubmit={handleCreate}
/>
