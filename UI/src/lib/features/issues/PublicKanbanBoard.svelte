<script lang="ts">
	import type { PublicIssue, PublicStatus } from '$lib/types/shared-link';
	import PublicKanbanColumn from './PublicKanbanColumn.svelte';

	let {
		issues,
		statuses
	}: {
		issues: PublicIssue[];
		statuses: PublicStatus[];
	} = $props();

	const issuesByStatus = $derived(() => {
		const map: Record<string, PublicIssue[]> = {};
		for (const st of statuses) {
			map[st.id] = [];
		}
		for (const issue of issues) {
			const statusId = issue.status_info?.id;
			if (statusId && map[statusId]) {
				map[statusId].push(issue);
			}
		}
		return map;
	});

	const sortedStatuses = $derived(
		[...statuses].sort((a, b) => a.position - b.position)
	);
</script>

<div class="flex h-full gap-4 overflow-x-auto p-4">
	{#each sortedStatuses as status (status.id)}
		<PublicKanbanColumn
			{status}
			issues={issuesByStatus()[status.id] ?? []}
		/>
	{/each}
</div>
