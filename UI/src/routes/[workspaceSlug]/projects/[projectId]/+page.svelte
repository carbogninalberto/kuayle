<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { getProject } from '$lib/api/projects';
	import { issuesState } from '$lib/features/issues/issues.state.svelte';
	import IssueRow from '$lib/features/issues/IssueRow.svelte';
	import IssueDetail from '$lib/features/issues/IssueDetail.svelte';
	import type { Project } from '$lib/types/project';
	import LoadingState from '$lib/components/shared/LoadingState.svelte';

	const slug = $derived(page.params.workspaceSlug ?? '');
	const projectId = $derived(page.params.projectId ?? '');

	let project = $state<Project | null>(null);

	onMount(async () => {
		project = await getProject(slug, projectId);
		issuesState.load(slug, { project: projectId });
	});
</script>

<div class="flex h-full flex-col">
	<div class="flex h-[49px] items-center border-b border-[var(--app-border)] px-6">
		<h1 class="text-sm font-medium text-[var(--color-text-primary)]">
			{project?.name ?? 'Loading...'}
		</h1>
		{#if project?.description}
			<p class="mt-1 text-xs text-[var(--color-text-secondary)]">{project.description}</p>
		{/if}
	</div>

	<div class="flex-1 overflow-y-auto">
		{#if issuesState.loading}
			<LoadingState />
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
