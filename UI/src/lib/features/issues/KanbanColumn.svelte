<script lang="ts">
	import type { Issue, IssueStatus } from '$lib/types/issue';
	import { STATUS_LABELS } from '$lib/types/issue';
	import IssueStatusIcon from './IssueStatusIcon.svelte';
	import IssuePriorityIcon from './IssuePriorityIcon.svelte';

	let {
		status,
		issues,
		onissueclick
	}: {
		status: IssueStatus;
		issues: Issue[];
		onissueclick: (issue: Issue) => void;
	} = $props();
</script>

<div class="flex w-72 shrink-0 flex-col">
	<div class="flex items-center gap-2 px-2 py-2">
		<IssueStatusIcon {status} />
		<span class="text-sm font-medium text-[var(--color-text-primary)]"
			>{STATUS_LABELS[status]}</span
		>
		<span class="text-xs text-[var(--color-text-tertiary)]">{issues.length}</span>
	</div>

	<div class="flex-1 space-y-1.5 overflow-y-auto px-1 pb-4">
		{#each issues as issue (issue.id)}
			<button
				class="w-full rounded-md border border-[var(--app-border)] bg-[var(--color-bg-secondary)] p-3 text-left transition-colors hover:border-[var(--app-border-hover)]"
				onclick={() => onissueclick(issue)}
			>
				<div class="flex items-center gap-2">
					<span class="text-xs text-[var(--color-text-tertiary)]">{issue.identifier}</span>
					<IssuePriorityIcon priority={issue.priority} size={14} />
				</div>
				<p class="mt-1 line-clamp-2 text-sm text-[var(--color-text-primary)]">{issue.title}</p>
			</button>
		{/each}
	</div>
</div>
