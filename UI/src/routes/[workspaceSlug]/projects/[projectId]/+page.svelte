<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { getProject, updateProject, deleteProject } from '$lib/api/projects';
	import { getWorkspace } from '$lib/api/workspaces';
	import { issuesState } from '$lib/features/issues/issues.state.svelte';
	import { teamStatusesState } from '$lib/features/issues/team-statuses.state.svelte';
	import { listCycles } from '$lib/api/cycles';
	import { listTeams } from '$lib/api/teams';
	import type { Project, ProjectStatus } from '$lib/types/project';
	import type { Cycle } from '$lib/types/cycle';
	import type { Team } from '$lib/types/team';
	import IssueRow from '$lib/features/issues/IssueRow.svelte';
	import IssueListLoadMore from '$lib/features/issues/IssueListLoadMore.svelte';
	import IssueDetail from '$lib/features/issues/IssueDetail.svelte';
	import GanttChart from '$lib/features/projects/GanttChart.svelte';
	import EmptyState from '$lib/components/shared/EmptyState.svelte';
	import CycleProgress from '$lib/features/cycles/CycleProgress.svelte';
	import DatePickerPopover from '$lib/components/shared/DatePickerPopover.svelte';
	import { Badge } from '$lib/components/ui/badge';
	import { Button } from '$lib/components/ui/button';
	import { Label } from '$lib/components/ui/label';
	import * as Dialog from '$lib/components/ui/dialog';
	import * as Select from '$lib/components/ui/select';
	import * as Popover from '$lib/components/ui/popover';
	import { getGitHubStatus } from '$lib/api/github';
	import { deleteDevMachineScopeSetting, getDevMachineScopeSetting, listDevMachineEnvironments, updateDevMachineScopeSetting } from '$lib/api/dev-machines';
	import type { GitHubRepo } from '$lib/types/github';
	import type { DevMachineEnvironment } from '$lib/types/dev-machine';
	import { appToast } from '$lib/features/toast/toast';
	import { createKeyboardHandler } from '$lib/utils/keyboard';
	import {
		Trash2,
		MoreHorizontal,
		Circle,
		Play,
		CheckCircle2,
		XCircle,
		Calendar,
		List,
		BarChart3,
		ChevronRight,
		SquareUser,
		Box,
		Settings2
	} from 'lucide-svelte';
	import { sidebarState } from '$lib/features/layout/sidebar.state.svelte';
	import SidebarToggle from '$lib/components/layout/SidebarToggle.svelte';

	const slug = $derived(page.params.workspaceSlug ?? '');
	const projectId = $derived(page.params.projectId ?? '');

	let project = $state<Project | null>(null);
	let teams = $state<Team[]>([]);
	let cycles = $state<Cycle[]>([]);
	let loading = $state(true);
	let statusOpen = $state(false);
	let actionsOpen = $state(false);
	let viewMode = $state<'list' | 'gantt'>('list');
	let lastSelectedId = $state<string | null>(null);
	let developmentOpen = $state(false);
	let developmentRepositories = $state<GitHubRepo[]>([]);
	let developmentEnvironments = $state<DevMachineEnvironment[]>([]);
	let developmentRepositoryId = $state('inherit');
	let developmentEnvironmentId = $state('inherit');
	let developmentLoading = $state(true);
	let developmentReady = $state(false);
	let savingDevelopment = $state(false);
	let canManageDevelopment = $state(false);
	let developmentRequestVersion = 0;
	let developmentSaveVersion = 0;
	const projectTeam = $derived(project?.team_id ? teams.find(t => t.id === project!.team_id) : null);

	const STATUS_OPTIONS: { value: ProjectStatus; label: string; icon: typeof Circle }[] = [
		{ value: 'planned', label: 'Planned', icon: Circle },
		{ value: 'in_progress', label: 'In Progress', icon: Play },
		{ value: 'completed', label: 'Completed', icon: CheckCircle2 },
		{ value: 'cancelled', label: 'Cancelled', icon: XCircle }
	];

	function isCurrentDevelopmentScope(s: string, pid: string, version: number) {
		return slug === s && projectId === pid && developmentRequestVersion === version;
	}

	async function loadDevelopmentSettings(s: string, pid: string, version: number) {
		try {
			const workspace = await getWorkspace(s);
			if (!isCurrentDevelopmentScope(s, pid, version)) return;
			canManageDevelopment = workspace.current_user_role === 'owner' || workspace.current_user_role === 'admin';
			if (!canManageDevelopment) return;
			const [github, developmentSetting, availableEnvironments] = await Promise.all([
				getGitHubStatus(s), getDevMachineScopeSetting(s, 'project', pid), listDevMachineEnvironments(s)
			]);
			if (!isCurrentDevelopmentScope(s, pid, version)) return;
			developmentRepositories = github.repos ?? [];
			developmentEnvironments = (availableEnvironments ?? []).filter((item) => item.status === 'ready');
			developmentRepositoryId = developmentSetting.github_repo_id ?? 'inherit';
			developmentEnvironmentId = developmentSetting.environment_id ?? 'inherit';
			developmentReady = true;
		} catch (error) {
			if (!isCurrentDevelopmentScope(s, pid, version)) return;
			appToast.apiError(error, 'Failed to load project development settings');
		} finally {
			if (isCurrentDevelopmentScope(s, pid, version)) developmentLoading = false;
		}
	}

	async function loadProject(s: string, pid: string) {
		loading = true;
		try {
			project = await getProject(s, pid);
			await issuesState.load(s, viewMode === 'gantt' ? { project: pid, per_page: '200' } : { project: pid });
			const firstTeamId = issuesState.issues[0]?.team_id;
			if (firstTeamId) {
				teamStatusesState.load(s, firstTeamId);
			}
			teams = await listTeams(s);
			const allCycles: Cycle[] = [];
			for (const team of teams) {
				const tc = await listCycles(s, team.id);
				allCycles.push(...tc);
			}
			cycles = allCycles;
		} catch {
			appToast.error('Project not found');
			goto(`/${slug}/projects`);
		} finally {
			loading = false;
		}
	}

	$effect(() => {
		loadProject(slug, projectId);
	});

	$effect(() => {
		const s = slug;
		const pid = projectId;
		const version = ++developmentRequestVersion;
		developmentSaveVersion++;
		developmentOpen = false;
		developmentRepositories = [];
		developmentEnvironments = [];
		developmentRepositoryId = 'inherit';
		developmentEnvironmentId = 'inherit';
		developmentLoading = true;
		developmentReady = false;
		savingDevelopment = false;
		canManageDevelopment = false;
		if (!s || !pid) return;
		void loadDevelopmentSettings(s, pid, version);
		return () => {
			if (developmentRequestVersion === version) developmentRequestVersion++;
			developmentSaveVersion++;
		};
	});

	async function handleStatusChange(status: ProjectStatus) {
		if (!project) return;
		try {
			project = await updateProject(slug, project.id, { status });
			statusOpen = false;
			appToast.success('Status updated');
		} catch (err: any) {
			appToast.apiError(err, 'Failed to update status');
		}
	}

	async function handleDateChange(field: 'start_date' | 'target_date', value: string | null) {
		if (!project) return;
		try {
			project = await updateProject(slug, project.id, { [field]: value });
			appToast.success('Date updated');
		} catch (err: any) {
			appToast.apiError(err, 'Failed to update date');
		}
	}

	async function handleDelete() {
		if (!project) return;
		try {
			await deleteProject(slug, project.id);
			appToast.success('Project deleted');
			goto(`/${slug}/projects`);
		} catch (err: any) {
			appToast.apiError(err, 'Failed to delete project');
		}
	}

	async function saveDevelopmentSettings() {
		if (!canManageDevelopment || developmentLoading || !developmentReady || savingDevelopment) return;
		const s = slug;
		const pid = projectId;
		const requestVersion = developmentRequestVersion;
		const saveVersion = ++developmentSaveVersion;
		const repositoryId = developmentRepositoryId;
		const environmentId = developmentEnvironmentId;
		const repository = developmentRepositories.find((item) => item.id === repositoryId);
		savingDevelopment = true;
		try {
			if (repositoryId === 'inherit' && environmentId === 'inherit') {
				await deleteDevMachineScopeSetting(s, 'project', pid);
			} else {
				await updateDevMachineScopeSetting(s, {
					scope_type: 'project', scope_id: pid,
					github_repo_id: repository?.id, base_branch: repository?.default_branch,
					environment_id: environmentId === 'inherit' ? undefined : environmentId
				});
			}
			if (!isCurrentDevelopmentScope(s, pid, requestVersion) || developmentSaveVersion !== saveVersion) return;
			developmentOpen = false;
			appToast.success('Project development settings saved');
		} catch (error) {
			if (!isCurrentDevelopmentScope(s, pid, requestVersion) || developmentSaveVersion !== saveVersion) return;
			appToast.apiError(error, 'Failed to save project development settings');
		} finally {
			if (isCurrentDevelopmentScope(s, pid, requestVersion) && developmentSaveVersion === saveVersion) savingDevelopment = false;
		}
	}

	const keyHandler = createKeyboardHandler([
		{ key: 'a', ctrl: true, handler: () => issuesState.selectAll() },
		{ key: 'Escape', handler: () => issuesState.clearSelection() },
	]);

	onMount(() => {
		document.addEventListener('keydown', keyHandler);
		return () => document.removeEventListener('keydown', keyHandler);
	});

	function statusVariant(status: ProjectStatus): 'default' | 'secondary' | 'outline' | 'destructive' {
		switch (status) {
			case 'in_progress': return 'default';
			case 'completed': return 'secondary';
			case 'cancelled': return 'destructive';
			default: return 'outline';
		}
	}
</script>

<div class="flex h-full flex-col">
	{#if !loading && project}
		<!-- Header -->
		<div class="flex h-[49px] items-center justify-between border-b border-[var(--app-border)] px-6">
			<div class="flex items-center gap-3">
				<SidebarToggle />
				<nav class="flex items-center gap-1.5 text-sm">
					{#if projectTeam}
						<a href="/{slug}/teams/{projectTeam.id}" class="flex items-center gap-1.5 text-[var(--color-text-tertiary)] hover:text-[var(--color-text-secondary)]">
							<SquareUser size={14} class="shrink-0" style="color: {sidebarState.getTeamColor(projectTeam.id)}" />
							{projectTeam.name}
						</a>
						<ChevronRight size={12} class="shrink-0 text-[var(--color-text-tertiary)]" />
						<a href="/{slug}/teams/{projectTeam.id}/projects" class="flex items-center gap-1.5 text-[var(--color-text-tertiary)] hover:text-[var(--color-text-secondary)]">
							<Box size={14} class="shrink-0" />
							Projects
						</a>
						<ChevronRight size={12} class="shrink-0 text-[var(--color-text-tertiary)]" />
					{/if}
					<span class="font-medium text-[var(--color-text-primary)]">{project.name}</span>
				</nav>
				<Popover.Root bind:open={statusOpen}>
					<Popover.Trigger>
						<Badge variant={statusVariant(project.status)} class="cursor-pointer text-[10px]">
							{project.status.replace('_', ' ')}
						</Badge>
					</Popover.Trigger>
					<Popover.Content class="w-40 p-1" align="start">
						{#each STATUS_OPTIONS as option}
							{@const StatusIcon = option.icon}
						<button
								onclick={() => handleStatusChange(option.value)}
								class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)] {project.status === option.value ? 'bg-[var(--color-bg-hover)]' : ''}"
							>
								<StatusIcon size={14} />
								{option.label}
							</button>
						{/each}
					</Popover.Content>
				</Popover.Root>
			</div>
			<div class="flex items-center gap-2">
				<Button variant="ghost" size="icon-sm" disabled={!canManageDevelopment || developmentLoading || !developmentReady} onclick={() => (developmentOpen = true)} title={developmentLoading ? 'Loading development settings' : canManageDevelopment && developmentReady ? 'Development settings' : canManageDevelopment ? 'Development settings unavailable' : 'Workspace admin required'}><Settings2 size={15} /></Button>
				<!-- View switcher -->
				<div class="flex rounded-md border border-[var(--app-border)]">
					<button
						onclick={() => (viewMode = 'list')}
						class="rounded-l-md px-2 py-1 {viewMode === 'list' ? 'bg-[var(--color-bg-hover)] text-[var(--color-text-primary)]' : 'text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)]'}"
						title="List view"
					>
						<List size={14} />
					</button>
					<button
						onclick={() => (viewMode = 'gantt')}
						class="rounded-r-md px-2 py-1 {viewMode === 'gantt' ? 'bg-[var(--color-bg-hover)] text-[var(--color-text-primary)]' : 'text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)]'}"
						title="Gantt view"
					>
						<BarChart3 size={14} />
					</button>
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
		</div>

		<!-- Project info -->
		<div class="border-b border-[var(--app-border)] px-6 py-4">
			<div class="flex items-center gap-4 text-xs text-[var(--color-text-tertiary)]">
				<div class="flex items-center gap-1.5">
					<Calendar size={12} />
					<span>Start:</span>
					<DatePickerPopover
						value={project.start_date}
						onchange={(d) => handleDateChange('start_date', d)}
						placeholder="Set start"
					/>
				</div>
				<div class="flex items-center gap-1.5">
					<span>Target:</span>
					<DatePickerPopover
						value={project.target_date}
						onchange={(d) => handleDateChange('target_date', d)}
						placeholder="Set target"
					/>
				</div>
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

		<!-- Content -->
		{#if viewMode === 'list'}
			<div class="flex-1 overflow-y-auto">
				{#if !issuesState.loading && issuesState.issues.length === 0}
					<EmptyState
						title="No issues in this project"
						description="Assign issues to this project when creating or editing them"
					/>
				{:else}
					{#each issuesState.issues as issue (issue.id)}
						<IssueRow {issue} {slug} {lastSelectedId} onlastselected={(id) => lastSelectedId = id} onclick={(i) => { lastSelectedId = i.id; issuesState.select(i); }} />
					{/each}
				{/if}
				<IssueListLoadMore />
			</div>
		{:else}
			<div class="flex-1 min-h-0 px-4 py-3">
				{#if !issuesState.loading}
					<GanttChart
						issues={issuesState.issues}
						{cycles}
						onissueclick={(i) => goto(`/${slug}/issue/${i.identifier}`)}
					/>
				{/if}
			</div>
		{/if}
	{/if}
</div>

<Dialog.Root bind:open={developmentOpen}>
	<Dialog.Content class="sm:max-w-md">
		<Dialog.Header><Dialog.Title>Project development</Dialog.Title><Dialog.Description>Choose defaults for issue repositories and machine environments in this project.</Dialog.Description></Dialog.Header>
		{#if !canManageDevelopment}<p class="rounded-md border border-[var(--app-border)] p-3 text-xs text-[var(--color-text-tertiary)]">Workspace owners and admins manage project development defaults.</p>{/if}
		<div class="space-y-4">
			<div class="space-y-1"><Label>Repository</Label><Select.Root type="single" value={developmentRepositoryId} disabled={developmentLoading || !developmentReady || !canManageDevelopment} onValueChange={(value) => value && (developmentRepositoryId = value)}><Select.Trigger class="w-full">{developmentLoading ? 'Loading...' : developmentRepositories.find((item) => item.id === developmentRepositoryId)?.full_name ?? 'Use team or workspace default'}</Select.Trigger><Select.Content><Select.Item value="inherit" label="Use inherited default">Use inherited default</Select.Item>{#each developmentRepositories as repository}<Select.Item value={repository.id} label={repository.full_name}>{repository.full_name}</Select.Item>{/each}</Select.Content></Select.Root></div>
			<div class="space-y-1"><Label>Environment</Label><Select.Root type="single" value={developmentEnvironmentId} disabled={developmentLoading || !developmentReady || !canManageDevelopment} onValueChange={(value) => value && (developmentEnvironmentId = value)}><Select.Trigger class="w-full">{developmentLoading ? 'Loading...' : developmentEnvironments.find((item) => item.id === developmentEnvironmentId)?.name ?? 'Use team or workspace default'}</Select.Trigger><Select.Content><Select.Item value="inherit" label="Use inherited default">Use inherited default</Select.Item>{#each developmentEnvironments as environment}<Select.Item value={environment.id} label={environment.name}>{environment.name}</Select.Item>{/each}</Select.Content></Select.Root></div>
		</div>
		<Dialog.Footer><Button variant="outline" onclick={() => (developmentOpen = false)}>Cancel</Button><Button onclick={saveDevelopmentSettings} disabled={developmentLoading || !developmentReady || savingDevelopment || !canManageDevelopment}>{savingDevelopment ? 'Saving...' : 'Save'}</Button></Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>

{#if issuesState.selectedIssue}
	<IssueDetail
		issue={issuesState.selectedIssue}
		{slug}
		onclose={() => issuesState.select(null)}
	/>
{/if}
