<script lang="ts">
	import type { IssueStatus, IssuePriority } from '$lib/types/issue';
	import { STATUS_LABELS, PRIORITY_LABELS } from '$lib/types/issue';
	import IssueStatusIcon from './IssueStatusIcon.svelte';
	import IssuePriorityIcon from './IssuePriorityIcon.svelte';
	import type { GroupByField } from './issues.state.svelte';
	import { ChevronDown, ChevronRight } from 'lucide-svelte';

	let {
		groupKey,
		groupBy,
		count,
		collapsed = false,
		ontoggle
	}: {
		groupKey: string;
		groupBy: GroupByField;
		count: number;
		collapsed?: boolean;
		ontoggle: () => void;
	} = $props();

	const label = $derived.by(() => {
		switch (groupBy) {
			case 'status':
				return STATUS_LABELS[groupKey as IssueStatus] ?? groupKey;
			case 'priority':
				return PRIORITY_LABELS[Number(groupKey) as IssuePriority] ?? groupKey;
			case 'assignee':
				return groupKey === 'unassigned' ? 'Unassigned' : groupKey;
			case 'project':
				return groupKey === 'no-project' ? 'No Project' : groupKey;
			default:
				return groupKey;
		}
	});
</script>

<button
	class="flex w-full items-center gap-2 bg-[var(--color-bg-secondary)] px-4 py-2 text-sm font-medium text-[var(--color-text-primary)]"
	onclick={ontoggle}
>
	{#if collapsed}
		<ChevronRight size={14} class="text-[var(--color-text-tertiary)]" />
	{:else}
		<ChevronDown size={14} class="text-[var(--color-text-tertiary)]" />
	{/if}

	{#if groupBy === 'status'}
		<IssueStatusIcon status={groupKey as IssueStatus} size={14} />
	{:else if groupBy === 'priority'}
		<IssuePriorityIcon priority={Number(groupKey) as IssuePriority} size={14} />
	{/if}

	<span>{label}</span>
	<span class="text-xs text-[var(--color-text-tertiary)]">{count}</span>
</button>
