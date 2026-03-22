<script lang="ts">
	import type { IssueStatus, IssuePriority } from '$lib/types/issue';
	import { PRIORITY_LABELS } from '$lib/types/issue';
	import { teamStatusesState } from './team-statuses.state.svelte';
	import type { WorkspaceMember } from '$lib/types/workspace';
	import type { Project } from '$lib/types/project';
	import IssueStatusIcon from './IssueStatusIcon.svelte';
	import IssuePriorityIcon from './IssuePriorityIcon.svelte';
	import type { GroupByField } from './issues.state.svelte';
	import { ChevronDown, ChevronRight, User, Plus } from 'lucide-svelte';

	let {
		groupKey,
		groupBy,
		groupLabel,
		count,
		collapsed = false,
		members = [],
		projects = [],
		ontoggle,
		onquickadd
	}: {
		groupKey: string;
		groupBy: GroupByField;
		groupLabel?: string;
		count: number;
		collapsed?: boolean;
		members?: WorkspaceMember[];
		projects?: Project[];
		ontoggle: () => void;
		onquickadd?: (groupKey: string) => void;
	} = $props();

	const label = $derived.by(() => {
		switch (groupBy) {
			case 'status': {
				const ts = teamStatusesState.statusById.get(groupKey);
				if (ts) return ts.name;
				// Fall back to label from groupedIssues (derived from status_info)
				return groupLabel ?? groupKey;
			}
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

<div class="mt-1 first:mt-0 mx-2">
	<button
		class="flex w-full items-center gap-2 rounded-md bg-[var(--color-bg-secondary)] px-4 py-1.5 text-xs font-medium text-[var(--color-text-secondary)]"
		onclick={ontoggle}
	>
		{#if collapsed}
			<ChevronRight size={12} class="text-[var(--color-text-tertiary)]" />
		{:else}
			<ChevronDown size={12} class="text-[var(--color-text-tertiary)]" />
		{/if}

		{#if groupBy === 'status'}
			{@const ts = teamStatusesState.statusById.get(groupKey)}
			<IssueStatusIcon status={groupKey as IssueStatus} category={ts?.category} color={ts?.color} size={14} />
		{:else if groupBy === 'priority'}
			<IssuePriorityIcon priority={Number(groupKey) as IssuePriority} size={14} />
		{:else if groupBy === 'assignee'}
			{#if groupKey !== 'unassigned'}
				<div class="flex h-4 w-4 items-center justify-center rounded-full bg-[var(--app-accent)] text-[8px] font-medium text-[var(--app-accent-foreground)]">
					{label.charAt(0).toUpperCase()}
				</div>
			{:else}
				<User size={14} class="text-[var(--color-text-tertiary)]" />
			{/if}
		{/if}

		<span>{label}</span>
		<span class="text-[10px] font-normal text-[var(--color-text-tertiary)]">{count}</span>
		{#if onquickadd}
			<button
				onclick={(e) => { e.stopPropagation(); onquickadd?.(groupKey); }}
				class="ml-auto rounded p-0.5 text-[var(--color-text-tertiary)] hover:text-[var(--color-text-primary)] hover:bg-[var(--color-bg-tertiary)] transition-colors"
			>
				<Plus size={14} />
			</button>
		{/if}
	</button>
</div>
