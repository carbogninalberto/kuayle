<script lang="ts">
	import type { Issue } from '$lib/types/issue';
	import IssueStatusIcon from './IssueStatusIcon.svelte';
	import IssuePriorityIcon from './IssuePriorityIcon.svelte';
	import { formatRelativeTime } from '$lib/utils/format';

	let { issue, onclick }: { issue: Issue; onclick: (issue: Issue) => void } = $props();
</script>

<button
	class="flex w-full items-center gap-3 border-b border-[var(--app-border)] px-4 py-2.5 text-left transition-colors hover:bg-[var(--color-bg-hover)]"
	onclick={() => onclick(issue)}
>
	<IssuePriorityIcon priority={issue.priority} />
	<span class="w-16 shrink-0 text-xs text-[var(--color-text-tertiary)]">{issue.identifier}</span>
	<IssueStatusIcon status={issue.status} />
	<span class="flex-1 truncate text-sm text-[var(--color-text-primary)]">{issue.title}</span>
	{#if issue.labels && issue.labels.length > 0}
		<div class="flex gap-1">
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
	<span class="shrink-0 text-xs text-[var(--color-text-tertiary)]"
		>{formatRelativeTime(issue.updated_at)}</span
	>
</button>
