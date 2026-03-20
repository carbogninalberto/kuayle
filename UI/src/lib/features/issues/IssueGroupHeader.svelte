<script lang="ts">
	import type { IssueStatus, IssuePriority } from '$lib/types/issue';
	import { STATUS_LABELS, PRIORITY_LABELS } from '$lib/types/issue';
	import type { WorkspaceMember } from '$lib/types/workspace';
	import type { Project } from '$lib/types/project';
	import IssueStatusIcon from './IssueStatusIcon.svelte';
	import IssuePriorityIcon from './IssuePriorityIcon.svelte';
	import type { GroupByField } from './issues.state.svelte';
	import { ChevronDown, ChevronRight, User } from 'lucide-svelte';

	let {
		groupKey,
		groupBy,
		count,
		collapsed = false,
		members = [],
		projects = [],
		ontoggle
	}: {
		groupKey: string;
		groupBy: GroupByField;
		count: number;
		collapsed?: boolean;
		members?: WorkspaceMember[];
		projects?: Project[];
		ontoggle: () => void;
	} = $props();

	const label = $derived.by(() => {
		switch (groupBy) {
			case 'status':
				return STATUS_LABELS[groupKey as IssueStatus] ?? groupKey;
			case 'priority':
				return PRIORITY_LABELS[Number(groupKey) as IssuePriority] ?? groupKey;
			case 'assignee': {
				if (groupKey === 'unassigned') return 'Unassigned';
				const member = members.find(m => m.user_id === groupKey);
				return member ? (member.name || member.email) : groupKey;
			}
			case 'project': {
				if (groupKey === 'no-project') return 'No Project';
				const project = projects.find(p => p.id === groupKey);
				return project ? project.name : groupKey;
			}
			default:
				return groupKey;
		}
	});
</script>

<button
	class="flex w-full items-center gap-2 border-b border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-3 py-1.5 text-xs font-medium text-[var(--color-text-primary)] transition-colors hover:bg-[var(--color-bg-hover)]"
	onclick={ontoggle}
>
	{#if collapsed}
		<ChevronRight size={12} class="text-[var(--color-text-tertiary)]" />
	{:else}
		<ChevronDown size={12} class="text-[var(--color-text-tertiary)]" />
	{/if}

	{#if groupBy === 'status'}
		<IssueStatusIcon status={groupKey as IssueStatus} size={14} />
	{:else if groupBy === 'priority'}
		<IssuePriorityIcon priority={Number(groupKey) as IssuePriority} size={14} />
	{:else if groupBy === 'assignee'}
		{#if groupKey !== 'unassigned'}
			<div class="flex h-4 w-4 items-center justify-center rounded-full bg-[var(--app-accent)] text-[8px] font-medium text-white">
				{label.charAt(0).toUpperCase()}
			</div>
		{:else}
			<User size={14} class="text-[var(--color-text-tertiary)]" />
		{/if}
	{/if}

	<span>{label}</span>
	<span class="text-[10px] font-normal text-[var(--color-text-tertiary)]">{count}</span>
</button>
