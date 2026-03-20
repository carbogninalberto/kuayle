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
		class="group flex w-full items-center gap-3 border-b border-[var(--app-border)] px-4 py-2.5 text-left transition-colors hover:bg-[var(--color-bg-hover)] {isSelected ? 'bg-[var(--color-bg-hover)]' : ''}"
		onclick={handleClick}
	>
		<span
			class="shrink-0 {isSelected ? 'opacity-100' : 'opacity-0 group-hover:opacity-100'}"
			onclick={handleCheckboxChange}
			onkeydown={(e) => { if (e.key === 'Enter' || e.key === ' ') handleCheckboxChange(e); }}
			role="checkbox"
			aria-checked={isSelected}
			tabindex={0}
		>
			<Checkbox checked={isSelected} />
		</span>
		<IssuePriorityIcon priority={issue.priority} />
		<span class="w-16 shrink-0 text-xs text-[var(--color-text-tertiary)]">{issue.identifier}</span>
		<IssueStatusIcon status={issue.status} />
		<span class="flex-1 truncate text-sm text-[var(--color-text-primary)]">{issue.title}</span>
		{#if issue.labels && issue.labels.length > 0}
			<div class="flex gap-1 shrink-0">
				{#each issue.labels.slice(0, 3) as label}
					<span
						class="rounded-full px-2 py-0.5 text-xs"
						style="background-color: {label.color}20; color: {label.color}"
					>
						{label.name}
					</span>
				{/each}
			</div>
		{/if}
		{#if issue.assignee}
			<div class="flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-[var(--app-accent)] text-[9px] font-medium text-white" title={issue.assignee.name}>
				{(issue.assignee.name ?? 'U').charAt(0).toUpperCase()}
			</div>
		{/if}
		{#if issue.due_date}
			<span class="shrink-0 text-xs text-[var(--color-text-tertiary)]">{new Date(issue.due_date).toLocaleDateString('en-US', { month: 'short', day: 'numeric' })}</span>
		{/if}
		<span class="shrink-0 text-xs text-[var(--color-text-tertiary)]"
			>{formatRelativeTime(issue.updated_at)}</span
		>
	</button>
</IssueContextMenu>
