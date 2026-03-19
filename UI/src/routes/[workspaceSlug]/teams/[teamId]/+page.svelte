<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { issuesState } from '$lib/features/issues/issues.state.svelte';
	import IssueRow from '$lib/features/issues/IssueRow.svelte';
	import IssueDetail from '$lib/features/issues/IssueDetail.svelte';
	import FilterBar from '$lib/components/shared/FilterBar.svelte';
	import EmptyState from '$lib/components/shared/EmptyState.svelte';
	import LoadingState from '$lib/components/shared/LoadingState.svelte';
	import CreateIssueDialog from '$lib/features/issues/CreateIssueDialog.svelte';
	import { listTeams } from '$lib/api/teams';
	import { listProjects } from '$lib/api/projects';
	import { listLabels } from '$lib/api/labels';
	import { listMembers } from '$lib/api/members';
	import type { Team } from '$lib/types/team';
	import type { Project } from '$lib/types/project';
	import type { Label } from '$lib/types/label';
	import type { WorkspaceMember } from '$lib/types/workspace';
	import { createKeyboardHandler } from '$lib/utils/keyboard';
	import { toast } from 'svelte-sonner';
	import { Plus } from 'lucide-svelte';

	const slug = $derived(page.params.workspaceSlug ?? '');
	const teamId = $derived(page.params.teamId ?? '');

	let teams = $state<Team[]>([]);
	let projects = $state<Project[]>([]);
	let labels = $state<Label[]>([]);
	let members = $state<WorkspaceMember[]>([]);
	let showCreateIssue = $state(false);
	let filters = $state<Record<string, string>>({});

	onMount(async () => {
		const [t, p, l, m] = await Promise.all([
			listTeams(slug),
			listProjects(slug),
			listLabels(slug),
			listMembers(slug)
		]);
		teams = t;
		projects = p;
		labels = l;
		members = m;
		loadIssues();
	});

	function loadIssues() {
		issuesState.load(slug, { team: teamId, ...filters });
	}

	function handleFilterChange(f: Record<string, string>) {
		filters = f;
		loadIssues();
	}

	const keyHandler = createKeyboardHandler([
		{
			key: 'c',
			handler: () => {
				const active = document.activeElement;
				if (active && (active.tagName === 'INPUT' || active.tagName === 'TEXTAREA' || (active as HTMLElement).isContentEditable)) return;
				showCreateIssue = true;
			}
		}
	]);

	onMount(() => {
		document.addEventListener('keydown', keyHandler);
		return () => document.removeEventListener('keydown', keyHandler);
	});
</script>

<div class="flex h-full flex-col">
	<div
		class="flex h-[49px] items-center justify-between border-b border-[var(--app-border)] px-6"
	>
		<div class="flex items-center gap-3">
			<h1 class="text-sm font-medium text-[var(--color-text-primary)]">Issues</h1>
			<a
				href="/{slug}/teams/{teamId}/board"
				class="text-xs text-[var(--color-text-tertiary)] hover:text-[var(--color-text-secondary)]"
			>
				Board view
			</a>
		</div>
		<button
			onclick={() => (showCreateIssue = true)}
			class="flex items-center gap-1 rounded-md bg-[var(--app-accent)] px-3 py-1.5 text-sm text-white hover:bg-[var(--app-accent-hover)]"
		>
			<Plus size={14} />
			New Issue
		</button>
	</div>

	<FilterBar {filters} onchange={handleFilterChange} />

	<div class="flex-1 overflow-y-auto">
		{#if issuesState.loading}
			<LoadingState />
		{:else if issuesState.issues.length === 0}
			<EmptyState
				title="No issues yet"
				description="Create your first issue to get started"
				action={{ label: 'New Issue', onclick: () => (showCreateIssue = true) }}
			/>
		{:else}
			{#each issuesState.issues as issue (issue.id)}
				<IssueRow {issue} onclick={(i) => issuesState.select(i)} />
			{/each}
		{/if}
	</div>
</div>

{#if issuesState.selectedIssue}
	<IssueDetail
		issue={issuesState.selectedIssue}
		{slug}
		onclose={() => issuesState.select(null)}
	/>
{/if}

<CreateIssueDialog
	bind:open={showCreateIssue}
	{teams}
	{projects}
	{labels}
	{members}
	defaultTeamId={teamId}
	onsubmit={async (req) => {
		try {
			await issuesState.create(slug, req);
			toast.success('Issue created');
		} catch (err: any) {
			toast.error(err?.error?.message || 'Failed to create issue');
		}
	}}
/>
