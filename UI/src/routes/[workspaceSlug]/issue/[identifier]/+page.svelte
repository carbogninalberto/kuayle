<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { getIssue } from '$lib/api/issues';
	import type { Issue } from '$lib/types/issue';
	import IssueDetail from '$lib/features/issues/IssueDetail.svelte';
	import LoadingState from '$lib/components/shared/LoadingState.svelte';

	const slug = $derived(page.params.workspaceSlug ?? '');
	const identifier = $derived(page.params.identifier ?? '');

	let issue = $state<Issue | null>(null);

	onMount(async () => {
		issue = await getIssue(slug, identifier);
	});
</script>

{#if issue}
	<IssueDetail {issue} {slug} onclose={() => history.back()} />
{:else}
	<LoadingState />
{/if}
