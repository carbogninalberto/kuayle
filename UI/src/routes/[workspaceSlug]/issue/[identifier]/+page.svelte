<script lang="ts">
	import { onMount } from 'svelte';
	import { m } from '$lib/paraglide/messages.js';
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
	let refresh = $state(0);
	let unavailable = $state(false);
	onMount(() => {
		const reload = (event: Event) => {
			const detail = (event as CustomEvent<{slug?: string; resources?: string[]}>).detail;
			if (detail?.slug && detail.slug !== slug) return;
			if (!detail?.resources?.length || detail.resources.some(r => ['issues', 'teams', 'members', 'workspace'].includes(r))) refresh++;
		};
		window.addEventListener('app:refresh', reload);
		return () => { requestId++; window.removeEventListener('app:refresh', reload); };
	});

	$effect(() => {
		void refresh;
		if (identifier && slug) {
			const requestSlug = slug;
			const nextIssueKey = `${slug}/${identifier}`;
			unavailable = false;
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
					teamStatusesState.reload(requestSlug, i.team_id);
					issuesState.load(requestSlug, { team: i.team_id });
				}
			}).catch(() => {
				if (currentRequest !== requestId) return;
				issue = null;
				unavailable = true;
			});
			return () => { requestId++; };
		}
	});

	function handleNavigate(direction: 'prev' | 'next') {
		const adj = issuesState.getAdjacentIdentifier(identifier, direction);
		if (adj) {
			// Replace history so browser back returns to the view the
			// issue was opened from instead of the previous issue.
			goto(`/${slug}/issue/${adj}`, { replaceState: true });
		}
	}

	function handleIssueUpdated(updated: Issue) {
		if (updated.identifier === identifier && issue?.id === updated.id) issue = updated;
	}
</script>

{#if unavailable}
	<p class="p-6 text-sm text-muted-foreground">{m['privacy.unavailable']()}</p>
{:else if issue}
	{#key issue.identifier}
		<FullPageIssueView
			{issue}
			{slug}
			onnavigate={handleNavigate}
			onupdated={handleIssueUpdated}
		/>
	{/key}
{/if}
