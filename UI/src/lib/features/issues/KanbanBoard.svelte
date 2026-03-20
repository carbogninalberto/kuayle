<script lang="ts">
	import type { Issue } from '$lib/types/issue';
	import type { TeamStatus } from '$lib/types/team-status';
	import type { WorkspaceMember } from '$lib/types/workspace';
	import type { Label } from '$lib/types/label';
	import KanbanColumn from './KanbanColumn.svelte';
	import { issuesState } from './issues.state.svelte';
	import { teamStatusesState } from './team-statuses.state.svelte';

	let {
		issuesByStatus,
		slug = '',
		members = [],
		labels = [],
		onissueclick
	}: {
		issuesByStatus: Record<string, Issue[]>;
		slug?: string;
		members?: WorkspaceMember[];
		labels?: Label[];
		onissueclick: (issue: Issue) => void;
	} = $props();

	// Local mutable copy for drag state
	let localByStatus = $state<Record<string, Issue[]>>({});

	$effect(() => {
		const copy: Record<string, Issue[]> = {};
		for (const ts of teamStatusesState.statusOrder) {
			copy[ts.id] = [...(issuesByStatus[ts.id] ?? [])];
		}
		localByStatus = copy;
	});

	function handleConsider(statusId: string, items: Issue[]) {
		localByStatus[statusId] = items;
	}

	function handleFinalize(statusId: string, items: Issue[]) {
		localByStatus[statusId] = items;

		// Detect moved issues and update
		for (let i = 0; i < items.length; i++) {
			const issue = items[i];
			const originalStatusId = issue.status_id ?? issue.status;
			if (originalStatusId !== statusId) {
				// Cross-column move: update status_id
				const sortOrder = calculateSortOrder(items, i);
				issuesState.update(slug, issue.identifier, { status_id: statusId, sort_order: sortOrder });
			} else {
				// Same column reorder: check if position changed
				const originalItems = issuesByStatus[statusId] ?? [];
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
	{#each teamStatusesState.statusOrder as ts (ts.id)}
		<KanbanColumn
			statusId={ts.id}
			teamStatus={ts}
			issues={localByStatus[ts.id] ?? []}
			{slug}
			{members}
			{labels}
			{onissueclick}
			onconsider={handleConsider}
			onfinalize={handleFinalize}
		/>
	{/each}
</div>
