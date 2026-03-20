<script lang="ts">
	import type { Issue, IssueStatus } from '$lib/types/issue';
	import { STATUS_LABELS } from '$lib/types/issue';
	import type { WorkspaceMember } from '$lib/types/workspace';
	import type { Label } from '$lib/types/label';
	import IssueStatusIcon from './IssueStatusIcon.svelte';
	import IssuePriorityIcon from './IssuePriorityIcon.svelte';
	import IssueContextMenu from './IssueContextMenu.svelte';
	import { dndzone } from 'svelte-dnd-action';

	let {
		status,
		issues,
		slug = '',
		members = [],
		labels = [],
		onissueclick,
		onconsider,
		onfinalize
	}: {
		status: IssueStatus;
		issues: Issue[];
		slug?: string;
		members?: WorkspaceMember[];
		labels?: Label[];
		onissueclick: (issue: Issue) => void;
		onconsider?: (status: IssueStatus, items: Issue[]) => void;
		onfinalize?: (status: IssueStatus, items: Issue[]) => void;
	} = $props();

	function handleConsider(e: CustomEvent<{ items: Issue[] }>) {
		onconsider?.(status, e.detail.items);
	}

	function handleFinalize(e: CustomEvent<{ items: Issue[] }>) {
		onfinalize?.(status, e.detail.items);
	}
</script>

<div class="flex w-72 shrink-0 flex-col">
	<div class="flex items-center gap-2 px-2 py-2">
		<IssueStatusIcon {status} />
		<span class="text-sm font-medium text-[var(--color-text-primary)]"
			>{STATUS_LABELS[status]}</span
		>
		<span class="text-xs text-[var(--color-text-tertiary)]">{issues.length}</span>
	</div>

	<div
		class="flex-1 space-y-1.5 overflow-y-auto px-1 pb-4 min-h-[60px]"
		use:dndzone={{ items: issues, type: 'issue', dropTargetStyle: {} }}
		onconsider={handleConsider}
		onfinalize={handleFinalize}
	>
		{#each issues as issue (issue.id)}
			<IssueContextMenu {issue} {slug} {members} {labels}>
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
			</IssueContextMenu>
		{/each}
	</div>
</div>
