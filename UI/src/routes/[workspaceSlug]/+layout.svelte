<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { authState } from '$lib/features/auth/auth.state.svelte';
	import { getWorkspace } from '$lib/api/workspaces';
	import { listTeams, createTeam } from '$lib/api/teams';
	import type { Workspace } from '$lib/types/workspace';
	import type { Team } from '$lib/types/team';
	import Sidebar from '$lib/components/layout/Sidebar.svelte';
	import CommandPalette from '$lib/components/layout/CommandPalette.svelte';
	import CreateIssueDialog from '$lib/features/issues/CreateIssueDialog.svelte';
	import CreateTeamDialog from '$lib/features/teams/CreateTeamDialog.svelte';
	import { issuesState } from '$lib/features/issues/issues.state.svelte';
	import { createKeyboardHandler } from '$lib/utils/keyboard';

	let { children } = $props();
	let workspace = $state<Workspace | null>(null);
	let teams = $state<Team[]>([]);
	let showCommandPalette = $state(false);
	let showCreateIssue = $state(false);
	let showCreateTeam = $state(false);

	const slug = $derived(page.params.workspaceSlug ?? '');

	onMount(async () => {
		await authState.init();
		if (!authState.authenticated) {
			goto('/login');
			return;
		}
		try {
			workspace = await getWorkspace(slug);
			teams = await listTeams(slug);
		} catch {
			goto('/login');
		}
	});

	const keyHandler = createKeyboardHandler([
		{ key: 'k', meta: true, handler: () => (showCommandPalette = !showCommandPalette) },
		{
			key: 'c',
			handler: () => {
				const active = document.activeElement;
				if (active && (active.tagName === 'INPUT' || active.tagName === 'TEXTAREA' || (active as HTMLElement).isContentEditable)) return;
				if (teams.length === 0) {
					showCreateTeam = true;
				} else {
					showCreateIssue = true;
				}
			}
		}
	]);

	onMount(() => {
		document.addEventListener('keydown', keyHandler);
		return () => document.removeEventListener('keydown', keyHandler);
	});

	async function handleCreateTeam(data: { name: string; key: string; description?: string }) {
		const team = await createTeam(slug, data);
		teams = [...teams, team];
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
		onsubmit={async (req) => {
			await issuesState.create(slug, req);
		}}
	/>

	<CreateTeamDialog
		bind:open={showCreateTeam}
		onsubmit={handleCreateTeam}
	/>
{:else}
	<div class="flex h-screen items-center justify-center">
		<div class="text-[var(--color-text-secondary)]">Loading...</div>
	</div>
{/if}
