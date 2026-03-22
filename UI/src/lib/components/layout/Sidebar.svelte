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
		Search,
		PanelLeftOpen
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

	// ── Resize & collapse logic ──
	const DEFAULT_WIDTH = 240;
	const MIN_WIDTH = 200;
	const MAX_WIDTH = 320;

	let sidebarWidth = $state(
		typeof localStorage !== 'undefined'
			? parseInt(localStorage.getItem('sidebar_width') || String(DEFAULT_WIDTH), 10)
			: DEFAULT_WIDTH
	);
	let collapsed = $state(
		typeof localStorage !== 'undefined'
			? localStorage.getItem('sidebar_collapsed_panel') === 'true'
			: false
	);
	let dragging = $state(false);
	let didDrag = $state(false);
	let hoveringHandle = $state(false);

	// The rendered width: either the full sidebar width or 0 when collapsed
	const renderedWidth = $derived(collapsed ? 0 : sidebarWidth);

	function persistWidth() {
		localStorage.setItem('sidebar_width', String(sidebarWidth));
	}

	function toggleCollapse() {
		collapsed = !collapsed;
		localStorage.setItem('sidebar_collapsed_panel', String(collapsed));
	}

	function expand() {
		collapsed = false;
		localStorage.setItem('sidebar_collapsed_panel', 'false');
	}

	function onPointerDown(e: PointerEvent) {
		e.preventDefault();
		dragging = true;
		didDrag = false;
		const startX = e.clientX;
		const startWidth = sidebarWidth;

		function onPointerMove(ev: PointerEvent) {
			const delta = ev.clientX - startX;
			if (Math.abs(delta) > 2) didDrag = true;
			const newWidth = Math.min(MAX_WIDTH, Math.max(MIN_WIDTH, startWidth + delta));
			sidebarWidth = newWidth;
		}

		function onPointerUp() {
			dragging = false;
			document.removeEventListener('pointermove', onPointerMove);
			document.removeEventListener('pointerup', onPointerUp);
			if (!didDrag) {
				// Plain click (no drag movement) → collapse/expand
				toggleCollapse();
			} else {
				persistWidth();
			}
		}

		document.addEventListener('pointermove', onPointerMove);
		document.addEventListener('pointerup', onPointerUp);
	}

	function onHandleDblClick() {
		sidebarWidth = DEFAULT_WIDTH;
		persistWidth();
		if (collapsed) expand();
	}
</script>

<aside
	class="relative flex h-full flex-col overflow-hidden border-r border-[var(--app-border)] bg-[var(--color-bg-secondary)]"
	style="width: {renderedWidth}px; min-width: 0; transition: {dragging ? 'none' : 'width 200ms cubic-bezier(0.4, 0, 0.2, 1)'};"
>
	<!-- Inner container keeps content at the sidebar's natural width so it doesn't squish -->
	<div class="flex h-full flex-col" style="width: {sidebarWidth}px; min-width: {sidebarWidth}px;">
		<!-- Workspace header -->
		<div class="flex h-[49px] items-center border-b border-[var(--app-border)] px-3">
			<WorkspaceSwitcher currentWorkspace={workspace} {slug} />
			<div class="ml-auto flex items-center gap-1">
				{#if onsearch}
					<button
						onclick={onsearch}
						class="rounded-md p-1 text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]"
						title="Search"
					>
						<Search size={16} />
					</button>
				{/if}
				{#if oncreateissue}
					<button
						onclick={oncreateissue}
						class="rounded-md p-1 text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]"
						title="New issue"
					>
						<SquarePen size={16} />
					</button>
				{/if}
			</div>
		</div>

		<!-- Navigation -->
		<nav class="flex-1 overflow-y-auto overflow-x-hidden px-2 py-2">
			<div class="space-y-px">
				<a
					href="/{slug}/inbox"
					class="flex items-center gap-2 rounded-md px-2 py-1 text-sm {isActive(`/${slug}/inbox`)
						? 'bg-[var(--color-bg-hover)]/50 text-[var(--color-text-primary)]'
						: 'text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]'}"
				>
					<Inbox size={16} class="shrink-0" />
					<span class="truncate">Inbox</span>
					{#if unreadCount > 0}
						<span class="ml-auto flex h-4 min-w-4 items-center justify-center rounded-full bg-[var(--app-accent)] px-1 text-[10px] font-medium text-white">
							{unreadCount > 99 ? '99+' : unreadCount}
						</span>
					{/if}
				</a>
				<a
					href="/{slug}/my-issues"
					class="flex items-center gap-2 rounded-md px-2 py-1 text-sm {isActive(
						`/${slug}/my-issues`
					)
						? 'bg-[var(--color-bg-hover)]/50 text-[var(--color-text-primary)]'
						: 'text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]'}"
				>
					<CircleUser size={16} class="shrink-0" />
					<span class="truncate">My Issues</span>
				</a>
				<a
					href="/{slug}/dashboard"
					class="flex items-center gap-2 rounded-md px-2 py-1 text-sm {isActive(
						`/${slug}/dashboard`
					)
						? 'bg-[var(--color-bg-hover)]/50 text-[var(--color-text-primary)]'
						: 'text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]'}"
				>
					<LayoutDashboard size={16} class="shrink-0" />
					<span class="truncate">Dashboard</span>
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
								class="flex items-center gap-2 rounded-md px-2 py-1 text-sm {isActive(href)
									? 'bg-[var(--color-bg-hover)]/50 text-[var(--color-text-primary)]'
									: 'text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]'}"
							>
								<Star size={14} class="shrink-0 text-yellow-500" />
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
							class="flex w-full items-center gap-2 rounded-md px-2 py-1 text-sm text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]"
						>
							<ChevronDown size={12} class="shrink-0 text-[var(--color-text-tertiary)] transition-transform {teamExpanded ? '' : '-rotate-90'}" />
							<Users size={16} class="shrink-0" />
							<span class="truncate">{team.name}</span>
						</button>
						{#if teamExpanded}
							<a
								href="/{slug}/teams/{team.id}"
								class="ml-7 flex items-center gap-2 rounded-md px-2 py-1 text-xs {isActive(
									`/${slug}/teams/${team.id}`
								) && !isActive(`/${slug}/teams/${team.id}/cycles`) && !isActive(`/${slug}/teams/${team.id}/triage`) && !isActive(`/${slug}/teams/${team.id}/projects`) && !isActive(`/${slug}/teams/${team.id}/views`)
									? 'bg-[var(--color-bg-hover)]/50 text-[var(--color-text-primary)]'
									: 'text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]'}"
							>
								<CircleDot size={13} />
								Issues
							</a>
							<a
								href="/{slug}/teams/{team.id}/cycles"
								class="ml-7 flex items-center gap-2 rounded-md px-2 py-1 text-xs {isActive(
									`/${slug}/teams/${team.id}/cycles`
								)
									? 'bg-[var(--color-bg-hover)]/50 text-[var(--color-text-primary)]'
									: 'text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]'}"
							>
								<RotateCcw size={13} />
								Cycles
							</a>
							<a
								href="/{slug}/teams/{team.id}/projects"
								class="ml-7 flex items-center gap-2 rounded-md px-2 py-1 text-xs {isActive(
									`/${slug}/teams/${team.id}/projects`
								)
									? 'bg-[var(--color-bg-hover)]/50 text-[var(--color-text-primary)]'
									: 'text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]'}"
							>
								<FolderKanban size={13} />
								Projects
							</a>
							<a
								href="/{slug}/teams/{team.id}/views"
								class="ml-7 flex items-center gap-2 rounded-md px-2 py-1 text-xs {isActive(
									`/${slug}/teams/${team.id}/views`
								)
									? 'bg-[var(--color-bg-hover)]/50 text-[var(--color-text-primary)]'
									: 'text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]'}"
							>
								<Bookmark size={13} />
								Views
							</a>
							{#if team.triage_enabled}
								<a
									href="/{slug}/teams/{team.id}/triage"
									class="ml-7 flex items-center gap-2 rounded-md px-2 py-1 text-xs {isActive(
										`/${slug}/teams/${team.id}/triage`
									)
										? 'bg-[var(--color-bg-hover)]/50 text-[var(--color-text-primary)]'
										: 'text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]'}"
								>
									<ShieldCheck size={13} />
									Triage
								</a>
							{/if}
							{#each teamProjects as project}
								<a
									href="/{slug}/projects/{project.id}"
									class="ml-7 flex items-center gap-2 rounded-md px-2 py-1 text-xs {isActive(`/${slug}/projects/${project.id}`)
										? 'bg-[var(--color-bg-hover)]/50 text-[var(--color-text-primary)]'
										: 'text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]'}"
								>
									<FolderKanban size={13} />
									{project.name}
								</a>
							{/each}
							{#each teamViews as view}
								<a
									href="/{slug}/views/{view.id}"
									class="ml-7 flex items-center gap-2 rounded-md px-2 py-1 text-xs {isActive(`/${slug}/views/${view.id}`)
										? 'bg-[var(--color-bg-hover)]/50 text-[var(--color-text-primary)]'
										: 'text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]'}"
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
							class="flex w-full items-center gap-2 rounded-md px-2 py-1 text-sm text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]"
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
								class="flex items-center gap-2 rounded-md px-2 py-1 text-sm {isActive(
									`/${slug}/views/${view.id}`
								)
									? 'bg-[var(--color-bg-hover)]/50 text-[var(--color-text-primary)]'
									: 'text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]'}"
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
						class="flex items-center gap-2 rounded-md px-2 py-1 text-sm {isActive(`/${slug}/projects`)
							? 'bg-[var(--color-bg-hover)]/50 text-[var(--color-text-primary)]'
							: 'text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]'}"
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
				class="flex items-center gap-2 rounded-md px-2 py-1 text-sm text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]"
			>
				<Settings size={16} />
				Settings
			</a>
			<button
				onclick={handleLogout}
				class="flex w-full items-center gap-2 rounded-md px-2 py-1 text-sm text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]"
			>
				<LogOut size={16} />
				Log out
			</button>
		</div>
	</div>

	<!-- Resize handle (always rendered, sits on the right edge) -->
	<!-- svelte-ignore a11y_no_static_element_interactions -->
	<div
		class="absolute top-0 right-0 z-10 h-full w-[8px] translate-x-1/2 cursor-col-resize"
		onpointerdown={onPointerDown}
		ondblclick={onHandleDblClick}
		onmouseenter={() => hoveringHandle = true}
		onmouseleave={() => hoveringHandle = false}
		title="Drag to resize &#10;Click to collapse"
	>
		<div class="mx-auto h-full w-[3px] rounded-full transition-colors {hoveringHandle || dragging ? 'bg-[var(--app-border-hover)]' : 'bg-transparent'}"></div>
	</div>
</aside>

<!-- Expand button shown over the collapsed edge -->
{#if collapsed}
	<button
		onclick={expand}
		class="fixed left-1 top-1/2 z-20 -translate-y-1/2 rounded-md border border-[var(--app-border)] bg-[var(--color-bg-secondary)] p-1.5 text-[var(--color-text-tertiary)] shadow-sm hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)] transition-opacity"
		title="Expand sidebar"
	>
		<PanelLeftOpen size={16} />
	</button>
{/if}

{#if didDrag}
	<div class="fixed inset-0 z-50 cursor-col-resize" style="user-select: none;"></div>
{/if}
