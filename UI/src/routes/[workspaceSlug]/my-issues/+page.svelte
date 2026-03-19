<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { authState } from '$lib/features/auth/auth.state.svelte';
	import { issuesState } from '$lib/features/issues/issues.state.svelte';
	import IssueRow from '$lib/features/issues/IssueRow.svelte';
	import IssueDetail from '$lib/features/issues/IssueDetail.svelte';
	import EmptyState from '$lib/components/shared/EmptyState.svelte';
	import LoadingState from '$lib/components/shared/LoadingState.svelte';

	const slug = $derived(page.params.workspaceSlug ?? '');

	onMount(() => {
		if (authState.user) {
			issuesState.load(slug, { assignee: authState.user.id });
		}
	});

	function handleIssueClick(issue: any) {
		issuesState.select(issue);
	}
</script>

<div class="h-full">
	<div class="flex h-[49px] items-center border-b border-[var(--app-border)] px-6">
		<h1 class="text-sm font-medium text-[var(--color-text-primary)]">My Issues</h1>
	</div>

	{#if issuesState.loading}
		<LoadingState />
	{:else if issuesState.issues.length === 0}
		<EmptyState
			title="No issues assigned to you"
			description="Issues assigned to you will appear here"
		/>
	{:else}
		{#each issuesState.issues as issue (issue.id)}
			<IssueRow {issue} onclick={handleIssueClick} />
		{/each}
	{/if}
</div>

{#if issuesState.selectedIssue}
	<IssueDetail
		issue={issuesState.selectedIssue}
		{slug}
		onclose={() => issuesState.select(null)}
	/>
{/if}
