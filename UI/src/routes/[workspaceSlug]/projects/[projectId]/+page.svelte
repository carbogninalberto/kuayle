<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { getProject, updateProject, deleteProject } from '$lib/api/projects';
	import { issuesState } from '$lib/features/issues/issues.state.svelte';
	import type { Project, ProjectStatus } from '$lib/types/project';
	import IssueRow from '$lib/features/issues/IssueRow.svelte';
	import IssueDetail from '$lib/features/issues/IssueDetail.svelte';
	import EmptyState from '$lib/components/shared/EmptyState.svelte';
	import LoadingState from '$lib/components/shared/LoadingState.svelte';
	import CycleProgress from '$lib/features/cycles/CycleProgress.svelte';
	import { Badge } from '$lib/components/ui/badge';
	import { Button } from '$lib/components/ui/button';
	import * as Popover from '$lib/components/ui/popover';
	import { toast } from 'svelte-sonner';
	import { formatRelativeTime } from '$lib/utils/format';
	import {
		ArrowLeft,
		Trash2,
		MoreHorizontal,
		Circle,
		Play,
		CheckCircle2,
		XCircle,
		Calendar
	} from 'lucide-svelte';

	const slug = $derived(page.params.workspaceSlug ?? '');
	const projectId = $derived(page.params.projectId ?? '');

	let project = $state<Project | null>(null);
	let loading = $state(true);
	let statusOpen = $state(false);
	let actionsOpen = $state(false);

	const STATUS_OPTIONS: { value: ProjectStatus; label: string; icon: typeof Circle }[] = [
		{ value: 'planned', label: 'Planned', icon: Circle },
		{ value: 'in_progress', label: 'In Progress', icon: Play },
		{ value: 'completed', label: 'Completed', icon: CheckCircle2 },
		{ value: 'cancelled', label: 'Cancelled', icon: XCircle }
	];

	onMount(async () => {
		try {
			project = await getProject(slug, projectId);
			issuesState.load(slug, { project: projectId });
		} catch {
			toast.error('Project not found');
			goto(`/${slug}/projects`);
		} finally {
			loading = false;
		}
	});

	async function handleStatusChange(status: ProjectStatus) {
		if (!project) return;
		try {
			project = await updateProject(slug, project.id, { status });
			statusOpen = false;
			toast.success('Status updated');
		} catch (err: any) {
			toast.error(err?.error?.message || 'Failed to update status');
		}
	}

	async function handleDelete() {
		if (!project) return;
		try {
			await deleteProject(slug, project.id);
			toast.success('Project deleted');
			goto(`/${slug}/projects`);
		} catch (err: any) {
			toast.error(err?.error?.message || 'Failed to delete project');
		}
	}

	function statusVariant(status: ProjectStatus): 'default' | 'secondary' | 'outline' | 'destructive' {
		switch (status) {
			case 'in_progress': return 'default';
			case 'completed': return 'secondary';
			case 'cancelled': return 'destructive';
			default: return 'outline';
		}
	}

	function formatDate(date: string | null): string {
		if (!date) return '—';
		return new Date(date).toLocaleDateString('en-US', { month: 'short', day: 'numeric', year: 'numeric' });
	}
</script>

<div class="flex h-full flex-col">
	{#if loading}
		<LoadingState />
	{:else if project}
		<!-- Header -->
		<div class="flex h-[49px] items-center justify-between border-b border-[var(--app-border)] px-6">
			<div class="flex items-center gap-3">
				<a
					href="/{slug}/projects"
					class="text-[var(--color-text-tertiary)] hover:text-[var(--color-text-secondary)]"
				>
					<ArrowLeft size={16} />
				</a>
				<h1 class="text-sm font-medium text-[var(--color-text-primary)]">{project.name}</h1>
				<Popover.Root bind:open={statusOpen}>
					<Popover.Trigger>
						<Badge variant={statusVariant(project.status)} class="cursor-pointer text-[10px]">
							{project.status.replace('_', ' ')}
						</Badge>
					</Popover.Trigger>
					<Popover.Content class="w-40 p-1" align="start">
						{#each STATUS_OPTIONS as option}
							<button
								onclick={() => handleStatusChange(option.value)}
								class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)] {project.status === option.value ? 'bg-[var(--color-bg-hover)]' : ''}"
							>
								<svelte:component this={option.icon} size={14} />
								{option.label}
							</button>
						{/each}
					</Popover.Content>
				</Popover.Root>
			</div>
			<Popover.Root bind:open={actionsOpen}>
				<Popover.Trigger>
					<Button variant="ghost" size="icon-sm">
						<MoreHorizontal size={14} />
					</Button>
				</Popover.Trigger>
				<Popover.Content class="w-40 p-1" align="end">
					<button
						onclick={() => { actionsOpen = false; handleDelete(); }}
						class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-error)] hover:bg-[var(--color-bg-hover)]"
					>
						<Trash2 size={14} />
						Delete project
					</button>
				</Popover.Content>
			</Popover.Root>
		</div>

		<!-- Project info -->
		<div class="border-b border-[var(--app-border)] px-6 py-4">
			<div class="flex items-center gap-6 text-xs text-[var(--color-text-tertiary)]">
				{#if project.start_date || project.target_date}
					<div class="flex items-center gap-1.5">
						<Calendar size={12} />
						{formatDate(project.start_date)} → {formatDate(project.target_date)}
					</div>
				{/if}
				{#if project.progress}
					<span>{project.progress.completed} of {project.progress.total} issues done</span>
				{/if}
			</div>
			{#if project.description}
				<p class="mt-2 text-sm text-[var(--color-text-secondary)]">{project.description}</p>
			{/if}
			{#if project.progress && project.progress.total > 0}
				<div class="mt-3 w-64">
					<CycleProgress progress={project.progress} />
				</div>
			{/if}
		</div>

		<!-- Issues -->
		<div class="flex-1 overflow-y-auto">
			{#if issuesState.loading}
				<LoadingState />
			{:else if issuesState.issues.length === 0}
				<EmptyState
					title="No issues in this project"
					description="Assign issues to this project when creating or editing them"
				/>
			{:else}
				{#each issuesState.issues as issue (issue.id)}
					<IssueRow {issue} onclick={(i) => issuesState.select(i)} />
				{/each}
			{/if}
		</div>
	{/if}
</div>

{#if issuesState.selectedIssue}
	<IssueDetail
		issue={issuesState.selectedIssue}
		{slug}
		onclose={() => issuesState.select(null)}
	/>
{/if}
