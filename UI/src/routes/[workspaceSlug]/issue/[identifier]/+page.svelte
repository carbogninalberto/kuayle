<script lang="ts">
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { getIssue } from '$lib/api/issues';
	import type { Issue } from '$lib/types/issue';
	import { issuesState } from '$lib/features/issues/issues.state.svelte';
	import { teamStatusesState } from '$lib/features/issues/team-statuses.state.svelte';
	import FullPageIssueView from '$lib/features/issues/FullPageIssueView.svelte';

	const slug = $derived(page.params.workspaceSlug ?? '');
	const identifier = $derived(page.params.identifier ?? '');

	let issue = $state<Issue | null>(null);
	let requestId = 0;
	let issueKey = '';

	$effect(() => {
		if (identifier && slug) {
			const nextIssueKey = `${slug}/${identifier}`;
			const currentRequest = ++requestId;
			if (nextIssueKey !== issueKey) {
				issueKey = nextIssueKey;
				issue = null;
			}
			getIssue(slug, identifier).then((i) => {
				if (currentRequest !== requestId) return;
				issue = i;
				// Load team issues for prev/next navigation if not already loaded
				if (issuesState.issues.length === 0 && i.team_id) {
					teamStatusesState.reload(slug, i.team_id);
					issuesState.load(slug, { team: i.team_id });
				}
			});
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
{/if}
