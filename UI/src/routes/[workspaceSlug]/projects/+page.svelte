<script lang="ts">
	import { getWorkspace } from '$lib/api/workspaces';
	import { hasPermission } from '$lib/security/permissions';
	import type { Role } from '$lib/security/roles';
	import { page } from '$app/state';
	import { listProjects, createProject } from '$lib/api/projects';
	import { listTeams } from '$lib/api/teams';
	import type { Project, ProjectStatus } from '$lib/types/project';
	import type { Team } from '$lib/types/team';
	import EmptyState from '$lib/components/shared/EmptyState.svelte';
	import CreateProjectDialog from '$lib/features/projects/CreateProjectDialog.svelte';
	import { Badge } from '$lib/components/ui/badge';
	import { sidebarState } from '$lib/features/layout/sidebar.state.svelte';
	import { appToast } from '$lib/features/toast/toast';
	import { Plus } from 'lucide-svelte';
	import SidebarToggle from '$lib/components/layout/SidebarToggle.svelte';
	import { m } from '$lib/paraglide/messages.js';
	import { getLocale, setLocale } from '$lib/paraglide/runtime.js';

	const slug = $derived(page.params.workspaceSlug ?? '');
	let projects = $state<Project[]>([]);
	let teams = $state<Team[]>([]);
	let loading = $state(true);
	let showCreateProject = $state(false);
	let role = $state<Role>('guest');
	const canManage = $derived(hasPermission(role, 'project:manage'));
	let loadFailed = $state(false);
	let requestVersion = 0;
	let retryVersion = $state(0);

	function statusLabel(status: ProjectStatus): string {
		return m[`projects.status.${status}`]();
	}

	$effect(() => {
		const s = slug,
			version = ++requestVersion;
		const retry = retryVersion;
		loading = true;
		loadFailed = false;
		role = 'guest';
		showCreateProject = false;
		Promise.all([listProjects(s), listTeams(s), getWorkspace(s)])
			.then(([p, t, workspace]) => {
				if (version !== requestVersion) return;
				projects = p;
				teams = t;
				role = workspace.current_user_role as Role;
			})
			.catch((error) => {
				if (version !== requestVersion) return;
				loadFailed = true;
				appToast.apiError(error, m['projects.failed_load']());
			})
			.finally(() => {
				if (version === requestVersion) loading = false;
			});
		return () => {
			requestVersion++;
		};
	});

	async function handleCreate(data: { name: string; description?: string; team_id?: string }) {
		if (!canManage) return;
		try {
			const project = await createProject(slug, data);
			projects = [...projects, project];
			sidebarState.addProject(project);
			appToast.success(m['projects.toast.created']());
		} catch (err: any) {
			appToast.apiError(err, m['projects.toast.failed_create']());
		}
	}

	function progressPercentage(project: Project): number {
		if (!project.progress || project.progress.total === 0) return 0;
		return Math.round(((project.progress.completed + project.progress.cancelled) / project.progress.total) * 100);
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

<div class="flex h-full min-w-0 flex-col overflow-y-auto">
	<div class="flex min-h-[49px] items-center justify-between gap-2 border-b border-[var(--app-border)] px-3 sm:px-4">
		<div class="flex items-center gap-2">
			<SidebarToggle />
			<h1 class="text-sm font-medium text-[var(--color-text-primary)]">{m['projects.title']()}</h1>
		</div>
		{#if canManage}
			<button
				onclick={() => (showCreateProject = true)}
				class="rounded-md p-1 text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]"
				title={m['projects.new_project']()}
			>
				<Plus size={16} />
			</button>
		{/if}
	</div>

	{#if loading}
		<div role="status" class="animate-pulse p-4 text-sm text-[var(--color-text-tertiary)]">{m['common.loading']()}</div>
	{:else if loadFailed}
		<EmptyState
			title={m['projects.failed_load']()}
			action={{ label: m['scopedIssues.retry'](), onclick: () => retryVersion++ }}
		/>
	{:else if projects.length === 0}
		<EmptyState
			title={m['projects.no_projects']()}
			description={m['projects.no_projects_desc']()}
			action={canManage ? { label: m['projects.new_project'](), onclick: () => (showCreateProject = true) } : undefined}
		/>
	{:else}
		<div class="divide-y divide-[var(--app-border)]">
			{#each projects as project}
				<a
					href="/{slug}/projects/{project.id}"
					class="flex items-center gap-3 px-3 py-3 sm:gap-4 sm:px-4 hover:bg-[var(--color-bg-hover)]"
				>
					<div class="flex-1 min-w-0">
						<div class="flex items-center gap-2">
							<span class="min-w-0 truncate text-sm font-medium text-[var(--color-text-primary)]">{project.name}</span>
							<Badge variant={statusVariant(project.status)} class="shrink-0 text-[10px]">
								{statusLabel(project.status)}
							</Badge>
							{#if project.team_id}
								{@const team = teams.find((t) => t.id === project.team_id)}
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
							<div
								class="relative hidden h-1.5 w-24 sm:block overflow-hidden rounded-full bg-[var(--color-bg-tertiary)]"
							>
								<div
									class="absolute left-0 top-0 h-full rounded-full bg-[var(--color-success)]"
									style="width: {progressPercentage(project)}%"
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

{#if canManage}
	<CreateProjectDialog bind:open={showCreateProject} {teams} onsubmit={handleCreate} />
{/if}
