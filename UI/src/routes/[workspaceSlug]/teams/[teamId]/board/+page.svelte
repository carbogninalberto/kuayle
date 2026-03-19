<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { issuesState } from '$lib/features/issues/issues.state.svelte';
	import KanbanBoard from '$lib/features/issues/KanbanBoard.svelte';
	import IssueDetail from '$lib/features/issues/IssueDetail.svelte';
	import LoadingState from '$lib/components/shared/LoadingState.svelte';

	const slug = $derived(page.params.workspaceSlug ?? '');
	const teamId = $derived(page.params.teamId ?? '');

	onMount(() => {
		issuesState.load(slug, { team: teamId, per_page: '200' });
	});
</script>

<div class="flex h-full flex-col">
	<div class="flex items-center gap-3 border-b border-[var(--app-border)] px-6 py-3">
		<h1 class="text-sm font-medium text-[var(--color-text-primary)]">Board</h1>
		<a
			href="/{slug}/teams/{teamId}"
			class="text-xs text-[var(--color-text-tertiary)] hover:text-[var(--color-text-secondary)]"
		>
			List view
		</a>
	</div>

	{#if issuesState.loading}
		<LoadingState />
	{:else}
		<div class="flex-1 overflow-hidden">
			<KanbanBoard
				issuesByStatus={issuesState.issuesByStatus}
				onissueclick={(i) => issuesState.select(i)}
			/>
		</div>
	{/if}
</div>

{#if issuesState.selectedIssue}
	<IssueDetail
		issue={issuesState.selectedIssue}
		{slug}
		onclose={() => issuesState.select(null)}
	/>
{/if}
