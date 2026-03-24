<script lang="ts">
	import type { PublicIssue, PublicStatus } from '$lib/types/shared-link';
	import type { IssuePriority } from '$lib/types/issue';
	import IssueStatusIcon from './IssueStatusIcon.svelte';
	import IssuePriorityIcon from './IssuePriorityIcon.svelte';
	import type { StatusCategory } from '$lib/types/team-status';

	let {
		status,
		issues
	}: {
		status: PublicStatus;
		issues: PublicIssue[];
	} = $props();
</script>

<div class="flex w-72 shrink-0 flex-col">
	<div class="flex items-center gap-2 px-2 py-2">
		<IssueStatusIcon category={status.category as StatusCategory} color={status.color} />
		<span class="text-sm font-medium text-[var(--color-text-primary)]">{status.name}</span>
		<span class="text-xs text-[var(--color-text-tertiary)]">{issues.length}</span>
	</div>

	<div class="flex-1 space-y-1.5 overflow-y-auto px-1 pb-4 min-h-[60px]">
		{#each issues as issue (issue.identifier)}
			<div class="w-full rounded-md border border-[var(--app-border)] bg-[var(--color-bg-secondary)] p-3 text-left">
				<div class="flex items-center gap-2">
					<span class="text-xs text-[var(--color-text-tertiary)]">{issue.identifier}</span>
					<IssuePriorityIcon priority={issue.priority as IssuePriority} size={14} />
				</div>
				<p class="mt-1 line-clamp-2 text-sm text-[var(--color-text-primary)]">{issue.title}</p>
				{#if issue.labels && issue.labels.length > 0}
					<div class="mt-2 flex flex-wrap gap-1">
						{#each issue.labels.slice(0, 3) as label}
							<span class="flex items-center gap-1 rounded-full border border-[var(--app-border)] bg-[var(--color-bg)] px-1.5 py-0 text-[10px] leading-4 text-[var(--color-text-tertiary)]">
								<span class="h-1.5 w-1.5 rounded-full shrink-0" style="background-color: {label.color}"></span>
								{label.name}
							</span>
						{/each}
					</div>
				{/if}
				{#if issue.assignees && issue.assignees.length > 0}
					<div class="mt-2 flex items-center gap-1">
						{#each issue.assignees.slice(0, 3) as assignee}
							<div class="flex h-5 w-5 items-center justify-center rounded-full bg-[var(--app-accent)] text-[9px] font-medium text-[var(--app-accent-foreground)]" title={assignee.name}>
								{(assignee.name ?? 'U').charAt(0).toUpperCase()}
							</div>
						{/each}
					</div>
				{/if}
			</div>
		{/each}
	</div>
</div>
