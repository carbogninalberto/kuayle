<script lang="ts">
	import type { PublicIssue } from '$lib/types/shared-link';
	import type { IssuePriority } from '$lib/types/issue';
	import IssueStatusIcon from './IssueStatusIcon.svelte';
	import IssuePriorityIcon from './IssuePriorityIcon.svelte';
	import { formatRelativeTime } from '$lib/utils/format';
	import { CalendarDays } from 'lucide-svelte';

	let {
		issue,
		onclick
	}: {
		issue: PublicIssue;
		onclick: (issue: PublicIssue) => void;
	} = $props();
</script>

<button
	class="mx-2 flex w-[calc(100%-1rem)] items-center gap-2 rounded-md px-3 py-1.5 text-left transition-colors duration-100 hover:bg-black/[0.02] dark:hover:bg-white/[0.02]"
	onclick={() => onclick(issue)}
>
	<!-- Priority -->
	<span class="shrink-0 flex items-center opacity-60">
		<IssuePriorityIcon priority={issue.priority as IssuePriority} size={14} />
	</span>

	<!-- Identifier -->
	<span class="w-[3.75rem] shrink-0 text-xs tabular-nums text-[var(--color-text-tertiary)]">{issue.identifier}</span>

	<!-- Status -->
	<span class="shrink-0 flex items-center opacity-60">
		<IssueStatusIcon
			status={issue.status}
			category={issue.status_info?.category}
			color={issue.status_info?.color}
			size={14}
		/>
	</span>

	<!-- Title -->
	<span class="flex-1 truncate text-[13px] text-[var(--color-text-primary)]">{issue.title}</span>

	<!-- Labels -->
	{#if issue.labels && issue.labels.length > 0}
		<div class="hidden gap-1 shrink-0 sm:flex">
			{#each issue.labels.slice(0, 2) as label}
				<span class="flex items-center gap-1 rounded-full border border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-1.5 py-0 text-[11px] leading-5 text-[var(--color-text-tertiary)]">
					<span class="h-1.5 w-1.5 rounded-full shrink-0" style="background-color: {label.color}"></span>
					{label.name}
				</span>
			{/each}
			{#if issue.labels.length > 2}
				<span class="text-[10px] text-[var(--color-text-tertiary)]">+{issue.labels.length - 2}</span>
			{/if}
		</div>
	{/if}

	<!-- Due date -->
	{#if issue.due_date}
		{@const due = new Date(issue.due_date)}
		{@const now = new Date()}
		{@const diffDays = Math.ceil((due.getTime() - now.getTime()) / 86400000)}
		<span class="hidden shrink-0 items-center gap-1 rounded-full border border-[var(--app-border)] px-1.5 py-0 text-[11px] leading-5 sm:inline-flex">
			<CalendarDays size={11} class={diffDays < 0 ? 'text-red-500' : diffDays === 0 ? 'text-orange-500' : diffDays <= 7 ? 'text-[var(--color-text-secondary)]' : 'text-[var(--color-text-tertiary)]'} />
			<span class="text-[var(--color-text-tertiary)]">{due.toLocaleDateString('en-US', { month: 'short', day: 'numeric' })}</span>
		</span>
	{/if}

	<!-- Assignees -->
	{#if issue.assignees && issue.assignees.length > 0}
		<span class="shrink-0 flex items-center">
			{#if issue.assignees.length > 1}
				<div class="flex -space-x-2">
					<div class="flex h-5 w-5 items-center justify-center rounded-full bg-[var(--app-accent)] text-[9px] font-medium text-[var(--app-accent-foreground)] ring-1 ring-[var(--color-bg)]" title={issue.assignees[0].name}>
						{(issue.assignees[0].name ?? 'U').charAt(0).toUpperCase()}
					</div>
					<div class="flex h-5 w-5 items-center justify-center rounded-full bg-[var(--color-bg-tertiary)] text-[8px] font-medium text-[var(--color-text-secondary)] ring-1 ring-[var(--color-bg)]">
						+{issue.assignees.length - 1}
					</div>
				</div>
			{:else}
				<div class="flex h-5 w-5 items-center justify-center rounded-full bg-[var(--app-accent)] text-[9px] font-medium text-[var(--app-accent-foreground)]" title={issue.assignees[0].name}>
					{(issue.assignees[0].name ?? 'U').charAt(0).toUpperCase()}
				</div>
			{/if}
		</span>
	{/if}

	<!-- Created -->
	{#if issue.created_at}
		<span class="hidden shrink-0 text-[11px] text-[var(--color-text-tertiary)] sm:inline">
			{formatRelativeTime(issue.created_at)}
		</span>
	{/if}
</button>
