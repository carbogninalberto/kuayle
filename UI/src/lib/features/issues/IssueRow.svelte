<script lang="ts">
	import type { Issue } from '$lib/types/issue';
	import type { WorkspaceMember } from '$lib/types/workspace';
	import type { Label } from '$lib/types/label';
	import IssueStatusIcon from './IssueStatusIcon.svelte';
	import IssuePriorityIcon from './IssuePriorityIcon.svelte';
	import IssueContextMenu from './IssueContextMenu.svelte';
	import { Checkbox } from '$lib/components/ui/checkbox';
	import { issuesState } from './issues.state.svelte';
	import { formatRelativeTime } from '$lib/utils/format';

	let {
		issue,
		slug = '',
		members = [],
		labels = [],
		onclick,
		lastSelectedId = null
	}: {
		issue: Issue;
		slug?: string;
		members?: WorkspaceMember[];
		labels?: Label[];
		onclick: (issue: Issue) => void;
		lastSelectedId?: string | null;
	} = $props();

	const isSelected = $derived(issuesState.selectedIds.has(issue.id));

	function handleCheckboxChange(e: Event) {
		e.stopPropagation();
		issuesState.toggleSelect(issue.id);
	}

	function handleClick(e: MouseEvent) {
		if (e.shiftKey && lastSelectedId) {
			e.preventDefault();
			issuesState.selectRange(lastSelectedId, issue.id);
		} else {
			onclick(issue);
		}
	}
</script>

<IssueContextMenu {issue} {slug} {members} {labels}>
	<button
		class="group flex w-full items-center gap-2 border-b border-[var(--app-border)] px-3 py-1.5 text-left transition-colors duration-100 hover:bg-[var(--color-bg-hover)] {isSelected ? 'bg-[var(--color-bg-hover)]' : ''}"
		onclick={handleClick}
	>
		<!-- Checkbox -->
		<span
			class="shrink-0 transition-opacity duration-100 {isSelected ? 'opacity-100' : 'opacity-0 group-hover:opacity-100'}"
			onclick={handleCheckboxChange}
			onkeydown={(e) => { if (e.key === 'Enter' || e.key === ' ') handleCheckboxChange(e); }}
			role="checkbox"
			aria-checked={isSelected}
			tabindex={0}
		>
			<Checkbox checked={isSelected} />
		</span>

		<!-- Priority -->
		<span class="shrink-0"><IssuePriorityIcon priority={issue.priority} size={14} /></span>

		<!-- Identifier -->
		<span class="w-[4.5rem] shrink-0 text-xs tabular-nums text-[var(--color-text-tertiary)]">{issue.identifier}</span>

		<!-- Status -->
		<span class="shrink-0"><IssueStatusIcon status={issue.status} size={14} /></span>

		<!-- Title -->
		<span class="flex-1 truncate text-[13px] text-[var(--color-text-primary)]">{issue.title}</span>

		<!-- Labels -->
		{#if issue.labels && issue.labels.length > 0}
			<div class="hidden gap-1 shrink-0 sm:flex">
				{#each issue.labels.slice(0, 2) as label}
					<span
						class="rounded-full border px-1.5 py-0 text-[11px] leading-5"
						style="border-color: {label.color}40; color: {label.color}"
					>
						{label.name}
					</span>
				{/each}
				{#if issue.labels.length > 2}
					<span class="text-[11px] text-[var(--color-text-tertiary)]">+{issue.labels.length - 2}</span>
				{/if}
			</div>
		{/if}

		<!-- Due date -->
		{#if issue.due_date}
			{@const due = new Date(issue.due_date)}
			{@const now = new Date()}
			{@const diffDays = Math.ceil((due.getTime() - now.getTime()) / 86400000)}
			<span class="hidden shrink-0 text-[11px] sm:inline {diffDays < 0 ? 'text-red-500' : diffDays === 0 ? 'text-orange-500' : diffDays <= 7 ? 'text-[var(--color-text-secondary)]' : 'text-[var(--color-text-tertiary)]'}">
				{due.toLocaleDateString('en-US', { month: 'short', day: 'numeric' })}
			</span>
		{/if}

		<!-- Assignee -->
		{#if issue.assignee}
			<div class="flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-[var(--app-accent)] text-[9px] font-medium text-white" title={issue.assignee.name}>
				{(issue.assignee.name ?? 'U').charAt(0).toUpperCase()}
			</div>
		{:else}
			<div class="h-5 w-5 shrink-0"></div>
		{/if}
	</button>
</IssueContextMenu>
