<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { authState } from '$lib/features/auth/auth.state.svelte';
	import { getWorkspace } from '$lib/api/workspaces';
	import { listTeams, createTeam, deleteTeam, leaveTeam } from '$lib/api/teams';
	import { listProjects } from '$lib/api/projects';
	import { listLabels } from '$lib/api/labels';
	import { listMembers } from '$lib/api/members';
	import { listViews } from '$lib/api/views';
	import { listNotifications } from '$lib/api/notifications';
	import type { Workspace } from '$lib/types/workspace';
	import type { Team } from '$lib/types/team';
	import type { Project } from '$lib/types/project';
	import type { Label } from '$lib/types/label';
	import type { WorkspaceMember } from '$lib/types/workspace';
	import type { View } from '$lib/types/view';
	import type { IssuePriority } from '$lib/types/issue';
	import Sidebar from '$lib/components/layout/Sidebar.svelte';
	import CommandPalette from '$lib/components/layout/CommandPalette.svelte';
	import CreateIssueDialog from '$lib/features/issues/CreateIssueDialog.svelte';
	import CreateTeamDialog from '$lib/features/teams/CreateTeamDialog.svelte';
	import * as Dialog from '$lib/components/ui/dialog';
	import ShortcutHelp from '$lib/components/shared/ShortcutHelp.svelte';
	import * as Sheet from '$lib/components/ui/sheet';
	import { Button } from '$lib/components/ui/button';
	import { IsMobile } from '$lib/hooks/is-mobile.svelte';
	import { issuesState } from '$lib/features/issues/issues.state.svelte';
	import { showIssueCreatedToast } from '$lib/features/issues/issue-created-toast';
	import { preferencesState } from '$lib/features/preferences/preferences.state.svelte';
	import { teamStatusesState } from '$lib/features/issues/team-statuses.state.svelte';
	import { sidebarState } from '$lib/features/layout/sidebar.state.svelte';
	import { createShortcutEngine, type ShortcutDef } from '$lib/utils/keyboard';
	import { Menu, Search, SquarePen } from 'lucide-svelte';
	import { appToast } from '$lib/features/toast/toast';

	let { children } = $props();
	let workspace = $state<Workspace | null>(null);
	let teams = $state<Team[]>([]);
	let projects = $state<Project[]>([]);
	let labels = $state<Label[]>([]);
	let members = $state<WorkspaceMember[]>([]);
	let views = $state<View[]>([]);
	let unreadCount = $state(0);
	let showCommandPalette = $state(false);
	let showCreateIssue = $state(false);
	let showCreateTeam = $state(false);
	let showShortcutHelp = $state(false);
	let showMobileSidebar = $state(false);
	let confirmTeam = $state<Team | null>(null);
	let confirmAction = $state<'leave' | 'delete' | null>(null);
	let confirmOpen = $state(false);
	let confirmSubmitting = $state(false);
	const isMobile = new IsMobile();

	const slug = $derived(page.params.workspaceSlug ?? '');
	const isSettings = $derived(page.url.pathname.includes('/settings'));

	async function loadWorkspaceData(workspaceSlug: string) {
		try {
			const [ws, t, p, l, m, v, notifRes] = await Promise.all([
				getWorkspace(workspaceSlug),
				listTeams(workspaceSlug),
				listProjects(workspaceSlug),
				listLabels(workspaceSlug),
				listMembers(workspaceSlug),
				listViews(workspaceSlug),
				listNotifications()
			]);
			workspace = ws;
			teams = t;
			projects = p;
			sidebarState.teams = t;
			sidebarState.projects = p;
			labels = l;
			members = m;
			views = v;
			unreadCount = notifRes.unread_count;
		} catch {
			goto('/login');
		}
	}

	async function reloadViews(workspaceSlug: string) {
		try {
			views = await listViews(workspaceSlug);
		} catch {
			// Keep the current navigation list if a background refresh fails.
		}
	}

	function handleAppRefresh(e: Event) {
		const detail = (e as CustomEvent<{ slug?: string; resources?: string[] }>).detail;
		if (detail?.slug && detail.slug !== slug) return;
		const resources = detail?.resources;
		if (!slug) return;
		if (!resources || resources.length === 0) {
			loadWorkspaceData(slug);
			if (issuesState.issues.length > 0) {
				issuesState.load(slug, issuesState.filters);
			}
			return;
		}
		if (resources.includes('issues') && issuesState.issues.length > 0) {
			issuesState.load(slug, issuesState.filters);
		}
		if (resources.includes('workspace')) {
			getWorkspace(slug).then((ws) => { workspace = ws; }).catch(() => {});
		}
		if (resources.includes('teams')) {
			listTeams(slug).then((t) => {
				teams = t;
				sidebarState.teams = t;
			}).catch(() => {});
		}
		if (resources.includes('projects')) {
			listProjects(slug).then((p) => {
				projects = p;
				sidebarState.projects = p;
			}).catch(() => {});
		}
		if (resources.includes('labels')) {
			listLabels(slug).then((l) => { labels = l; }).catch(() => {});
		}
		if (resources.includes('members')) {
			listMembers(slug).then((m) => { members = m; }).catch(() => {});
		}
		if (resources.includes('views')) {
			reloadViews(slug);
		}
		if (resources.includes('notifications')) {
			listNotifications().then((r) => { unreadCount = r.unread_count; }).catch(() => {});
		}
	}

	onMount(async () => {
		await authState.init();
		if (!authState.authenticated) {
			goto('/login');
			return;
		}
		preferencesState.syncRemote();
		await loadWorkspaceData(slug);
	});

	// Re-fetch all data when workspace slug changes (e.g. workspace switch)
	let loadedSlug = '';
	$effect(() => {
		if (slug && slug !== loadedSlug) {
			loadedSlug = slug;
			loadWorkspaceData(slug);
		}
	});

	// Full shortcut definitions
	const shortcutDefs: ShortcutDef[] = [
		// Navigation sequences (G + key)
		{ keys: ['g', 'i'], handler: () => goto(`/${slug}/inbox`), label: 'Go to Inbox', category: 'Navigation' },
		{ keys: ['g', 'm'], handler: () => goto(`/${slug}/my-issues`), label: 'Go to My Issues', category: 'Navigation' },
		{ keys: ['g', 'p'], handler: () => goto(`/${slug}/projects`), label: 'Go to Projects', category: 'Navigation' },
		{ keys: ['g', 's'], handler: () => goto(`/${slug}/settings`), label: 'Go to Settings', category: 'Navigation' },
		// Actions
		{
			key: 'c',
			handler: () => {
				if (teams.length === 0) {
					showCreateTeam = true;
				} else {
					// Ensure statuses are loaded for the target team
					const targetTeam = getCreateTeamId();
					if (targetTeam) {
						teamStatusesState.load(slug, targetTeam);
					}
					showCreateIssue = true;
				}
			},
			label: 'Create issue',
			category: 'Actions'
		},
		{ key: 'k', meta: true, handler: () => (showCommandPalette = !showCommandPalette), label: 'Command palette', category: 'Actions' },
		{ key: '/', handler: () => (showCommandPalette = true), label: 'Search', category: 'Actions' },
		{ key: '?', shift: true, handler: () => (showShortcutHelp = !showShortcutHelp), label: 'Keyboard shortcuts', category: 'Help' },
	];

	const shortcutEngine = createShortcutEngine(shortcutDefs);

	onMount(() => {
		document.addEventListener('keydown', shortcutEngine.handler);
		window.addEventListener('app:refresh', handleAppRefresh);
		return () => {
			document.removeEventListener('keydown', shortcutEngine.handler);
			window.removeEventListener('app:refresh', handleAppRefresh);
		};
	});

	async function handleCreateTeam(data: { name: string; key: string; description?: string }) {
		try {
			const team = await createTeam(slug, data);
			teams = [...teams, team];
			sidebarState.teams = teams;
			appToast.success('Team created');
		} catch (err: any) {
			appToast.apiError(err, 'Failed to create team');
		}
	}

	function removeTeamFromState(teamId: string) {
		teams = teams.filter((team) => team.id !== teamId);
		sidebarState.teams = teams;

		const teamPath = `/${slug}/teams/${teamId}`;
		const settingsPath = `/${slug}/settings/teams/${teamId}`;
		if (page.url.pathname.startsWith(teamPath) || page.url.pathname.startsWith(settingsPath)) {
			goto(`/${slug}/my-issues`);
		}
	}

	function openTeamConfirm(team: Team, action: 'leave' | 'delete') {
		confirmTeam = team;
		confirmAction = action;
		confirmOpen = true;
	}

	function handleLeaveTeam(team: Team) {
		openTeamConfirm(team, 'leave');
	}

	function handleDeleteTeam(team: Team) {
		openTeamConfirm(team, 'delete');
	}

	async function confirmTeamAction() {
		if (!confirmTeam || !confirmAction) return;
		confirmSubmitting = true;
		try {
			if (confirmAction === 'leave') {
				const result = await leaveTeam(slug, confirmTeam.id);
				removeTeamFromState(confirmTeam.id);
				appToast.success(result.status === 'deleted' ? 'Team deleted' : 'Left team');
			} else {
				await deleteTeam(slug, confirmTeam.id);
				removeTeamFromState(confirmTeam.id);
				appToast.success('Team deleted');
			}
			confirmOpen = false;
			confirmTeam = null;
			confirmAction = null;
		} catch (err: any) {
			appToast.apiError(err, `Failed to ${confirmAction} team`);
		} finally {
			confirmSubmitting = false;
		}
	}

	function openCreateIssue() {
		if (teams.length === 0) {
			showCreateTeam = true;
		} else {
			const targetTeam = getCreateTeamId();
			if (targetTeam) {
				teamStatusesState.load(slug, targetTeam);
			}
			showCreateIssue = true;
		}
		showMobileSidebar = false;
	}

	function singleFilterValue(value?: string): string | undefined {
		if (!value) return undefined;
		const values = value.split(',').filter(Boolean);
		return values.length === 1 ? values[0] : undefined;
	}

	function getActiveIssueFilters(): Record<string, string> {
		const pathname = page.url.pathname;
		const teamPath = page.params.teamId ? `/${slug}/teams/${page.params.teamId}` : '';
		const projectPath = page.params.projectId ? `/${slug}/projects/${page.params.projectId}` : '';

		if (pathname === `/${slug}/my-issues`) return issuesState.filters;
		if (teamPath && (pathname === teamPath || pathname === `${teamPath}/board`)) return issuesState.filters;
		if (projectPath && pathname === projectPath) return issuesState.filters;
		return {};
	}

	function getCreateTeamId(): string | undefined {
		const activeFilters = getActiveIssueFilters();
		const routeProject = projects.find((project) => project.id === page.params.projectId);
		return page.params.teamId ?? routeProject?.team_id ?? singleFilterValue(activeFilters.team) ?? teams[0]?.id;
	}

	function getCreateStatusId(): string | undefined {
		const value = singleFilterValue(getActiveIssueFilters().status);
		if (!value) return undefined;
		return teamStatusesState.statusById.get(value)?.id ?? teamStatusesState.statusOrder.find((status) => status.slug === value)?.id;
	}

	function getCreatePriority(): IssuePriority | undefined {
		const value = singleFilterValue(getActiveIssueFilters().priority);
		const priority = value === undefined ? NaN : Number(value);
		return [0, 1, 2, 3, 4].includes(priority) ? priority as IssuePriority : undefined;
	}

	function getCreateProjectId(): string | null | undefined {
		const routeProjectId = page.params.projectId;
		if (routeProjectId) return routeProjectId;
		const value = singleFilterValue(getActiveIssueFilters().project);
		if (value === 'none') return null;
		return value;
	}

	function getCreateAssigneeIds(): string[] | undefined {
		const value = singleFilterValue(getActiveIssueFilters().assignee);
		if (value === 'none') return [];
		return value ? [value] : undefined;
	}

	function getCreateLabelIds(): string[] | undefined {
		const value = singleFilterValue(getActiveIssueFilters().label);
		return value ? [value] : undefined;
	}

	// WebSocket connection — reconnects when slug changes
	let ws_conn: WebSocket | null = null;
	let wsSlug = '';
	let wsReconnectTimer: ReturnType<typeof setTimeout> | null = null;
	let wsDestroyed = false;

	$effect(() => {
		if (slug && slug !== wsSlug) {
			wsSlug = slug;
			clearWebSocketReconnect();
			ws_conn?.close();
			connectWebSocket(slug);
		}
	});

	onDestroy(() => {
		wsDestroyed = true;
		clearWebSocketReconnect();
		ws_conn?.close();
		window.removeEventListener('ws:send', handleWSSend as EventListener);
	});

	// Allow child components to send WebSocket messages
	function handleWSSend(e: CustomEvent<any>) {
		if (ws_conn?.readyState === WebSocket.OPEN) {
			ws_conn.send(JSON.stringify(e.detail));
		}
	}
	window.addEventListener('ws:send', handleWSSend as EventListener);

	function connectWebSocket(workspaceSlug: string) {
		if (wsDestroyed || wsSlug !== workspaceSlug) return;
		clearWebSocketReconnect();
		const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
		const wsUrl = `${protocol}//${window.location.host}/api/workspaces/${workspaceSlug}/ws`;
		const socket = new WebSocket(wsUrl);
		ws_conn = socket;

		socket.onmessage = (event) => {
			if (socket !== ws_conn) return;
			try {
				const data = JSON.parse(event.data);
				handleWSMessage(data);
			} catch {
				// Ignore malformed messages
			}
		};

		socket.onopen = () => {
			if (socket !== ws_conn) return;
			window.dispatchEvent(new CustomEvent('ws:reconnected'));
		};

		socket.onerror = () => {
			socket.close();
		};

		socket.onclose = () => {
			if (socket !== ws_conn) return;
			// Only reconnect if still on the same workspace
			if (!wsDestroyed && wsSlug === workspaceSlug) {
				wsReconnectTimer = setTimeout(() => connectWebSocket(workspaceSlug), 3000);
			}
		};
	}

	function clearWebSocketReconnect() {
		if (wsReconnectTimer) {
			clearTimeout(wsReconnectTimer);
			wsReconnectTimer = null;
		}
	}

	function handleWSMessage(msg: { type: string; payload: any }) {
		switch (msg.type) {
			case 'issue.created':
			case 'issue.updated':
			case 'issue.triaged':
			case 'issues.bulk_updated':
			case 'issues.bulk_deleted': {
				if (slug && issuesState.issues.length > 0) {
					issuesState.load(slug, issuesState.filters, false);
				}
				window.dispatchEvent(new CustomEvent('ws:issue-updated', { detail: msg.payload }));
				break;
			}
			case 'issue.deleted': {
				if (slug && issuesState.issues.length > 0) {
					issuesState.load(slug, issuesState.filters, false);
				}
				window.dispatchEvent(new CustomEvent('ws:issue-deleted', { detail: msg.payload }));
				break;
			}
			case 'comment.created': {
				window.dispatchEvent(new CustomEvent('ws:comment-created', { detail: msg.payload }));
				break;
			}
			case 'view.created':
			case 'view.updated':
			case 'view.deleted': {
				window.dispatchEvent(new CustomEvent('app:refresh', { detail: { ...msg.payload, resources: ['views'] } }));
				break;
			}
			case 'app.refresh': {
				window.dispatchEvent(new CustomEvent('app:refresh', { detail: msg.payload ?? {} }));
				break;
			}
			case 'github:pr_updated':
			case 'github:branch_created':
			case 'github:commit_pushed': {
				window.dispatchEvent(new CustomEvent(`ws:${msg.type}`, { detail: msg.payload }));
				break;
			}
			case 'notification.created': {
				unreadCount++;
				window.dispatchEvent(new CustomEvent('ws:notification', { detail: msg.payload }));
				break;
			}
			case 'presence.join':
			case 'presence.leave':
			case 'presence.sync':
			case 'cursor.move':
			case 'focus.update':
			case 'focus.leave': {
				window.dispatchEvent(new CustomEvent(`ws:${msg.type}`, { detail: msg.payload }));
				break;
			}
		}
	}
</script>

{#if workspace}
	<div class="flex h-dvh bg-[var(--color-bg)]">
		{#if !isSettings}
			<div class="hidden md:contents">
				<Sidebar
					{workspace}
					{teams}
					{views}
					{projects}
					{unreadCount}
					{slug}
					oncreateissue={openCreateIssue}
					oncreateteam={() => (showCreateTeam = true)}
					onleaveteam={handleLeaveTeam}
					ondeleteteam={handleDeleteTeam}
					onsearch={() => (showCommandPalette = true)}
					onshortcutshelp={() => (showShortcutHelp = true)}
				/>
			</div>

			<Sheet.Root bind:open={showMobileSidebar}>
				<Sheet.Content side="left" class="w-[min(88vw,320px)] p-0 [&>button]:hidden" showCloseButton={false}>
					<Sheet.Header class="sr-only">
						<Sheet.Title>Workspace navigation</Sheet.Title>
						<Sheet.Description>Navigate workspace sections, teams, views, and projects.</Sheet.Description>
					</Sheet.Header>
					<Sidebar
						{workspace}
						{teams}
						{views}
						{projects}
						{unreadCount}
						{slug}
						mobile
						oncreateissue={openCreateIssue}
						oncreateteam={() => { showCreateTeam = true; showMobileSidebar = false; }}
						onleaveteam={(team) => { showMobileSidebar = false; handleLeaveTeam(team); }}
						ondeleteteam={(team) => { showMobileSidebar = false; handleDeleteTeam(team); }}
					onsearch={() => { showCommandPalette = true; showMobileSidebar = false; }}
					onnavigate={() => (showMobileSidebar = false)}
					onshortcutshelp={() => { showMobileSidebar = false; showShortcutHelp = true; }}
				/>
				</Sheet.Content>
			</Sheet.Root>
		{/if}
		<main class="flex min-w-0 flex-1 flex-col overflow-hidden">
			{#if !isSettings}
				<div class="flex h-12 shrink-0 items-center justify-between border-b border-[var(--app-border)] bg-[var(--color-bg)] px-3 md:hidden">
					<div class="flex min-w-0 items-center gap-2">
						<Button variant="ghost" size="icon-lg" onclick={() => (showMobileSidebar = true)} aria-label="Open navigation">
							<Menu size={18} />
						</Button>
						<span class="truncate text-sm font-medium text-[var(--color-text-primary)]">{workspace.name}</span>
					</div>
					<div class="flex shrink-0 items-center gap-1">
						<Button variant="ghost" size="icon-lg" onclick={() => (showCommandPalette = true)} aria-label="Search">
							<Search size={18} />
						</Button>
						<Button variant="ghost" size="icon-lg" onclick={openCreateIssue} aria-label="Create issue">
							<SquarePen size={18} />
						</Button>
					</div>
				</div>
			{/if}
			<div class="min-h-0 flex-1 overflow-auto">
				{@render children()}
			</div>
		</main>
	</div>

	{#if showCommandPalette}
		<CommandPalette {slug} {teams} onclose={() => (showCommandPalette = false)} oncreateissue={openCreateIssue} />
	{/if}

	<CreateIssueDialog
		bind:open={showCreateIssue}
		{slug}
		{teams}
		{projects}
		{labels}
		{members}
		defaultTeamId={getCreateTeamId()}
		defaultStatusId={getCreateStatusId()}
		defaultPriority={getCreatePriority()}
		defaultProjectId={getCreateProjectId()}
		defaultAssigneeIds={getCreateAssigneeIds()}
		defaultLabelIds={getCreateLabelIds()}
		onlabelcreated={(label) => (labels = [label, ...labels.filter((existing) => existing.id !== label.id)])}
		onsubmit={async (req) => {
			try {
				const created = await issuesState.create(slug, req);
				showIssueCreatedToast(slug, created);
			} catch (err: any) {
				appToast.apiError(err, 'Failed to create issue');
			}
		}}
	/>

	<CreateTeamDialog
		bind:open={showCreateTeam}
		onsubmit={handleCreateTeam}
	/>

	<Dialog.Root bind:open={confirmOpen}>
		<Dialog.Content class="sm:max-w-[420px] border-[var(--app-border)] bg-[var(--color-bg-secondary)]">
			<Dialog.Header>
				<Dialog.Title>
					{confirmAction === 'delete' ? 'Delete team' : 'Leave team'}
				</Dialog.Title>
				<Dialog.Description>
					{#if confirmAction === 'delete'}
						This will permanently delete {confirmTeam?.name ?? 'this team'} and its issues, cycles, and statuses.
					{:else}
						You will leave {confirmTeam?.name ?? 'this team'}. If you are the last member or workspace owner, the team will be deleted.
					{/if}
				</Dialog.Description>
			</Dialog.Header>
			<Dialog.Footer>
				<Button variant="outline" onclick={() => (confirmOpen = false)} disabled={confirmSubmitting}>Cancel</Button>
				<Button variant="destructive" onclick={confirmTeamAction} disabled={confirmSubmitting}>
					{confirmSubmitting ? 'Working...' : confirmAction === 'delete' ? 'Delete team' : 'Leave team'}
				</Button>
			</Dialog.Footer>
		</Dialog.Content>
	</Dialog.Root>

	<ShortcutHelp
		bind:open={showShortcutHelp}
		shortcuts={shortcutDefs}
	/>
{:else}
	<div class="flex h-screen items-center justify-center">
	</div>
{/if}
