<script lang="ts">
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { logout } from '$lib/api/auth';
	import { authState } from '$lib/features/auth/auth.state.svelte';
	import type { Workspace } from '$lib/types/workspace';
	import type { Team } from '$lib/types/team';
	import type { View } from '$lib/types/view';
	import WorkspaceSwitcher from './WorkspaceSwitcher.svelte';
	import type { Favorite } from '$lib/api/favorites';
	import type { Project } from '$lib/types/project';
	import {
		Inbox,
		LayoutDashboard,
		CircleUser,
		Settings,
		LogOut,
		Users,
		FolderKanban,
		Plus,
		Bookmark,
		RotateCcw,
		ShieldCheck,
		Star,
		ChevronDown,
		CircleDot,
		SquarePen,
		Search
	} from 'lucide-svelte';

	let {
		workspace,
		teams,
		views = [],
		favorites = [],
		projects = [],
		unreadCount = 0,
		slug,
		oncreateissue,
		oncreateteam,
		onsearch
	}: {
		workspace: Workspace;
		teams: Team[];
		views?: View[];
		favorites?: Favorite[];
		projects?: Project[];
		unreadCount?: number;
		slug: string;
		oncreateissue?: () => void;
		oncreateteam?: () => void;
		onsearch?: () => void;
	} = $props();

	const currentPath = $derived(page.url.pathname);

	function isActive(path: string): boolean {
		return currentPath.startsWith(path);
	}

	async function handleLogout() {
		await logout();
		authState.clear();
		goto('/login');
	}

	function initCollapsed(key: string): boolean {
		if (typeof localStorage === 'undefined') return false;
		return localStorage.getItem(`sidebar_${key}`) === 'collapsed';
	}

	function toggleSection(key: string, current: boolean): boolean {
		const next = !current;
		localStorage.setItem(`sidebar_${key}`, next ? 'collapsed' : 'expanded');
		return next;
	}

	let teamsCollapsed = $state(initCollapsed('teams'));
	let favoritesCollapsed = $state(initCollapsed('favorites'));
	let viewsCollapsed = $state(initCollapsed('views'));
	let projectsCollapsed = $state(initCollapsed('projects'));

	// Per-team collapsed state
	let collapsedTeams = $state<Set<string>>(new Set(
		typeof localStorage !== 'undefined'
			? JSON.parse(localStorage.getItem('sidebar_collapsed_teams') || '[]')
			: []
	));

	function toggleTeam(teamId: string) {
		const next = new Set(collapsedTeams);
		if (next.has(teamId)) {
			next.delete(teamId);
		} else {
			next.add(teamId);
		}
		collapsedTeams = next;
		localStorage.setItem('sidebar_collapsed_teams', JSON.stringify([...next]));
	}
</script>

<aside
	class="flex h-full w-60 flex-col border-r border-[var(--app-border)] bg-[var(--color-bg-secondary)]"
>
	<!-- Workspace header -->
	<div class="flex h-[49px] items-center border-b border-[var(--app-border)] px-3">
		<WorkspaceSwitcher currentWorkspace={workspace} {slug} />
		<div class="ml-auto flex items-center gap-1">
			{#if onsearch}
				<button
					onclick={onsearch}
					class="rounded-md p-1 text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)]"
					title="Search"
				>
					<Search size={16} />
				</button>
			{/if}
			{#if oncreateissue}
				<button
					onclick={oncreateissue}
					class="rounded-md p-1 text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)]"
					title="New issue"
				>
					<SquarePen size={16} />
				</button>
			{/if}
		</div>
	</div>

	<!-- Navigation -->
	<nav class="flex-1 overflow-y-auto px-2 py-2">
		<div class="space-y-0.5">
			<a
				href="/{slug}/inbox"
				class="flex items-center gap-2 rounded-md px-2 py-1.5 text-sm {isActive(`/${slug}/inbox`)
					? 'bg-[var(--color-bg-hover)] text-[var(--color-text-primary)]'
					: 'text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)]'}"
			>
				<Inbox size={16} />
				Inbox
				{#if unreadCount > 0}
					<span class="ml-auto flex h-4 min-w-4 items-center justify-center rounded-full bg-[var(--app-accent)] px-1 text-[10px] font-medium text-white">
						{unreadCount > 99 ? '99+' : unreadCount}
					</span>
				{/if}
			</a>
			<a
				href="/{slug}/my-issues"
				class="flex items-center gap-2 rounded-md px-2 py-1.5 text-sm {isActive(
					`/${slug}/my-issues`
				)
					? 'bg-[var(--color-bg-hover)] text-[var(--color-text-primary)]'
					: 'text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)]'}"
			>
				<CircleUser size={16} />
				My Issues
			</a>
			<a
				href="/{slug}/dashboard"
				class="flex items-center gap-2 rounded-md px-2 py-1.5 text-sm {isActive(
					`/${slug}/dashboard`
				)
					? 'bg-[var(--color-bg-hover)] text-[var(--color-text-primary)]'
					: 'text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)]'}"
			>
				<LayoutDashboard size={16} />
				Dashboard
			</a>
		</div>

		<!-- Favorites -->
		{#if favorites.length > 0}
			<div class="mt-4">
				<button onclick={() => favoritesCollapsed = toggleSection('favorites', favoritesCollapsed)} class="flex w-full items-center justify-between px-2 py-1">
					<span class="text-xs font-medium uppercase text-[var(--color-text-tertiary)]">Favorites</span>
					<ChevronDown size={12} class="text-[var(--color-text-tertiary)] transition-transform {favoritesCollapsed ? '-rotate-90' : ''}" />
				</button>
				{#if !favoritesCollapsed}
					{#each favorites as fav}
						{@const href = fav.entity_type === 'project' ? `/${slug}/projects/${fav.entity_id}` : fav.entity_type === 'team' ? `/${slug}/teams/${fav.entity_id}` : fav.entity_type === 'view' ? `/${slug}/views/${fav.entity_id}` : `/${slug}/dashboard`}
						<a
							{href}
							class="flex items-center gap-2 rounded-md px-2 py-1.5 text-sm {isActive(href)
								? 'bg-[var(--color-bg-hover)] text-[var(--color-text-primary)]'
								: 'text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)]'}"
						>
							<Star size={14} class="text-yellow-500" />
							<span class="truncate">{fav.entity_type}</span>
						</a>
					{/each}
				{/if}
			</div>
		{/if}

		<!-- Teams -->
		<div class="mt-4">
			<div class="flex items-center justify-between px-2 py-1">
				<button onclick={() => teamsCollapsed = toggleSection('teams', teamsCollapsed)} class="flex items-center gap-1">
					<span class="text-xs font-medium uppercase text-[var(--color-text-tertiary)]">Teams</span>
					<ChevronDown size={12} class="text-[var(--color-text-tertiary)] transition-transform {teamsCollapsed ? '-rotate-90' : ''}" />
				</button>
				{#if oncreateteam}
					<button
						onclick={oncreateteam}
						class="rounded p-0.5 text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-secondary)]"
						title="Create team"
					>
						<Plus size={14} />
					</button>
				{/if}
			</div>
			{#if !teamsCollapsed}
				{#each teams as team}
					{@const teamExpanded = !collapsedTeams.has(team.id)}
					{@const teamProjects = projects.filter(p => p.team_id === team.id)}
					{@const teamViews = views.filter(v => v.filters?.team === team.id)}
					<button
						onclick={() => toggleTeam(team.id)}
						class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)]"
					>
						<ChevronDown size={12} class="shrink-0 text-[var(--color-text-tertiary)] transition-transform {teamExpanded ? '' : '-rotate-90'}" />
						<Users size={16} class="shrink-0" />
						<span class="truncate">{team.name}</span>
					</button>
					{#if teamExpanded}
						<a
							href="/{slug}/teams/{team.id}"
							class="flex items-center gap-2 rounded-md px-2 py-1.5 pl-8 text-xs {isActive(
								`/${slug}/teams/${team.id}`
							) && !isActive(`/${slug}/teams/${team.id}/cycles`) && !isActive(`/${slug}/teams/${team.id}/triage`)
								? 'bg-[var(--color-bg-hover)] text-[var(--color-text-primary)]'
								: 'text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)]'}"
						>
							<CircleDot size={13} />
							Issues
						</a>
						<a
							href="/{slug}/teams/{team.id}/cycles"
							class="flex items-center gap-2 rounded-md px-2 py-1.5 pl-8 text-xs {isActive(
								`/${slug}/teams/${team.id}/cycles`
							)
								? 'bg-[var(--color-bg-hover)] text-[var(--color-text-primary)]'
								: 'text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)]'}"
						>
							<RotateCcw size={13} />
							Cycles
						</a>
						{#if team.triage_enabled}
							<a
								href="/{slug}/teams/{team.id}/triage"
								class="flex items-center gap-2 rounded-md px-2 py-1.5 pl-8 text-xs {isActive(
									`/${slug}/teams/${team.id}/triage`
								)
									? 'bg-[var(--color-bg-hover)] text-[var(--color-text-primary)]'
									: 'text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)]'}"
							>
								<ShieldCheck size={13} />
								Triage
							</a>
						{/if}
						{#each teamProjects as project}
							<a
								href="/{slug}/projects/{project.id}"
								class="flex items-center gap-2 rounded-md px-2 py-1.5 pl-8 text-xs {isActive(`/${slug}/projects/${project.id}`)
									? 'bg-[var(--color-bg-hover)] text-[var(--color-text-primary)]'
									: 'text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)]'}"
							>
								<FolderKanban size={13} />
								{project.name}
							</a>
						{/each}
						{#each teamViews as view}
							<a
								href="/{slug}/views/{view.id}"
								class="flex items-center gap-2 rounded-md px-2 py-1.5 pl-8 text-xs {isActive(`/${slug}/views/${view.id}`)
									? 'bg-[var(--color-bg-hover)] text-[var(--color-text-primary)]'
									: 'text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)]'}"
							>
								<Bookmark size={13} />
								{view.name}
							</a>
						{/each}
					{/if}
				{/each}
				{#if teams.length === 0}
					<button
						onclick={oncreateteam}
						class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)]"
					>
						<Plus size={14} />
						Create your first team
					</button>
				{/if}
			{/if}
		</div>

		<!-- Views -->
		{#if views.length > 0}
			<div class="mt-4">
				<button onclick={() => viewsCollapsed = toggleSection('views', viewsCollapsed)} class="flex w-full items-center justify-between px-2 py-1">
					<span class="text-xs font-medium uppercase text-[var(--color-text-tertiary)]">Views</span>
					<ChevronDown size={12} class="text-[var(--color-text-tertiary)] transition-transform {viewsCollapsed ? '-rotate-90' : ''}" />
				</button>
				{#if !viewsCollapsed}
					{#each views as view}
						<a
							href="/{slug}/views/{view.id}"
							class="flex items-center gap-2 rounded-md px-2 py-1.5 text-sm {isActive(
								`/${slug}/views/${view.id}`
							)
								? 'bg-[var(--color-bg-hover)] text-[var(--color-text-primary)]'
								: 'text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)]'}"
						>
							<Bookmark size={16} />
							{view.name}
						</a>
					{/each}
				{/if}
			</div>
		{/if}

		<!-- Projects -->
		<div class="mt-4">
			<div class="flex items-center justify-between px-2 py-1">
				<button onclick={() => projectsCollapsed = toggleSection('projects', projectsCollapsed)} class="flex items-center gap-1">
					<span class="text-xs font-medium uppercase text-[var(--color-text-tertiary)]">Projects</span>
					<ChevronDown size={12} class="text-[var(--color-text-tertiary)] transition-transform {projectsCollapsed ? '-rotate-90' : ''}" />
				</button>
			</div>
			{#if !projectsCollapsed}
				<a
					href="/{slug}/projects"
					class="flex items-center gap-2 rounded-md px-2 py-1.5 text-sm {isActive(`/${slug}/projects`)
						? 'bg-[var(--color-bg-hover)] text-[var(--color-text-primary)]'
						: 'text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)]'}"
				>
					<FolderKanban size={16} />
					All Projects
				</a>
			{/if}
		</div>
	</nav>

	<!-- Footer -->
	<div class="border-t border-[var(--app-border)] px-2 py-2">
		<a
			href="/{slug}/settings"
			class="flex items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)]"
		>
			<Settings size={16} />
			Settings
		</a>
		<button
			onclick={handleLogout}
			class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)]"
		>
			<LogOut size={16} />
			Log out
		</button>
	</div>
</aside>
