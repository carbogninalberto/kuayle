<script lang="ts">
	import { onMount } from 'svelte';
	import type { Issue } from '$lib/types/issue';
	import { listSubIssues } from '$lib/api/issue-relations';
	import IssueStatusIcon from './IssueStatusIcon.svelte';
	import IssuePriorityIcon from './IssuePriorityIcon.svelte';
	import { Button } from '$lib/components/ui/button';
	import * as Collapsible from '$lib/components/ui/collapsible';
	import { Progress } from '$lib/components/ui/progress';
	import { Badge } from '$lib/components/ui/badge';
	import { ChevronRight, Plus } from 'lucide-svelte';

	let {
		slug,
		identifier,
		subIssueCount = 0,
		subIssueDone = 0,
		onaddsubissue,
		onclickissue
	}: {
		slug: string;
		identifier: string;
		subIssueCount?: number;
		subIssueDone?: number;
		onaddsubissue?: () => void;
		onclickissue?: (issue: Issue) => void;
	} = $props();

	let subIssues = $state<Issue[]>([]);
	let isOpen = $state(false);
	let loaded = $state(false);

	let progressPercent = $derived(
		subIssueCount > 0 ? Math.round((subIssueDone / subIssueCount) * 100) : 0
	);

	async function loadSubIssues() {
		if (loaded) return;
		subIssues = await listSubIssues(slug, identifier);
		loaded = true;
	}

	$effect(() => {
		if (isOpen && !loaded) {
			loadSubIssues();
		}
	});
</script>

{#if subIssueCount > 0 || onaddsubissue}
	<Collapsible.Root bind:open={isOpen}>
		<div class="space-y-2">
			<div class="flex items-center gap-2">
				<Collapsible.Trigger
					class="flex items-center gap-1.5 text-sm text-[var(--color-text-secondary)] hover:text-[var(--color-text-primary)]"
				>
					<ChevronRight
						size={14}
						class="transition-transform {isOpen ? 'rotate-90' : ''}"
					/>
					<span class="font-medium">Sub-issues</span>
					{#if subIssueCount > 0}
						<Badge variant="secondary" class="text-xs">
							{subIssueDone}/{subIssueCount}
						</Badge>
					{/if}
				</Collapsible.Trigger>

				{#if onaddsubissue}
					<Button variant="ghost" size="sm" onclick={onaddsubissue} class="ml-auto h-6 px-1.5">
						<Plus size={14} />
					</Button>
				{/if}
			</div>

			{#if subIssueCount > 0}
				<div class="flex items-center gap-2">
					<Progress value={progressPercent} class="h-1.5 flex-1" />
					<span class="text-xs text-[var(--color-text-tertiary)]">{progressPercent}%</span>
				</div>
			{/if}

			<Collapsible.Content>
				<div class="ml-4 space-y-0.5 border-l border-[var(--app-border)] pl-3">
					{#each subIssues as subIssue}
						<button
							class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left transition-colors hover:bg-[var(--color-bg-hover)]"
							onclick={() => onclickissue?.(subIssue)}
						>
							<IssueStatusIcon status={subIssue.status} size={14} />
							<span class="shrink-0 text-xs text-[var(--color-text-tertiary)]">
								{subIssue.identifier}
							</span>
							<span class="flex-1 truncate text-sm text-[var(--color-text-primary)]">
								{subIssue.title}
							</span>
							<IssuePriorityIcon priority={subIssue.priority} />
							{#if subIssue.assignee}
								<span
									class="flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-[var(--color-bg-tertiary)] text-[10px] text-[var(--color-text-secondary)]"
								>
									{subIssue.assignee.name?.charAt(0).toUpperCase() ?? '?'}
								</span>
							{/if}
						</button>
					{/each}
					{#if subIssues.length === 0 && loaded}
						<p class="py-2 text-xs text-[var(--color-text-tertiary)]">No sub-issues</p>
					{/if}
				</div>
			</Collapsible.Content>
		</div>
	</Collapsible.Root>
{/if}
