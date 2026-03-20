<script lang="ts">
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { getIssue } from '$lib/api/issues';
	import type { Issue } from '$lib/types/issue';
	import { issuesState } from '$lib/features/issues/issues.state.svelte';
	import FullPageIssueView from '$lib/features/issues/FullPageIssueView.svelte';
	import LoadingState from '$lib/components/shared/LoadingState.svelte';

	const slug = $derived(page.params.workspaceSlug ?? '');
	const identifier = $derived(page.params.identifier ?? '');

	let issue = $state<Issue | null>(null);

	$effect(() => {
		if (identifier && slug) {
			issue = null;
			getIssue(slug, identifier).then((i) => (issue = i));
		}
	});

	function handleNavigate(direction: 'prev' | 'next') {
		const adj = issuesState.getAdjacentIdentifier(identifier, direction);
		if (adj) {
			goto(`/${slug}/issue/${adj}`);
		}
	}

	function handleIssueUpdated(updated: Issue) {
		issue = updated;
	}
</script>

{#if issue}
	{#key issue.identifier}
		<FullPageIssueView
			{issue}
			{slug}
			onnavigate={handleNavigate}
			onupdated={handleIssueUpdated}
		/>
	{/key}
{:else}
	<LoadingState />
{/if}
