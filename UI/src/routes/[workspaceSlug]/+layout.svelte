<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { authState } from '$lib/features/auth/auth.state.svelte';
	import { getWorkspace } from '$lib/api/workspaces';
	import { listTeams, createTeam } from '$lib/api/teams';
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
	import Sidebar from '$lib/components/layout/Sidebar.svelte';
	import CommandPalette from '$lib/components/layout/CommandPalette.svelte';
	import CreateIssueDialog from '$lib/features/issues/CreateIssueDialog.svelte';
	import CreateTeamDialog from '$lib/features/teams/CreateTeamDialog.svelte';
	import ShortcutHelp from '$lib/components/shared/ShortcutHelp.svelte';
	import { issuesState } from '$lib/features/issues/issues.state.svelte';
	import { teamStatusesState } from '$lib/features/issues/team-statuses.state.svelte';
	import { sidebarState } from '$lib/features/layout/sidebar.state.svelte';
	import { createShortcutEngine, type ShortcutDef } from '$lib/utils/keyboard';
	import { toast } from 'svelte-sonner';

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

	onMount(async () => {
		await authState.init();
		if (!authState.authenticated) {
			goto('/login');
			return;
		}
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
					const targetTeam = page.params.teamId ?? teams[0]?.id;
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
		return () => document.removeEventListener('keydown', shortcutEngine.handler);
	});

	async function handleCreateTeam(data: { name: string; key: string; description?: string }) {
		try {
			const team = await createTeam(slug, data);
			teams = [...teams, team];
			toast.success('Team created');
		} catch (err: any) {
			toast.error(err?.error?.message || 'Failed to create team');
		}
	}

	// WebSocket connection — reconnects when slug changes
	let ws_conn: WebSocket | null = null;
	let wsSlug = '';

	$effect(() => {
		if (slug && slug !== wsSlug) {
			wsSlug = slug;
			ws_conn?.close();
			connectWebSocket(slug);
		}
	});

	onDestroy(() => {
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
		const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
		const wsUrl = `${protocol}//${window.location.host}/api/workspaces/${workspaceSlug}/ws`;
		ws_conn = new WebSocket(wsUrl);

		ws_conn.onmessage = (event) => {
			try {
				const data = JSON.parse(event.data);
				handleWSMessage(data);
			} catch {
				// Ignore malformed messages
			}
		};

		ws_conn.onopen = () => {
			window.dispatchEvent(new CustomEvent('ws:reconnected'));
		};

		ws_conn.onclose = () => {
			// Only reconnect if still on the same workspace
			if (wsSlug === workspaceSlug) {
				setTimeout(() => connectWebSocket(workspaceSlug), 3000);
			}
		};
	}

	function handleWSMessage(msg: { type: string; payload: any }) {
		switch (msg.type) {
			case 'issue.created':
			case 'issue.updated':
			case 'issue.triaged':
			case 'issues.bulk_updated':
			case 'issues.bulk_deleted': {
				if (slug && issuesState.issues.length > 0) {
					issuesState.load(slug, issuesState.filters);
				}
				window.dispatchEvent(new CustomEvent('ws:issue-updated', { detail: msg.payload }));
				break;
			}
			case 'issue.deleted': {
				if (slug && issuesState.issues.length > 0) {
					issuesState.load(slug, issuesState.filters);
				}
				window.dispatchEvent(new CustomEvent('ws:issue-deleted', { detail: msg.payload }));
				break;
			}
			case 'comment.created': {
				window.dispatchEvent(new CustomEvent('ws:comment-created', { detail: msg.payload }));
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
	<div class="flex h-screen bg-[var(--color-bg)]">
		{#if !isSettings}
			<Sidebar
				{workspace}
				{teams}
				{views}
				{projects}
				{unreadCount}
				{slug}
				oncreateissue={() => {
					if (teams.length === 0) {
						showCreateTeam = true;
					} else {
						const targetTeam = page.params.teamId ?? teams[0]?.id;
						if (targetTeam) {
							teamStatusesState.load(slug, targetTeam);
						}
						showCreateIssue = true;
					}
				}}
				oncreateteam={() => (showCreateTeam = true)}
				onsearch={() => (showCommandPalette = true)}
			/>
		{/if}
		<main class="flex-1 overflow-auto">
			{@render children()}
		</main>
	</div>

	{#if showCommandPalette}
		<CommandPalette {slug} {teams} onclose={() => (showCommandPalette = false)} />
	{/if}

	<CreateIssueDialog
		bind:open={showCreateIssue}
		{slug}
		{teams}
		{projects}
		{labels}
		{members}
		defaultTeamId={page.params.teamId ?? teams[0]?.id}
		onsubmit={async (req) => {
			try {
				const created = await issuesState.create(slug, req);
				toast.success('Issue created');
				// If the created issue's team doesn't match the current page's team filter,
				// remove it from the optimistic list to avoid confusion
				const currentTeam = page.params.teamId;
				if (currentTeam && created.team_id !== currentTeam) {
					issuesState.issues = issuesState.issues.filter(i => i.id !== created.id);
					issuesState.totalCount--;
				}
			} catch (err: any) {
				toast.error(err?.error?.message || 'Failed to create issue');
			}
		}}
	/>

	<CreateTeamDialog
		bind:open={showCreateTeam}
		onsubmit={handleCreateTeam}
	/>

	<ShortcutHelp
		bind:open={showShortcutHelp}
		shortcuts={shortcutDefs}
	/>
{:else}
	<div class="flex h-screen items-center justify-center">
	</div>
{/if}
