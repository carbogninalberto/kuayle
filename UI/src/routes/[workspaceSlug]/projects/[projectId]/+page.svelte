<script lang="ts">
	import TestCommandField from '$lib/features/dev-machines/TestCommandField.svelte';
	import ComposeFileField from '$lib/features/dev-machines/ComposeFileField.svelte';
	import AgentDefaultsField from '$lib/features/dev-machines/AgentDefaultsField.svelte';
	import { parseTestCommand, formatTestCommand } from '$lib/features/dev-machines/test-command';
	import ScopedIssueList from '$lib/features/issues/ScopedIssueList.svelte';
	import ScopeDeleteDialog from '$lib/features/issues/ScopeDeleteDialog.svelte';
	import { hasPermission } from '$lib/security/permissions';
	import type { Role } from '$lib/security/roles';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { getProject, updateProject, deleteProject } from '$lib/api/projects';
	import { getWorkspace } from '$lib/api/workspaces';
	import { listTeams } from '$lib/api/teams';
	import type { Project, ProjectStatus } from '$lib/types/project';
	import type { Team } from '$lib/types/team';
	import CycleProgress from '$lib/features/cycles/CycleProgress.svelte';
	import DatePickerPopover from '$lib/components/shared/DatePickerPopover.svelte';
	import { Badge } from '$lib/components/ui/badge';
	import { Button } from '$lib/components/ui/button';
	import { Label } from '$lib/components/ui/label';
	import * as Dialog from '$lib/components/ui/dialog';
	import * as Select from '$lib/components/ui/select';
	import * as Popover from '$lib/components/ui/popover';
	import { getGitHubStatus } from '$lib/api/github';
	import {
		deleteDevMachineScopeSetting,
		getDevMachineScopeSetting,
		listDevMachineEnvironments,
		updateDevMachineScopeSetting
	} from '$lib/api/dev-machines';
	import type { GitHubRepo } from '$lib/types/github';
	import type { DevMachineEnvironment } from '$lib/types/dev-machine';
	import { appToast } from '$lib/features/toast/toast';
	import { m } from '$lib/paraglide/messages.js';
	import { getLocale, setLocale } from '$lib/paraglide/runtime.js';
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
	} from '@lucide/svelte';
	import { sidebarState } from '$lib/features/layout/sidebar.state.svelte';
	import SidebarToggle from '$lib/components/layout/SidebarToggle.svelte';

	const slug = $derived(page.params.workspaceSlug ?? '');
	const projectId = $derived(page.params.projectId ?? '');

	let project = $state<Project | null>(null);
	let teams = $state<Team[]>([]);
	let loading = $state(true);
	let statusOpen = $state(false);
	let actionsOpen = $state(false);
	let showDelete = $state(false);
	let workspaceRole = $state<Role>('guest');
	const canManage = $derived(hasPermission(workspaceRole, 'project:manage'));
	let projectRequestVersion = 0;
	let developmentOpen = $state(false);
	let developmentRepositories = $state<GitHubRepo[]>([]);
	let developmentEnvironments = $state<DevMachineEnvironment[]>([]);
	let developmentRepositoryId = $state('inherit');
	let developmentEnvironmentId = $state('inherit');
	let developmentTestCommand = $state('');
	let developmentAgentProvider = $state('inherit');
	let developmentAgentInstructions = $state('');
	let developmentComposeFile = $state('');
	let developmentLoading = $state(true);
	let developmentReady = $state(false);
	let savingDevelopment = $state(false);
	let canManageDevelopment = $state(false);
	let developmentRequestVersion = 0;
	let developmentSaveVersion = 0;
	const projectTeam = $derived(project?.team_id ? teams.find((t) => t.id === project!.team_id) : null);

	const STATUS_OPTIONS: { value: ProjectStatus; label: string; icon: typeof Circle }[] = [
		{ value: 'planned', label: m['projects.status.planned'](), icon: Circle },
		{ value: 'in_progress', label: m['projects.status.in_progress'](), icon: Play },
		{ value: 'completed', label: m['projects.status.completed'](), icon: CheckCircle2 },
		{ value: 'cancelled', label: m['projects.status.cancelled'](), icon: XCircle }
	];

	function isCurrentDevelopmentScope(s: string, pid: string, version: number) {
		return slug === s && projectId === pid && developmentRequestVersion === version;
	}

	async function loadDevelopmentSettings(s: string, pid: string, version: number) {
		try {
			const workspace = await getWorkspace(s);
			if (!isCurrentDevelopmentScope(s, pid, version)) return;
			workspaceRole = workspace.current_user_role as Role;
			canManageDevelopment = workspace.current_user_role === 'owner' || workspace.current_user_role === 'admin';
			const [github, developmentSetting, availableEnvironments] = await Promise.all([
				getGitHubStatus(s),
				getDevMachineScopeSetting(s, 'project', pid),
				listDevMachineEnvironments(s)
			]);
			if (!isCurrentDevelopmentScope(s, pid, version)) return;
			developmentRepositories = github.repos ?? [];
			developmentEnvironments = (availableEnvironments ?? []).filter((item) => item.status === 'ready');
			developmentRepositoryId = developmentSetting.github_repo_id ?? 'inherit';
			developmentEnvironmentId = developmentSetting.environment_id ?? 'inherit';
			developmentTestCommand = formatTestCommand(developmentSetting.test_command); developmentAgentProvider = developmentSetting.agent_provider ?? 'inherit'; developmentAgentInstructions = developmentSetting.agent_instructions ?? ''; developmentComposeFile = developmentSetting.compose_file ?? '';
			developmentReady = true;
		} catch (error) {
			if (!isCurrentDevelopmentScope(s, pid, version)) return;
			appToast.apiError(error, m['projects.toast.failed_load_development']());
		} finally {
			if (isCurrentDevelopmentScope(s, pid, version)) developmentLoading = false;
		}
	}

	async function loadProject(s: string, pid: string, version: number) {
		loading = true;
		try {
			const [current, availableTeams] = await Promise.all([getProject(s, pid), listTeams(s)]);
			if (version !== projectRequestVersion) return;
			project = current;
			teams = availableTeams;
		} catch (error) {
			if (version !== projectRequestVersion) return;
			appToast.apiError(error, m['projects.toast.not_found']());
			void goto(`/${s}/projects`);
		} finally {
			if (version === projectRequestVersion) loading = false;
		}
	}
	async function refreshProject() {
		const s = slug,
			pid = projectId,
			version = projectRequestVersion;
		try {
			const current = await getProject(s, pid);
			if (version === projectRequestVersion) project = current;
		} catch (error) {
			if (version === projectRequestVersion) appToast.apiError(error, m['projects.toast.not_found']());
		}
	}
	$effect(() => {
		const s = slug,
			pid = projectId,
			version = ++projectRequestVersion;
		project = null;
		workspaceRole = 'guest';
		showDelete = false;
		void loadProject(s, pid, version);
		return () => {
			projectRequestVersion++;
		};
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
		developmentTestCommand = '';
		developmentAgentProvider = 'inherit';
		developmentAgentInstructions = '';
		developmentComposeFile = '';
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
		if (!project || !canManage) return;
		try {
			await updateProject(slug, project.id, { status });
			await refreshProject();
			statusOpen = false;
			appToast.success(m['projects.toast.status_updated']());
		} catch (err: any) {
			appToast.apiError(err, m['projects.toast.failed_update_status']());
		}
	}

	async function handleDateChange(field: 'start_date' | 'target_date', value: string | null) {
		if (!project || !canManage) return;
		try {
			await updateProject(slug, project.id, { [field]: value });
			await refreshProject();
			appToast.success(m['projects.toast.date_updated']());
		} catch (err: any) {
			appToast.apiError(err, m['projects.toast.failed_update_date']());
		}
	}

	async function handleDelete() {
		if (!project || !canManage) return;
		try {
			await deleteProject(slug, project.id);
			showDelete = false;
			appToast.success(m['projects.toast.deleted']());
			goto(`/${slug}/projects`);
		} catch (err: any) {
			appToast.apiError(err, m['projects.toast.failed_delete']());
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
			const command = parseTestCommand(developmentTestCommand);
			if (repositoryId === 'inherit' && environmentId === 'inherit' && command === undefined && developmentAgentProvider === 'inherit' && !developmentAgentInstructions.trim() && !developmentComposeFile.trim()) {
				await deleteDevMachineScopeSetting(s, 'project', pid);
			} else {
				await updateDevMachineScopeSetting(s, {
					scope_type: 'project',
					scope_id: pid,
					github_repo_id: repository?.id,
					base_branch: repository?.default_branch,
					environment_id: environmentId === 'inherit' ? undefined : environmentId,
					test_command: command, agent_provider: developmentAgentProvider === 'inherit' ? undefined : developmentAgentProvider, agent_instructions: developmentAgentInstructions.trim() || undefined, compose_file: developmentComposeFile.trim() || undefined
				});
			}
			if (!isCurrentDevelopmentScope(s, pid, requestVersion) || developmentSaveVersion !== saveVersion) return;
			developmentOpen = false;
			appToast.success(m['projects.toast.development_saved']());
		} catch (error) {
			if (!isCurrentDevelopmentScope(s, pid, requestVersion) || developmentSaveVersion !== saveVersion) return;
			appToast.apiError(error, m['projects.toast.failed_save_development']());
		} finally {
			if (isCurrentDevelopmentScope(s, pid, requestVersion) && developmentSaveVersion === saveVersion)
				savingDevelopment = false;
		}
	}

	function statusVariant(status: ProjectStatus): 'default' | 'secondary' | 'outline' | 'destructive' {
		switch (status) {
			case 'in_progress':
				return 'default';
			case 'completed':
				return 'secondary';
			case 'cancelled':
				return 'destructive';
			default:
				return 'outline';
		}
	}
</script>

<div class="flex h-full min-w-0 flex-col">
	{#if !loading && project}
		<!-- Header -->
		<div class="flex min-h-[49px] items-center justify-between gap-2 border-b border-[var(--app-border)] px-3 sm:px-4">
			<div class="flex min-w-0 items-center gap-3">
				<SidebarToggle />
				<nav class="flex min-w-0 items-center gap-1.5 text-sm">
					{#if projectTeam}
						<a
							href="/{slug}/teams/{projectTeam.id}"
							class="flex items-center gap-1.5 text-[var(--color-text-tertiary)] hover:text-[var(--color-text-secondary)]"
						>
							<SquareUser size={14} class="shrink-0" style="color: {sidebarState.getTeamColor(projectTeam.id)}" />
							<span class="hidden sm:inline">{projectTeam.name}</span>
						</a>
						<ChevronRight size={12} class="shrink-0 text-[var(--color-text-tertiary)]" />
						<a
							href="/{slug}/teams/{projectTeam.id}/projects"
							class="flex items-center gap-1.5 text-[var(--color-text-tertiary)] hover:text-[var(--color-text-secondary)]"
						>
							<Box size={14} class="shrink-0" />
							<span class="hidden sm:inline">{m['projects.title']()}</span>
						</a>
						<ChevronRight size={12} class="shrink-0 text-[var(--color-text-tertiary)]" />
					{/if}
					<span class="truncate font-medium text-[var(--color-text-primary)]">{project.name}</span>
				</nav>
				<Popover.Root bind:open={statusOpen}>
					<Popover.Trigger disabled={!canManage}>
						<Badge variant={statusVariant(project.status)} class="cursor-pointer text-[10px]">
							{m[`projects.status.${project.status}`]()}
						</Badge>
					</Popover.Trigger>
					<Popover.Content class="w-40 p-1" align="start">
						{#each STATUS_OPTIONS as option}
							{@const StatusIcon = option.icon}
							<Button variant="plain" onclick={() => handleStatusChange(option.value)}
								class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)] {project.status ===
								option.value
									? 'bg-[var(--color-bg-hover)]'
									: ''}"
							>
								<StatusIcon size={14} />
								{option.label}
							</Button>
						{/each}
					</Popover.Content>
				</Popover.Root>
			</div>
			<div class="flex items-center gap-2">
				<Button
					variant="ghost"
					size="icon-sm"
					disabled={developmentLoading || !developmentReady}
					onclick={() => (developmentOpen = true)}
					title={developmentLoading
						? m['projects.development.loading']()
						: developmentReady
							? canManageDevelopment
								? m['projects.development.settings']()
								: m['projects.development.view_settings']()
							: m['projects.development.unavailable']()}><Settings2 size={15} /></Button
				>

				{#if canManage}
					<Popover.Root bind:open={actionsOpen}>
						<Popover.Trigger
							aria-label={m['projects.actions']()}
							class="rounded-md p-1.5 hover:bg-[var(--color-bg-hover)]"><MoreHorizontal size={14} /></Popover.Trigger
						>
						<Popover.Content class="w-40 p-1" align="end">
							<Button variant="plain" onclick={() => {
									actionsOpen = false;
									showDelete = true;
								}}
								class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-error)] hover:bg-[var(--color-bg-hover)]"
							>
								<Trash2 size={14} />
								{m['projects.delete']()}
							</Button>
						</Popover.Content>
					</Popover.Root>
				{/if}
			</div>
		</div>

		<!-- Project info -->
		<div class="border-b border-[var(--app-border)] px-3 py-4 sm:px-4">
			<div class="flex flex-wrap items-center gap-4 text-xs text-[var(--color-text-tertiary)]">
				<div class="flex items-center gap-1.5">
					<Calendar size={12} />
					<span>{m['projects.start_date']()}</span>
					{#if canManage}<DatePickerPopover
							value={project.start_date}
							onchange={(d) => handleDateChange('start_date', d)}
							placeholder={m['projects.set_start']()}
						/>{:else}<span>{project.start_date?.slice(0, 10) ?? '—'}</span>{/if}
				</div>
				<div class="flex items-center gap-1.5">
					<span>{m['projects.target_date']()}</span>
					{#if canManage}<DatePickerPopover
							value={project.target_date}
							onchange={(d) => handleDateChange('target_date', d)}
							placeholder={m['projects.set_target']()}
						/>{:else}<span>{project.target_date?.slice(0, 10) ?? '—'}</span>{/if}
				</div>
				{#if project.progress}
					<span>{m['projects.progress']({ completed: project.progress.completed, total: project.progress.total })}</span
					>
				{/if}
			</div>
			{#if project.description}
				<p class="mt-2 text-sm text-[var(--color-text-secondary)]">{project.description}</p>
			{/if}
			{#if project.progress && project.progress.total > 0}
				<div class="mt-3 w-full sm:w-64">
					<CycleProgress progress={project.progress} />
				</div>
			{/if}
		</div>

		{#key `${slug}/${projectId}`}
			<ScopedIssueList
				{slug}
				scope={{ kind: 'project', id: projectId, teamId: project.team_id ?? undefined }}
				role={workspaceRole}
				teamOptions={teams}
				allowGantt
				onchanged={refreshProject}
			/>
		{/key}
	{:else if loading}
		<div role="status" class="animate-pulse p-4 text-sm text-[var(--color-text-tertiary)]">{m['common.loading']()}</div>
	{/if}
</div>

{#if showDelete && canManage}<ScopeDeleteDialog
		title={m['projects.delete']()}
		message={m['projects.delete_confirm']()}
		oncancel={() => (showDelete = false)}
		onconfirm={handleDelete}
	/>{/if}

<Dialog.Root bind:open={developmentOpen}>
	<Dialog.Content class="max-h-[90dvh] overflow-y-auto sm:max-w-md">
		<Dialog.Header
			><Dialog.Title>{m['projects.development.title']()}</Dialog.Title><Dialog.Description
				>{m['projects.development.description']()}</Dialog.Description
			></Dialog.Header
		>
		{#if !canManageDevelopment}<p
				class="rounded-md border border-[var(--app-border)] p-3 text-xs text-[var(--color-text-tertiary)]"
			>
				{m['projects.development.permission_note']()}
			</p>{/if}
		<div class="space-y-4">
			<div class="space-y-1">
				<Label>{m['projects.development.repository']()}</Label><Select.Root
					type="single"
					value={developmentRepositoryId}
					disabled={developmentLoading || !developmentReady || !canManageDevelopment}
					onValueChange={(value) => value && (developmentRepositoryId = value)}
					><Select.Trigger class="w-full"
						>{developmentLoading
							? m['common.loading']()
							: (developmentRepositories.find((item) => item.id === developmentRepositoryId)?.full_name ??
								m['projects.development.team_or_workspace_default']())}</Select.Trigger
					><Select.Content
						><Select.Item value="inherit" label={m['projects.development.inherited_default']()}
							>{m['projects.development.inherited_default']()}</Select.Item
						>{#each developmentRepositories as repository}<Select.Item
								value={repository.id}
								label={repository.full_name}>{repository.full_name}</Select.Item
							>{/each}</Select.Content
					></Select.Root
				>
			</div>
			<div class="space-y-1">
				<Label>{m['projects.development.environment']()}</Label><Select.Root
					type="single"
					value={developmentEnvironmentId}
					disabled={developmentLoading || !developmentReady || !canManageDevelopment}
					onValueChange={(value) => value && (developmentEnvironmentId = value)}
					><Select.Trigger class="w-full"
						>{developmentLoading
							? m['common.loading']()
							: (developmentEnvironments.find((item) => item.id === developmentEnvironmentId)?.name ??
								m['projects.development.team_or_workspace_default']())}</Select.Trigger
					><Select.Content
						><Select.Item value="inherit" label={m['projects.development.inherited_default']()}
							>{m['projects.development.inherited_default']()}</Select.Item
						>{#each developmentEnvironments as environment}<Select.Item value={environment.id} label={environment.name}
								>{environment.name}</Select.Item
							>{/each}</Select.Content
					></Select.Root
				>
			</div>
			<TestCommandField bind:value={developmentTestCommand} disabled={developmentLoading || !developmentReady || savingDevelopment || !canManageDevelopment} /><ComposeFileField bind:value={developmentComposeFile} disabled={developmentLoading || !developmentReady || savingDevelopment || !canManageDevelopment} /><AgentDefaultsField bind:provider={developmentAgentProvider} bind:instructions={developmentAgentInstructions} disabled={developmentLoading || !developmentReady || savingDevelopment || !canManageDevelopment} />
		</div>
		<Dialog.Footer
			><Button variant="outline" onclick={() => (developmentOpen = false)}>{m['common.cancel']()}</Button><Button
				onclick={saveDevelopmentSettings}
				disabled={developmentLoading || !developmentReady || savingDevelopment || !canManageDevelopment}
				>{savingDevelopment ? m['common.saving']() : m['common.save']()}</Button
			></Dialog.Footer
		>
	</Dialog.Content>
</Dialog.Root>
