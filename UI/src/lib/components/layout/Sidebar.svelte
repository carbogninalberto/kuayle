<script lang="ts">
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { flip } from 'svelte/animate';
	import { cubicOut } from 'svelte/easing';
	import { logout } from '$lib/api/auth';

	function slideFade(node: HTMLElement, params: { duration?: number } = {}) {
		const duration = params.duration ?? 200;
		const h = node.offsetHeight;
		return {
			duration,
			easing: cubicOut,
			css: (t: number) => `overflow: hidden; height: ${t * h}px; opacity: ${t};`
		};
	}
	import { authState } from '$lib/features/auth/auth.state.svelte';
	import type { Workspace } from '$lib/types/workspace';
	import type { Team } from '$lib/types/team';
	import type { View } from '$lib/types/view';
	import { isPersonalView, isTeamView, isWorkspaceView } from '$lib/types/view';
	import TeamIcon from '$lib/components/shared/TeamIcon.svelte';
	import WorkspaceSwitcher from './WorkspaceSwitcher.svelte';
	import type { Favorite } from '$lib/api/favorites';
	import type { Project } from '$lib/types/project';
	import { sidebarState } from '$lib/features/layout/sidebar.state.svelte';
	import { deleteProject } from '$lib/api/projects';
	import { deleteView } from '$lib/api/views';
	import { currentReleaseUrl, currentVersionLabel } from '$lib/release';
	import * as Popover from '$lib/components/ui/popover';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu';
	import * as ContextMenu from '$lib/components/ui/context-menu';
	import * as AlertDialog from '$lib/components/ui/alert-dialog';
	import { dndzone } from 'svelte-dnd-action';
	import { appToast } from '$lib/features/toast/toast';
	import {
		ArrowUpRight,
		Bookmark,
		Copy,
		Inbox,
		CircleUser,
		Settings,
		LogOut,
		SquareUser,
		Box,
		Plus,
		Layers,
		RefreshCcwDot,
		ShieldCheck,
		Star,
		ChevronDown,
		SquaresSubtract,
		SquarePen,
		Search,
		Ellipsis,
		Trash2,
		Info,
		CircleQuestionMark
	} from 'lucide-svelte';

	let {
		workspace,
		teams,
		views = [],
		favorites = [],
		projects = [],
		unreadCount = 0,
		slug,
		mobile = false,
		oncreateissue,
		oncreateteam,
		onleaveteam,
		ondeleteteam,
		onsearch,
		onnavigate,
		onshortcutshelp
	}: {
		workspace: Workspace;
		teams: Team[];
		views?: View[];
		favorites?: Favorite[];
		projects?: Project[];
		unreadCount?: number;
		slug: string;
		mobile?: boolean;
		oncreateissue?: () => void;
		oncreateteam?: () => void;
		onleaveteam?: (team: Team) => void;
		ondeleteteam?: (team: Team) => void;
		onsearch?: () => void;
		onnavigate?: () => void;
		onshortcutshelp?: () => void;
	} = $props();

	const currentPath = $derived(page.url.pathname);
	type SidebarDndEvent<T> = CustomEvent<{
		items: T[];
		info?: { trigger?: string; id?: string };
	}>;

	const sidebarDndFlipMs = 180;
	const dragOptions = {
		flipDurationMs: sidebarDndFlipMs,
		morphDisabled: true,
		dropTargetStyle: { outline: 'none' },
		dropTargetClasses: [],
		transformDraggedElement: transformSidebarDraggedElement
	};
	const menuIconClass = 'mr-2 h-3.5 w-3.5';
	let activeSidebarDragId = $state<string | null>(null);

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

	let collapsedTeams = $state<Set<string>>(new Set());
	let loadedCollapsedTeamsSlug = '';
	let pendingDeleteView = $state<View | null>(null);
	let deleteViewOpen = $state(false);

	function collapsedTeamsStorageKey() {
		return `sidebar_collapsed_teams_${slug}`;
	}

	function loadCollapsedTeamIds(): string[] {
		if (typeof localStorage === 'undefined') return [];
		try {
			const stored =
				localStorage.getItem(collapsedTeamsStorageKey()) ?? localStorage.getItem('sidebar_collapsed_teams') ?? '[]';
			const value = JSON.parse(stored);
			return Array.isArray(value) ? value.filter((id): id is string => typeof id === 'string') : [];
		} catch {
			return [];
		}
	}

	$effect(() => {
		if (!slug || loadedCollapsedTeamsSlug === slug) return;
		loadedCollapsedTeamsSlug = slug;
		collapsedTeams = new Set(loadCollapsedTeamIds());
	});

	function toggleTeam(teamId: string) {
		const next = new Set(collapsedTeams);
		if (next.has(teamId)) {
			next.delete(teamId);
		} else {
			next.add(teamId);
		}
		collapsedTeams = next;
		localStorage.setItem(collapsedTeamsStorageKey(), JSON.stringify([...next]));
	}

	function orderStorageKey(kind: 'teams' | 'views' | 'projects') {
		return `sidebar_order_${slug}_${kind}`;
	}

	function loadOrder(kind: 'teams' | 'views' | 'projects'): string[] {
		if (typeof localStorage === 'undefined') return [];
		try {
			const value = JSON.parse(localStorage.getItem(orderStorageKey(kind)) || '[]');
			return Array.isArray(value) ? value.filter((id): id is string => typeof id === 'string') : [];
		} catch {
			return [];
		}
	}

	function saveOrder(kind: 'teams' | 'views' | 'projects', ids: string[]) {
		if (typeof localStorage === 'undefined') return;
		localStorage.setItem(orderStorageKey(kind), JSON.stringify(ids));
	}

	function applyStoredOrder<T extends { id: string }>(items: T[], kind: 'teams' | 'views' | 'projects'): T[] {
		const order = loadOrder(kind);
		if (order.length === 0) return [...items];
		const index = new Map(order.map((id, i) => [id, i]));
		return [...items].sort((a, b) => {
			const aIndex = index.get(a.id);
			const bIndex = index.get(b.id);
			if (aIndex === undefined && bIndex === undefined) return 0;
			if (aIndex === undefined) return 1;
			if (bIndex === undefined) return -1;
			return aIndex - bIndex;
		});
	}

	function replaceOrderedGroup<T extends { id: string }>(items: T[], groupItems: T[]): T[] {
		const groupIds = new Set(groupItems.map((item) => item.id));
		const firstGroupIndex = items.findIndex((item) => groupIds.has(item.id));
		const next = items.filter((item) => !groupIds.has(item.id));
		next.splice(firstGroupIndex < 0 ? next.length : firstGroupIndex, 0, ...groupItems);
		return next;
	}

	let orderedTeams = $state<Team[]>([]);
	let orderedViews = $state<View[]>([]);
	let orderedProjects = $state<Project[]>([]);
	let personalViews = $derived(orderedViews.filter(isPersonalView));
	let workspaceViews = $derived(orderedViews.filter(isWorkspaceView));

	function getTranslateY(transform: string): string {
		const match = transform.match(/translate3d\([^,]+,\s*([^,]+),/);
		return match?.[1] ?? '0px';
	}

	function lockDraggedElementToVerticalAxis(draggedEl: HTMLElement) {
		const translateY = getTranslateY(draggedEl.style.transform);
		draggedEl.style.transform = `translate3d(0px, ${translateY}, 0)`;
	}

	function transformSidebarDraggedElement(draggedEl?: HTMLElement) {
		if (!draggedEl) return;
		const row = draggedEl.querySelector<HTMLElement>('[data-sidebar-drag-row]');
		const rowHeight = row?.getBoundingClientRect().height;

		draggedEl.style.outline = 'none';
		draggedEl.style.boxShadow = 'none';
		draggedEl.style.overflow = 'hidden';
		draggedEl.style.pointerEvents = 'none';
		draggedEl.style.opacity = '0.92';
		if (rowHeight) draggedEl.style.height = `${rowHeight}px`;

		if (draggedEl.dataset.sidebarAxisLocked === 'true') return;
		draggedEl.dataset.sidebarAxisLocked = 'true';

		const handleMove = () => lockDraggedElementToVerticalAxis(draggedEl);
		const handleEnd = () => {
			window.removeEventListener('mousemove', handleMove);
			window.removeEventListener('touchmove', handleMove);
			window.removeEventListener('mouseup', handleEnd);
			window.removeEventListener('touchend', handleEnd);
		};

		setTimeout(() => {
			window.addEventListener('mousemove', handleMove);
			window.addEventListener('touchmove', handleMove);
			window.addEventListener('mouseup', handleEnd, { once: true });
			window.addEventListener('touchend', handleEnd, { once: true });
		}, 0);
	}

	function updateActiveSidebarDrag<T>(e: SidebarDndEvent<T>) {
		if (e.detail.info?.trigger === 'dragStarted' && e.detail.info.id) {
			activeSidebarDragId = e.detail.info.id;
		}
	}

	function clearActiveSidebarDrag() {
		activeSidebarDragId = null;
	}

	$effect(() => {
		const nextTeams = applyStoredOrder(teams, 'teams');
		orderedTeams = nextTeams;
		sidebarState.teams = nextTeams;
	});

	$effect(() => {
		orderedViews = applyStoredOrder(views, 'views');
	});

	$effect(() => {
		const nextProjects = applyStoredOrder(projects, 'projects').sort((a, b) => {
			const order = loadOrder('projects');
			if (order.length > 0) return 0;
			return (a.sort_order ?? 0) - (b.sort_order ?? 0);
		});
		orderedProjects = nextProjects;
		sidebarState.projects = nextProjects;
	});

	function handleTeamsConsider(e: SidebarDndEvent<Team>) {
		updateActiveSidebarDrag(e);
		orderedTeams = e.detail.items;
	}

	function handleTeamsFinalize(e: SidebarDndEvent<Team>) {
		orderedTeams = e.detail.items;
		saveOrder(
			'teams',
			orderedTeams.map((team) => team.id)
		);
		sidebarState.teams = orderedTeams;
		clearActiveSidebarDrag();
	}

	function handleViewsConsider(e: SidebarDndEvent<View>) {
		updateActiveSidebarDrag(e);
		orderedViews = replaceOrderedGroup(orderedViews, e.detail.items);
	}

	function handleViewsFinalize(e: SidebarDndEvent<View>) {
		orderedViews = replaceOrderedGroup(orderedViews, e.detail.items);
		saveOrder(
			'views',
			orderedViews.map((view) => view.id)
		);
		clearActiveSidebarDrag();
	}

	function handleTeamViewsConsider(e: SidebarDndEvent<View>) {
		updateActiveSidebarDrag(e);
		orderedViews = replaceOrderedGroup(orderedViews, e.detail.items);
	}

	function handleTeamViewsFinalize(e: SidebarDndEvent<View>) {
		orderedViews = replaceOrderedGroup(orderedViews, e.detail.items);
		saveOrder(
			'views',
			orderedViews.map((view) => view.id)
		);
		clearActiveSidebarDrag();
	}

	function handleProjectsConsider(e: SidebarDndEvent<Project>) {
		updateActiveSidebarDrag(e);
		orderedProjects = replaceOrderedGroup(orderedProjects, e.detail.items);
		sidebarState.projects = orderedProjects;
	}

	function handleProjectsFinalize(e: SidebarDndEvent<Project>) {
		orderedProjects = replaceOrderedGroup(orderedProjects, e.detail.items);
		saveOrder(
			'projects',
			orderedProjects.map((project) => project.id)
		);
		sidebarState.projects = orderedProjects;
		clearActiveSidebarDrag();
	}

	function copyLink(path: string) {
		navigator.clipboard.writeText(`${window.location.origin}${path}`);
		appToast.success('Link copied');
	}

	function requestDeleteView(view: View) {
		pendingDeleteView = view;
		deleteViewOpen = true;
	}

	async function handleDeleteView() {
		const view = pendingDeleteView;
		if (!view) return;
		try {
			await deleteView(slug, view.id);
			views = views.filter((item) => item.id !== view.id);
			orderedViews = orderedViews.filter((item) => item.id !== view.id);
			appToast.success('View deleted');
			if (currentPath === `/${slug}/views/${view.id}`) goto(`/${slug}/my-issues`);
		} catch (err: any) {
			appToast.apiError(err, 'Failed to delete view');
		} finally {
			deleteViewOpen = false;
			pendingDeleteView = null;
		}
	}

	async function handleDeleteProject(project: Project) {
		try {
			await deleteProject(slug, project.id);
			projects = projects.filter((item) => item.id !== project.id);
			orderedProjects = orderedProjects.filter((item) => item.id !== project.id);
			sidebarState.projects = orderedProjects;
			appToast.success('Project deleted');
			if (currentPath === `/${slug}/projects/${project.id}`) goto(`/${slug}/projects`);
		} catch (err: any) {
			appToast.apiError(err, 'Failed to delete project');
		}
	}

	// ── Resize & collapse logic ──
	const DEFAULT_WIDTH = 240;
	const MIN_WIDTH = 220;
	const MAX_WIDTH = 320;
	const COLLAPSE_THRESHOLD = 140;

	let sidebarWidth = $state(
		typeof localStorage !== 'undefined'
			? parseInt(localStorage.getItem('sidebar_width') || String(DEFAULT_WIDTH), 10)
			: DEFAULT_WIDTH
	);
	// Trigger expand animation: runs before DOM so content mounts at offset
	let expanding = $state(false);
	let wasCollapsed = sidebarState.collapsed;
	$effect.pre(() => {
		const isCollapsed = sidebarState.collapsed;
		if (wasCollapsed && !isCollapsed) {
			expanding = true;
			requestAnimationFrame(() => {
				requestAnimationFrame(() => {
					expanding = false;
				});
			});
		}
		wasCollapsed = isCollapsed;
	});

	// Close drawer when sidebar expands
	$effect(() => {
		if (!sidebarState.collapsed) drawerOpen = false;
	});

	let dragging = $state(false);
	let didDrag = $state(false);
	let hoveringHandle = $state(false);

	// Drawer state: sidebar shown as overlay when collapsed and mouse enters left edge
	let drawerOpen = $state(false);
	let drawerTimeout: ReturnType<typeof setTimeout> | undefined;
	// Skip the inline sidebar's width transition when pinning from drawer
	let skipTransition = $state(false);

	function openDrawer() {
		clearTimeout(drawerTimeout);
		drawerOpen = true;
	}

	function scheduleCloseDrawer() {
		clearTimeout(drawerTimeout);
		drawerTimeout = setTimeout(() => {
			drawerOpen = false;
		}, 300);
	}

	function cancelCloseDrawer() {
		clearTimeout(drawerTimeout);
	}

	let collapsing = $state(false);
	const ANIM_DURATION = 300;

	const renderedWidth = $derived(sidebarState.collapsed || collapsing ? 0 : sidebarWidth);
	const contentWidth = $derived(Math.max(sidebarWidth, MIN_WIDTH));
	// How far below MIN_WIDTH the user has dragged (0 when above MIN_WIDTH or not dragging)
	const belowMin = $derived(dragging && sidebarWidth < MIN_WIDTH ? MIN_WIDTH - sidebarWidth : 0);
	// Normalized 0..1 progress through the collapse zone, eased for smooth deceleration
	const collapseProgress = $derived(Math.min(1, belowMin / MIN_WIDTH));
	const easedProgress = $derived(1 - Math.pow(1 - collapseProgress, 3)); // ease-out cubic

	// Content slide values: driven by drag, collapsing click, or expanding reverse
	const atOffset = $derived(expanding || collapsing);
	const slideX = $derived(atOffset ? -60 : -easedProgress * 60);
	const slideY = $derived(atOffset ? 48 : easedProgress * 48);
	const slideOpacity = $derived(atOffset ? 0.5 : 1 - easedProgress * 0.5);

	function persistWidth() {
		localStorage.setItem('sidebar_width', String(sidebarWidth));
	}

	function toggleCollapse() {
		if (sidebarState.collapsed) {
			sidebarState.expand();
		} else {
			// Mark collapsed immediately (shows toggle in headers) and animate out
			collapsing = true;
			sidebarState.collapse();
			setTimeout(() => {
				collapsing = false;
			}, ANIM_DURATION);
		}
	}

	function expand() {
		// When pinning from drawer, skip the width animation so it doesn't replay
		skipTransition = true;
		sidebarState.expand();
		requestAnimationFrame(() => {
			skipTransition = false;
		});
	}

	// Eased resize: applies a cubic bezier-like curve so dragging feels weighted.
	// Near the center of the range it moves 1:1, near the edges it decelerates.
	function easedResize(startWidth: number, rawDelta: number): number {
		const range = MAX_WIDTH - MIN_WIDTH;
		const raw = startWidth + rawDelta;
		// Normalize to 0..1 within the allowed range
		const t = Math.max(0, Math.min(1, (raw - MIN_WIDTH) / range));
		// Ease-in-out cubic: smooth deceleration near edges
		const eased = t < 0.5 ? 4 * t * t * t : 1 - Math.pow(-2 * t + 2, 3) / 2;
		return MIN_WIDTH + eased * range;
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
			const raw = startWidth + delta;
			if (raw < COLLAPSE_THRESHOLD) {
				// Visual feedback: shrink towards 0 as user drags left
				sidebarWidth = Math.max(0, raw);
			} else {
				sidebarWidth = Math.round(easedResize(startWidth, delta));
			}
		}

		function onPointerUp() {
			dragging = false;
			document.removeEventListener('pointermove', onPointerMove);
			document.removeEventListener('pointerup', onPointerUp);
			if (!didDrag) {
				toggleCollapse();
			} else if (sidebarWidth < COLLAPSE_THRESHOLD) {
				// Dragged far enough left — collapse
				sidebarWidth = startWidth; // restore for next expand
				sidebarState.collapse();
			} else {
				persistWidth();
			}
			didDrag = false;
		}

		document.addEventListener('pointermove', onPointerMove);
		document.addEventListener('pointerup', onPointerUp);
	}

	function onHandleDblClick() {
		sidebarWidth = DEFAULT_WIDTH;
		persistWidth();
		if (sidebarState.collapsed) expand();
	}
</script>

{#if mobile}
	<div class="flex h-full flex-col bg-[var(--color-bg-secondary)]">
		{@render sidebarContent()}
	</div>
{:else}
	<!-- Inline sidebar (pushes content) -->
	<aside
		class="relative flex h-full shrink-0 flex-col overflow-hidden border-r border-[var(--app-border)] bg-[var(--color-bg-secondary)]"
		style="width: {renderedWidth}px; min-width: 0; transition: {dragging || skipTransition
			? 'none'
			: `width ${ANIM_DURATION}ms cubic-bezier(0.25, 1, 0.5, 1)`};"
	>
		{#if !sidebarState.collapsed || collapsing}
			<div
				class="flex h-full flex-col"
				style="width: {contentWidth}px; min-width: {contentWidth}px; transform: translate({slideX}px, {slideY}px); opacity: {slideOpacity}; transition: {dragging
					? 'none'
					: `transform ${ANIM_DURATION}ms cubic-bezier(0.25, 1, 0.5, 1), opacity ${ANIM_DURATION}ms cubic-bezier(0.25, 1, 0.5, 1)`};"
			>
				{@render sidebarContent()}
			</div>

			<!-- Resize handle -->
			<!-- svelte-ignore a11y_click_events_have_key_events -->
			<!-- svelte-ignore a11y_no_static_element_interactions -->
			<div
				class="absolute top-0 right-0 z-10 h-full w-[8px] translate-x-1/2 cursor-col-resize"
				onpointerdown={onPointerDown}
				ondblclick={onHandleDblClick}
				onmouseenter={() => (hoveringHandle = true)}
				onmouseleave={() => (hoveringHandle = false)}
				title="Drag to resize &#10;Click to collapse"
			>
				<div
					class="mx-auto h-full w-[3px] rounded-full transition-colors {hoveringHandle || dragging
						? 'bg-[var(--app-border-hover)]'
						: 'bg-transparent'}"
				></div>
			</div>
		{/if}
	</aside>

	<!-- When collapsed: hover zone on left edge triggers drawer -->
	{#if sidebarState.collapsed}
		<!-- svelte-ignore a11y_no_static_element_interactions -->
		<div class="fixed left-0 top-0 z-20 h-full w-[6px]" onmouseenter={openDrawer}></div>

		<!-- Drawer overlay sidebar (below page header) -->
		<div
			role="button"
			tabindex="0"
			aria-label="Close sidebar"
			class="fixed inset-0 top-[49px] z-40 transition-[background-color] duration-300 {drawerOpen
				? 'pointer-events-auto'
				: 'pointer-events-none'}"
			style="background-color: {drawerOpen ? 'rgba(0,0,0,0.15)' : 'transparent'};"
			onclick={() => (drawerOpen = false)}
			onkeydown={(e) => {
				if (e.key === 'Enter' || e.key === 'Escape') drawerOpen = false;
			}}
		>
			<div
				role="presentation"
				class="absolute left-0 top-0 h-full flex flex-col border-r border-[var(--app-border)] bg-[var(--color-bg-secondary)] shadow-xl transition-transform duration-300"
				style="width: {sidebarWidth}px; transform: translateX({drawerOpen ? '0' : '-100%'}); will-change: transform;"
				onclick={(e) => e.stopPropagation()}
				onkeydown={(e) => e.stopPropagation()}
				onmouseenter={cancelCloseDrawer}
				onmouseleave={scheduleCloseDrawer}
			>
				{@render sidebarContent()}
			</div>
		</div>
	{/if}

	{#if didDrag}
		<div class="fixed inset-0 z-50 cursor-col-resize" style="user-select: none;"></div>
	{/if}
{/if}

<AlertDialog.Root bind:open={deleteViewOpen}>
	<AlertDialog.Content>
		<AlertDialog.Header>
			<AlertDialog.Title>Delete view?</AlertDialog.Title>
			<AlertDialog.Description>
				This will permanently delete {pendingDeleteView?.name ?? 'this view'}.
			</AlertDialog.Description>
		</AlertDialog.Header>
		<AlertDialog.Footer>
			<AlertDialog.Cancel variant="outline" onclick={() => (pendingDeleteView = null)}>Cancel</AlertDialog.Cancel>
			<AlertDialog.Action variant="destructive" onclick={handleDeleteView}>Delete view</AlertDialog.Action>
		</AlertDialog.Footer>
	</AlertDialog.Content>
</AlertDialog.Root>

{#snippet sidebarContent()}
	<!-- Workspace header -->
	<div class="flex h-[49px] items-center gap-1 px-3">
		<div class="min-w-0 flex-1">
			<WorkspaceSwitcher currentWorkspace={workspace} {slug} />
		</div>
		<div class="flex shrink-0 items-center gap-1">
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
		<!-- svelte-ignore a11y_no_static_element_interactions -->
		<div
			onclick={(e) => {
				if ((e.target as HTMLElement).closest('a')) onnavigate?.();
			}}
			onkeydown={() => {}}
		>
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
						<span
							class="ml-auto flex h-4 min-w-4 items-center justify-center rounded-full bg-[var(--app-accent)] px-1 text-[10px] font-medium text-[var(--app-accent-foreground)]"
						>
							{unreadCount > 99 ? '99+' : unreadCount}
						</span>
					{/if}
				</a>
				<a
					href="/{slug}/my-issues"
					class="flex items-center gap-2 rounded-md px-2 py-1 text-sm {isActive(`/${slug}/my-issues`)
						? 'bg-[var(--color-bg-hover)]/50 text-[var(--color-text-primary)]'
						: 'text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]'}"
				>
					<CircleUser size={16} class="shrink-0" />
					<span class="truncate">My Issues</span>
				</a>
			</div>

			<!-- Favorites -->
			{#if favorites.length > 0}
				<div class="mt-4">
					<button
						onclick={() => (favoritesCollapsed = toggleSection('favorites', favoritesCollapsed))}
						class="flex w-full items-center px-2 py-1"
					>
						<span class="flex items-center gap-1">
							<span class="text-[11px] font-semibold text-[var(--color-text-secondary)]">Favorites</span>
							<ChevronDown
								size={12}
								class="text-[var(--color-text-tertiary)] transition-transform {favoritesCollapsed ? '-rotate-90' : ''}"
							/>
						</span>
					</button>
					{#if !favoritesCollapsed}
						<div transition:slideFade>
							{#each favorites as fav}
								{@const href =
									fav.entity_type === 'project'
										? `/${slug}/projects/${fav.entity_id}`
										: fav.entity_type === 'team'
											? `/${slug}/teams/${fav.entity_id}`
											: fav.entity_type === 'view'
												? `/${slug}/views/${fav.entity_id}`
												: `/${slug}/my-issues`}
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
						</div>
					{/if}
				</div>
			{/if}

			<!-- Teams -->
			<div class="group/teams mt-4">
				<div
					role="button"
					tabindex="0"
					class="flex cursor-pointer items-center justify-between px-2 py-1"
					onclick={() => (teamsCollapsed = toggleSection('teams', teamsCollapsed))}
					onkeydown={(e) => {
						if (e.key === 'Enter' || e.key === ' ') {
							e.preventDefault();
							teamsCollapsed = toggleSection('teams', teamsCollapsed);
						}
					}}
				>
					<span class="flex items-center gap-1">
						<span class="text-[11px] font-semibold text-[var(--color-text-secondary)]">Teams</span>
						<ChevronDown
							size={12}
							class="text-[var(--color-text-tertiary)] transition-transform {teamsCollapsed ? '-rotate-90' : ''}"
						/>
					</span>
					{#if oncreateteam}
						<button
							onclick={(e) => {
								e.stopPropagation();
								oncreateteam?.();
							}}
							class="rounded p-0.5 text-[var(--color-text-tertiary)] opacity-0 transition-opacity group-hover/teams:opacity-100 hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-secondary)]"
							title="Create team"
						>
							<Plus size={14} />
						</button>
					{/if}
				</div>
				{#if !teamsCollapsed}
					<div
						transition:slideFade
						use:dndzone={{ items: orderedTeams, type: 'sidebar-team', ...dragOptions }}
						onconsider={handleTeamsConsider}
						onfinalize={handleTeamsFinalize}
					>
						{#each orderedTeams as team (team.id)}
							{@const teamExpanded = !collapsedTeams.has(team.id)}
							{@const teamProjects = orderedProjects.filter((p) => p.team_id === team.id)}
							{@const teamViews = orderedViews.filter((view) => isTeamView(view, team.id))}
							<div
								animate:flip={{ duration: sidebarDndFlipMs }}
								class="transition-[transform,opacity] duration-150 {activeSidebarDragId === team.id
									? 'opacity-60'
									: ''}"
							>
								<ContextMenu.Root>
									<ContextMenu.Trigger class="block">
										<!-- svelte-ignore a11y_no_static_element_interactions -->
										<div
											data-sidebar-drag-row
											class="group/team flex items-center rounded-md hover:bg-[var(--color-bg-hover)]"
										>
											<button
												onclick={() => toggleTeam(team.id)}
												class="flex min-w-0 flex-1 items-center gap-2 px-2 py-1 text-sm text-[var(--color-text-secondary)] hover:text-[var(--color-text-primary)]"
											>
												<TeamIcon {team} />
												<span class="truncate">{team.name}</span>
												<ChevronDown
													size={12}
													class="shrink-0 text-[var(--color-text-tertiary)] transition-transform {teamExpanded
														? ''
														: '-rotate-90'}"
												/>
											</button>
											<DropdownMenu.Root>
												<DropdownMenu.Trigger
													class="mr-1 shrink-0 rounded p-0.5 text-[var(--color-text-tertiary)] opacity-0 group-hover/team:opacity-100 data-[state=open]:opacity-100 hover:bg-[var(--color-bg-tertiary)] hover:text-[var(--color-text-secondary)]"
													onclick={(e) => e.stopPropagation()}
												>
													<Ellipsis size={14} />
												</DropdownMenu.Trigger>
												<DropdownMenu.Content side="right" align="start" class="w-44">
													<DropdownMenu.Item onclick={() => goto(`/${slug}/settings/teams/${team.id}`)}>
														<Settings size={14} class="mr-2" />
														Team settings
													</DropdownMenu.Item>
													<DropdownMenu.Separator />
													<DropdownMenu.Item onclick={() => onleaveteam?.(team)} class="text-[var(--color-error)]">
														<LogOut size={14} class="mr-2" />
														Leave team...
													</DropdownMenu.Item>
													<DropdownMenu.Item onclick={() => ondeleteteam?.(team)} class="text-[var(--color-error)]">
														<Trash2 size={14} class="mr-2" />
														Delete team...
													</DropdownMenu.Item>
												</DropdownMenu.Content>
											</DropdownMenu.Root>
										</div>
									</ContextMenu.Trigger>
									<ContextMenu.Content class="w-44">
										<ContextMenu.Item onclick={() => goto(`/${slug}/teams/${team.id}`)}>
											<SquareUser class={menuIconClass} />
											Open team
										</ContextMenu.Item>
										<ContextMenu.Item onclick={() => goto(`/${slug}/settings/teams/${team.id}`)}>
											<Settings class={menuIconClass} />
											Team settings
										</ContextMenu.Item>
										<ContextMenu.Separator />
										<ContextMenu.Item
											onclick={() => onleaveteam?.(team)}
											class="text-[var(--color-error)] focus:text-[var(--color-error)]"
										>
											<LogOut class={menuIconClass} />
											Leave team...
										</ContextMenu.Item>
										<ContextMenu.Item
											onclick={() => ondeleteteam?.(team)}
											class="text-[var(--color-error)] focus:text-[var(--color-error)]"
										>
											<Trash2 class={menuIconClass} />
											Delete team...
										</ContextMenu.Item>
									</ContextMenu.Content>
								</ContextMenu.Root>
								{#if teamExpanded}
									<div transition:slideFade>
										<a
											href="/{slug}/teams/{team.id}"
											class="ml-4 flex items-center gap-2 rounded-md px-2 py-1 text-xs {isActive(
												`/${slug}/teams/${team.id}`
											) &&
											!isActive(`/${slug}/teams/${team.id}/cycles`) &&
											!isActive(`/${slug}/teams/${team.id}/triage`) &&
											!isActive(`/${slug}/teams/${team.id}/projects`) &&
											!isActive(`/${slug}/teams/${team.id}/views`)
												? 'bg-[var(--color-bg-hover)]/50 text-[var(--color-text-primary)]'
												: 'text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]'}"
										>
											<SquaresSubtract size={13} />
											Issues
										</a>
										<a
											href="/{slug}/teams/{team.id}/cycles"
											class="ml-4 flex items-center gap-2 rounded-md px-2 py-1 text-xs {isActive(
												`/${slug}/teams/${team.id}/cycles`
											)
												? 'bg-[var(--color-bg-hover)]/50 text-[var(--color-text-primary)]'
												: 'text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]'}"
										>
											<RefreshCcwDot size={13} />
											Cycles
										</a>
										<a
											href="/{slug}/teams/{team.id}/projects"
											class="ml-4 flex items-center gap-2 rounded-md px-2 py-1 text-xs {isActive(
												`/${slug}/teams/${team.id}/projects`
											)
												? 'bg-[var(--color-bg-hover)]/50 text-[var(--color-text-primary)]'
												: 'text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]'}"
										>
											<Box size={13} />
											Projects
										</a>
										{#if teamProjects.length > 0}
											<div class="relative ml-[31px]">
												<div class="absolute left-0 top-0 bottom-2 w-px bg-[var(--app-border-hover)]"></div>
												<div
													use:dndzone={{ items: teamProjects, type: `sidebar-project-${team.id}`, ...dragOptions }}
													onconsider={handleProjectsConsider}
													onfinalize={handleProjectsFinalize}
												>
													{#each teamProjects as project (project.id)}
														<div
															animate:flip={{ duration: sidebarDndFlipMs }}
															class="transition-[transform,opacity] duration-150 {activeSidebarDragId === project.id
																? 'opacity-60'
																: ''}"
														>
															<ContextMenu.Root>
																<ContextMenu.Trigger class="block">
																	<a
																		data-sidebar-drag-row
																		href="/{slug}/projects/{project.id}"
																		class="relative flex items-center gap-2 rounded-md px-2 py-1 text-xs {isActive(
																			`/${slug}/projects/${project.id}`
																		)
																			? 'bg-[var(--color-bg-hover)]/50 text-[var(--color-text-primary)]'
																			: 'text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]'}"
																	>
																		<span class="ml-2 truncate capitalize">{project.name}</span>
																	</a>
																</ContextMenu.Trigger>
																<ContextMenu.Content class="w-44">
																	<ContextMenu.Item onclick={() => goto(`/${slug}/projects/${project.id}`)}>
																		<Box class={menuIconClass} />
																		Open project
																	</ContextMenu.Item>
																	<ContextMenu.Item
																		onclick={() => window.open(`/${slug}/projects/${project.id}`, '_blank')}
																	>
																		<ArrowUpRight class={menuIconClass} />
																		Open in new tab
																	</ContextMenu.Item>
																	<ContextMenu.Item onclick={() => copyLink(`/${slug}/projects/${project.id}`)}>
																		<Copy class={menuIconClass} />
																		Copy link
																	</ContextMenu.Item>
																	<ContextMenu.Separator />
																	<ContextMenu.Item
																		onclick={() => handleDeleteProject(project)}
																		class="text-[var(--color-error)] focus:text-[var(--color-error)]"
																	>
																		<Trash2 class={menuIconClass} />
																		Delete project
																	</ContextMenu.Item>
																</ContextMenu.Content>
															</ContextMenu.Root>
														</div>
													{/each}
												</div>
											</div>
										{/if}
										<a
											href="/{slug}/teams/{team.id}/views"
											class="ml-4 flex items-center gap-2 rounded-md px-2 py-1 text-xs {isActive(
												`/${slug}/teams/${team.id}/views`
											)
												? 'bg-[var(--color-bg-hover)]/50 text-[var(--color-text-primary)]'
												: 'text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]'}"
										>
											<Layers size={13} />
											Views
										</a>
										{#if team.triage_enabled}
											<a
												href="/{slug}/teams/{team.id}/triage"
												class="ml-4 flex items-center gap-2 rounded-md px-2 py-1 text-xs {isActive(
													`/${slug}/teams/${team.id}/triage`
												)
													? 'bg-[var(--color-bg-hover)]/50 text-[var(--color-text-primary)]'
													: 'text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]'}"
											>
												<ShieldCheck size={13} />
												Triage
											</a>
										{/if}
										{#if teamViews.length > 0}
											<div class="relative ml-[31px]">
												<div class="absolute left-0 top-0 bottom-2 w-px bg-[var(--app-border-hover)]"></div>
												<div
													use:dndzone={{ items: teamViews, type: `sidebar-view-${team.id}`, ...dragOptions }}
													onconsider={handleTeamViewsConsider}
													onfinalize={handleTeamViewsFinalize}
												>
													{#each teamViews as view (view.id)}
														<div
															animate:flip={{ duration: sidebarDndFlipMs }}
															class="transition-[transform,opacity] duration-150 {activeSidebarDragId === view.id
																? 'opacity-60'
																: ''}"
														>
															<ContextMenu.Root>
																<ContextMenu.Trigger class="block">
																	<a
																		data-sidebar-drag-row
																		href="/{slug}/views/{view.id}"
																		class="relative flex items-center gap-2 rounded-md px-2 py-1 text-xs {isActive(
																			`/${slug}/views/${view.id}`
																		)
																			? 'bg-[var(--color-bg-hover)]/50 text-[var(--color-text-primary)]'
																			: 'text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]'}"
																	>
																		<Layers size={13} />
																		{view.name}
																	</a>
																</ContextMenu.Trigger>
																<ContextMenu.Content class="w-44">
																	<ContextMenu.Item onclick={() => goto(`/${slug}/views/${view.id}`)}>
																		<Layers class={menuIconClass} />
																		Open view
																	</ContextMenu.Item>
																	<ContextMenu.Item onclick={() => window.open(`/${slug}/views/${view.id}`, '_blank')}>
																		<ArrowUpRight class={menuIconClass} />
																		Open in new tab
																	</ContextMenu.Item>
																	<ContextMenu.Item onclick={() => copyLink(`/${slug}/views/${view.id}`)}>
																		<Copy class={menuIconClass} />
																		Copy link
																	</ContextMenu.Item>
																	<ContextMenu.Separator />
																	<ContextMenu.Item
																		onclick={() => requestDeleteView(view)}
																		class="text-[var(--color-error)] focus:text-[var(--color-error)]"
																	>
																		<Trash2 class={menuIconClass} />
																		Delete view
																	</ContextMenu.Item>
																</ContextMenu.Content>
															</ContextMenu.Root>
														</div>
													{/each}
												</div>
											</div>
										{/if}
									</div>
								{/if}
							</div>
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
					</div>
				{/if}
			</div>

			<!-- Views -->
			{#if personalViews.length > 0 || workspaceViews.length > 0}
				<div class="mt-4">
					<button
						onclick={() => (viewsCollapsed = toggleSection('views', viewsCollapsed))}
						class="flex w-full items-center px-2 py-1"
					>
						<span class="flex items-center gap-1">
							<span class="text-[11px] font-semibold text-[var(--color-text-secondary)]">Views</span>
							<ChevronDown
								size={12}
								class="text-[var(--color-text-tertiary)] transition-transform {viewsCollapsed ? '-rotate-90' : ''}"
							/>
						</span>
					</button>
					{#if !viewsCollapsed}
						<div transition:slideFade>
							<a
								href="/{slug}/my-views"
								class="flex items-center gap-2 rounded-md px-2 py-1 text-sm {isActive(`/${slug}/my-views`)
									? 'bg-[var(--color-bg-hover)]/50 text-[var(--color-text-primary)]'
									: 'text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]'}"
							>
								<Bookmark size={16} class="shrink-0" />
								<span class="truncate">My Views</span>
								{#if personalViews.length > 0}
									<span class="ml-auto text-[10px] text-[var(--color-text-tertiary)]">{personalViews.length}</span>
								{/if}
							</a>
						</div>

						{#if workspaceViews.length > 0}
							<div
								transition:slideFade
								use:dndzone={{ items: workspaceViews, type: 'sidebar-view', ...dragOptions }}
								onconsider={handleViewsConsider}
								onfinalize={handleViewsFinalize}
							>
								{#each workspaceViews as view (view.id)}
									<div
										animate:flip={{ duration: sidebarDndFlipMs }}
										class="transition-[transform,opacity] duration-150 {activeSidebarDragId === view.id
											? 'opacity-60'
											: ''}"
									>
										<ContextMenu.Root>
											<ContextMenu.Trigger class="block">
												<a
													data-sidebar-drag-row
													href="/{slug}/views/{view.id}"
													class="flex items-center gap-2 rounded-md px-2 py-1 text-sm {isActive(
														`/${slug}/views/${view.id}`
													)
														? 'bg-[var(--color-bg-hover)]/50 text-[var(--color-text-primary)]'
														: 'text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]'}"
												>
													<Layers size={16} />
													{view.name}
												</a>
											</ContextMenu.Trigger>
											<ContextMenu.Content class="w-44">
												<ContextMenu.Item onclick={() => goto(`/${slug}/views/${view.id}`)}>
													<Layers class={menuIconClass} />
													Open view
												</ContextMenu.Item>
												<ContextMenu.Item onclick={() => window.open(`/${slug}/views/${view.id}`, '_blank')}>
													<ArrowUpRight class={menuIconClass} />
													Open in new tab
												</ContextMenu.Item>
												<ContextMenu.Item onclick={() => copyLink(`/${slug}/views/${view.id}`)}>
													<Copy class={menuIconClass} />
													Copy link
												</ContextMenu.Item>
												<ContextMenu.Separator />
												<ContextMenu.Item
													onclick={() => requestDeleteView(view)}
													class="text-[var(--color-error)] focus:text-[var(--color-error)]"
												>
													<Trash2 class={menuIconClass} />
													Delete view
												</ContextMenu.Item>
											</ContextMenu.Content>
										</ContextMenu.Root>
									</div>
								{/each}
							</div>
						{/if}
					{/if}
				</div>
			{/if}

			<!-- Projects -->
			<div class="mt-4">
				<button
					type="button"
					class="flex w-full cursor-pointer items-center justify-between px-2 py-1"
					onclick={() => (projectsCollapsed = toggleSection('projects', projectsCollapsed))}
				>
					<span class="flex items-center gap-1">
						<span class="text-[11px] font-semibold text-[var(--color-text-secondary)]">Projects</span>
						<ChevronDown
							size={12}
							class="text-[var(--color-text-tertiary)] transition-transform {projectsCollapsed ? '-rotate-90' : ''}"
						/>
					</span>
				</button>
				{#if !projectsCollapsed}
					<div transition:slideFade>
						<a
							href="/{slug}/projects"
							class="flex items-center gap-2 rounded-md px-2 py-1 text-sm {isActive(`/${slug}/projects`)
								? 'bg-[var(--color-bg-hover)]/50 text-[var(--color-text-primary)]'
								: 'text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]'}"
						>
							<Box size={16} />
							All Projects
						</a>
					</div>
				{/if}
			</div>
		</div>
	</nav>

	<!-- Profile -->
	<div class="flex items-center gap-2 px-2 py-2">
		<Popover.Root>
			<Popover.Trigger>
				{#if authState.user?.avatar_url}
					<img
						src={authState.user.avatar_url}
						alt=""
						class="h-7 w-7 shrink-0 cursor-pointer rounded-full transition-[filter] hover:brightness-125"
					/>
				{:else}
					<div
						class="flex h-7 w-7 shrink-0 cursor-pointer items-center justify-center rounded-full bg-[var(--app-accent)] text-xs font-bold text-[var(--app-accent-foreground)] transition-[filter] hover:brightness-125"
					>
						{(authState.user?.name ?? 'U').charAt(0).toUpperCase()}
					</div>
				{/if}
			</Popover.Trigger>
			<Popover.Content side="top" align="start" class="w-52 p-1">
				<a
					href="/{slug}/settings"
					class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)]"
				>
					<Settings size={14} />
					Settings
				</a>
				<div class="mt-1 border-t border-[var(--app-border)] pt-1">
					<a
						href={currentReleaseUrl}
						target="_blank"
						rel="noopener"
						class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)]"
					>
						<Info size={14} />
						{currentVersionLabel}
					</a>
					<button
						onclick={handleLogout}
						class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)]"
					>
						<LogOut size={14} />
						Log out
					</button>
				</div>
			</Popover.Content>
		</Popover.Root>
		<button
			onclick={() => onshortcutshelp?.()}
			class="flex h-7 w-7 shrink-0 items-center justify-center rounded-full border border-[var(--app-border)] text-[var(--color-text-secondary)] transition-colors hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]"
			aria-label="Keyboard shortcuts"
			title="Keyboard shortcuts (?)"
		>
			<CircleQuestionMark size={16} />
		</button>
	</div>
{/snippet}
