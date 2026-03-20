<script lang="ts">
	import { onMount } from 'svelte';
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

	onMount(async () => {
		issue = await getIssue(slug, identifier);
	});

	$effect(() => {
		// Re-fetch when identifier changes (prev/next navigation)
		if (identifier) {
			getIssue(slug, identifier).then((i) => (issue = i));
		}
	});

	function handleNavigate(direction: 'prev' | 'next') {
		const adj = issuesState.getAdjacentIdentifier(identifier, direction);
		if (adj) {
			goto(`/${slug}/issue/${adj}`);
		}
	}
</script>

{#if issue}
	<FullPageIssueView
		{issue}
		{slug}
		onnavigate={handleNavigate}
		onclose={() => history.back()}
	/>
{:else}
	<LoadingState />
{/if}
