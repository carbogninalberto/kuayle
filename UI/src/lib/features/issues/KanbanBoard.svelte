<script lang="ts">
	import type { Issue, IssueStatus } from '$lib/types/issue';
	import { STATUS_ORDER } from '$lib/types/issue';
	import KanbanColumn from './KanbanColumn.svelte';

	let {
		issuesByStatus,
		onissueclick
	}: {
		issuesByStatus: Record<IssueStatus, Issue[]>;
		onissueclick: (issue: Issue) => void;
	} = $props();
</script>

<div class="flex gap-4 overflow-x-auto p-4">
	{#each STATUS_ORDER as status}
		<KanbanColumn {status} issues={issuesByStatus[status] ?? []} {onissueclick} />
	{/each}
</div>
