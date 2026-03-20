<script lang="ts">
	import type { Issue, IssueStatus } from '$lib/types/issue';
	import { STATUS_ORDER } from '$lib/types/issue';
	import type { WorkspaceMember } from '$lib/types/workspace';
	import type { Label } from '$lib/types/label';
	import KanbanColumn from './KanbanColumn.svelte';
	import { issuesState } from './issues.state.svelte';

	let {
		issuesByStatus,
		slug = '',
		members = [],
		labels = [],
		onissueclick
	}: {
		issuesByStatus: Record<IssueStatus, Issue[]>;
		slug?: string;
		members?: WorkspaceMember[];
		labels?: Label[];
		onissueclick: (issue: Issue) => void;
	} = $props();

	// Local mutable copy for drag state
	let localByStatus = $state<Record<string, Issue[]>>({});

	$effect(() => {
		const copy: Record<string, Issue[]> = {};
		for (const status of STATUS_ORDER) {
			copy[status] = [...(issuesByStatus[status] ?? [])];
		}
		localByStatus = copy;
	});

	function handleConsider(status: IssueStatus, items: Issue[]) {
		localByStatus[status] = items;
	}

	function handleFinalize(status: IssueStatus, items: Issue[]) {
		localByStatus[status] = items;

		// Detect moved issues and update
		for (let i = 0; i < items.length; i++) {
			const issue = items[i];
			const originalStatus = issue.status;
			if (originalStatus !== status) {
				// Cross-column move: update status
				const sortOrder = calculateSortOrder(items, i);
				issuesState.update(slug, issue.identifier, { status, sort_order: sortOrder });
			} else {
				// Same column reorder: check if position changed
				const originalItems = issuesByStatus[status] ?? [];
				const originalIdx = originalItems.findIndex((it) => it.id === issue.id);
				if (originalIdx !== i) {
					const sortOrder = calculateSortOrder(items, i);
					issuesState.update(slug, issue.identifier, { sort_order: sortOrder });
				}
			}
		}
	}

	function calculateSortOrder(items: Issue[], index: number): number {
		const prev = index > 0 ? items[index - 1].sort_order : 0;
		const next = index < items.length - 1 ? items[index + 1].sort_order : prev + 2000;
		return (prev + next) / 2;
	}
</script>

<div class="flex gap-4 overflow-x-auto p-4">
	{#each STATUS_ORDER as status}
		<KanbanColumn
			{status}
			issues={localByStatus[status] ?? []}
			{slug}
			{members}
			{labels}
			{onissueclick}
			onconsider={handleConsider}
			onfinalize={handleFinalize}
		/>
	{/each}
</div>
