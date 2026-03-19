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
	import { createShortcutEngine, type ShortcutDef } from '$lib/utils/keyboard';
	import { toast } from 'svelte-sonner';

	let { children } = $props();
	let workspace = $state<Workspace | null>(null);
	let teams = $state<Team[]>([]);
	let projects = $state<Project[]>([]);
	let labels = $state<Label[]>([]);
	let members = $state<WorkspaceMember[]>([]);
	let views = $state<View[]>([]);
	let showCommandPalette = $state(false);
	let showCreateIssue = $state(false);
	let showCreateTeam = $state(false);
	let showShortcutHelp = $state(false);

	const slug = $derived(page.params.workspaceSlug ?? '');

	onMount(async () => {
		await authState.init();
		if (!authState.authenticated) {
			goto('/login');
			return;
		}
		try {
			const [ws, t, p, l, m, v] = await Promise.all([
				getWorkspace(slug),
				listTeams(slug),
				listProjects(slug),
				listLabels(slug),
				listMembers(slug),
				listViews(slug)
			]);
			workspace = ws;
			teams = t;
			projects = p;
			labels = l;
			members = m;
			views = v;
		} catch {
			goto('/login');
		}
	});

	// Full shortcut definitions
	const shortcutDefs: ShortcutDef[] = [
		// Navigation sequences (G + key)
		{ keys: ['g', 'i'], handler: () => goto(`/${slug}/inbox`), label: 'Go to Inbox', category: 'Navigation' },
		{ keys: ['g', 'm'], handler: () => goto(`/${slug}/my-issues`), label: 'Go to My Issues', category: 'Navigation' },
		{ keys: ['g', 'd'], handler: () => goto(`/${slug}/dashboard`), label: 'Go to Dashboard', category: 'Navigation' },
		{ keys: ['g', 'p'], handler: () => goto(`/${slug}/projects`), label: 'Go to Projects', category: 'Navigation' },
		{ keys: ['g', 's'], handler: () => goto(`/${slug}/settings`), label: 'Go to Settings', category: 'Navigation' },
		// Actions
		{
			key: 'c',
			handler: () => {
				if (teams.length === 0) {
					showCreateTeam = true;
				} else {
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

	// WebSocket connection
	let ws_conn: WebSocket | null = null;

	onMount(() => {
		if (slug) {
			connectWebSocket();
		}
	});

	onDestroy(() => {
		ws_conn?.close();
	});

	function connectWebSocket() {
		const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
		const wsUrl = `${protocol}//${window.location.host}/api/workspaces/${slug}/ws`;
		ws_conn = new WebSocket(wsUrl);

		ws_conn.onclose = () => {
			setTimeout(connectWebSocket, 3000);
		};
	}
</script>

{#if workspace}
	<div class="flex h-screen bg-[var(--color-bg)]">
		<Sidebar
			{workspace}
			{teams}
			{views}
			{slug}
			oncreateissue={() => {
				if (teams.length === 0) {
					showCreateTeam = true;
				} else {
					showCreateIssue = true;
				}
			}}
			oncreateteam={() => (showCreateTeam = true)}
		/>
		<main class="flex-1 overflow-auto">
			{@render children()}
		</main>
	</div>

	{#if showCommandPalette}
		<CommandPalette {slug} {teams} onclose={() => (showCommandPalette = false)} />
	{/if}

	<CreateIssueDialog
		bind:open={showCreateIssue}
		{teams}
		{projects}
		{labels}
		{members}
		onsubmit={async (req) => {
			try {
				await issuesState.create(slug, req);
				toast.success('Issue created');
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
		<div class="text-[var(--color-text-secondary)]">Loading...</div>
	</div>
{/if}
