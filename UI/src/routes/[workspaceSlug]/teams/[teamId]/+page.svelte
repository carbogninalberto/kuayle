<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { issuesState } from '$lib/features/issues/issues.state.svelte';
	import IssueRow from '$lib/features/issues/IssueRow.svelte';
	import IssueForm from '$lib/features/issues/IssueForm.svelte';
	import IssueDetail from '$lib/features/issues/IssueDetail.svelte';
	import FilterBar from '$lib/components/shared/FilterBar.svelte';
	import EmptyState from '$lib/components/shared/EmptyState.svelte';
	import LoadingState from '$lib/components/shared/LoadingState.svelte';
	import { listTeams } from '$lib/api/teams';
	import type { Team } from '$lib/types/team';
	import { createKeyboardHandler } from '$lib/utils/keyboard';
	import { Plus } from 'lucide-svelte';

	const slug = $derived(page.params.workspaceSlug ?? '');
	const teamId = $derived(page.params.teamId ?? '');

	let teams = $state<Team[]>([]);
	let showForm = $state(false);
	let filters = $state<Record<string, string>>({});

	onMount(async () => {
		teams = await listTeams(slug);
		loadIssues();
	});

	function loadIssues() {
		issuesState.load(slug, { team: teamId, ...filters });
	}

	async function handleCreate(req: any) {
		await issuesState.create(slug, req);
		showForm = false;
	}

	function handleFilterChange(f: Record<string, string>) {
		filters = f;
		loadIssues();
	}

	const keyHandler = createKeyboardHandler([
		{ key: 'c', handler: () => (showForm = true) }
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
			onclick={() => (showForm = true)}
			class="flex items-center gap-1 rounded-md bg-[var(--app-accent)] px-3 py-1.5 text-sm text-white hover:bg-[var(--app-accent-hover)]"
		>
			<Plus size={14} />
			New Issue
		</button>
	</div>

	<FilterBar {filters} onchange={handleFilterChange} />

	{#if showForm}
		<div class="border-b border-[var(--app-border)]">
			<IssueForm {teams} onsubmit={handleCreate} oncancel={() => (showForm = false)} />
		</div>
	{/if}

	<div class="flex-1 overflow-y-auto">
		{#if issuesState.loading}
			<LoadingState />
		{:else if issuesState.issues.length === 0}
			<EmptyState
				title="No issues yet"
				description="Create your first issue to get started"
				action={{ label: 'New Issue', onclick: () => (showForm = true) }}
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
