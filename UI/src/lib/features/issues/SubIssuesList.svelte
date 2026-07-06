<script lang="ts">
	import type { Issue, IssuePriority, UpdateIssueRequest } from '$lib/types/issue';
	import type { WorkspaceMember } from '$lib/types/workspace';
	import { listSubIssues } from '$lib/api/issue-relations';
	import { issuesState } from './issues.state.svelte';
	import { teamStatusesState } from './team-statuses.state.svelte';
	import { StatusSelector, PrioritySelector, AssigneeSelector } from './selectors';
	import IssueStatusIcon from './IssueStatusIcon.svelte';
	import IssuePriorityIcon from './IssuePriorityIcon.svelte';
	import * as Collapsible from '$lib/components/ui/collapsible';
	import { toast } from 'svelte-sonner';
	import { ChevronRight, Plus, UserCircle } from 'lucide-svelte';

	let {
		slug,
		identifier,
		subIssueCount = 0,
		subIssueDone = 0,
		members = [],
		onaddsubissue,
		onclickissue,
		onupdated
	}: {
		slug: string;
		identifier: string;
		subIssueCount?: number;
		subIssueDone?: number;
		members?: WorkspaceMember[];
		onaddsubissue?: () => void;
		onclickissue?: (issue: Issue) => void;
		onupdated?: () => void | Promise<void>;
	} = $props();

	let subIssues = $state<Issue[]>([]);
	let isOpen = $state(false);
	let loaded = $state(false);
	let loading = $state(false);
	let lastCount = $state(0);

	let progressPercent = $derived(
		subIssueCount > 0 ? Math.round((subIssueDone / subIssueCount) * 100) : 0
	);
	let progressOffset = $derived(31.416 - (31.416 * progressPercent) / 100);

	async function loadSubIssues() {
		if (loaded || loading) return;
		loading = true;
		try {
			subIssues = await listSubIssues(slug, identifier);
			loaded = true;
		} finally {
			loading = false;
		}
	}

	$effect(() => {
		if (isOpen && !loaded) {
			loadSubIssues();
		}
	});

	$effect(() => {
		if (subIssueCount !== lastCount) {
			lastCount = subIssueCount;
			loaded = false;
			subIssues = [];
			if (isOpen) loadSubIssues();
		}
	});

	async function updateSubIssue(subIssue: Issue, updates: UpdateIssueRequest) {
		try {
			const updated = await issuesState.update(slug, subIssue.identifier, updates);
			subIssues = subIssues.map((item) => item.id === updated.id ? updated : item);
			await onupdated?.();
		} catch (err: any) {
			toast.error(err?.error?.message || 'Failed to update sub-issue');
		}
	}

	function assigneeIds(subIssue: Issue): string[] {
		const ids = (subIssue.assignees ?? []).map((assignee) => assignee.id);
		if (ids.length === 0 && subIssue.assignee) return [subIssue.assignee.id];
		return ids;
	}

	function displayAssignees(subIssue: Issue) {
		if (subIssue.assignees && subIssue.assignees.length > 0) return subIssue.assignees;
		return subIssue.assignee ? [subIssue.assignee] : [];
	}

	function assigneeName(assignee: NonNullable<Issue['assignee']>) {
		return assignee.name ?? assignee.email ?? 'Unassigned';
	}

	function assigneeTitle(subIssue: Issue) {
		const assignees = displayAssignees(subIssue);
		return assignees.length > 0 ? assignees.map(assigneeName).join(', ') : 'Add assignee';
	}

	async function toggleAssignee(subIssue: Issue, userId: string) {
		const currentIds = assigneeIds(subIssue);
		const newIds = currentIds.includes(userId)
			? currentIds.filter((id) => id !== userId)
			: [...currentIds, userId];
		await updateSubIssue(subIssue, { assignee_ids: newIds });
	}
</script>

{#if subIssueCount > 0 || onaddsubissue}
	<Collapsible.Root bind:open={isOpen}>
		<div class="overflow-hidden rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)]/60">
			<div class="flex items-center gap-2 px-3 py-2">
				<Collapsible.Trigger
					class="flex min-w-0 flex-1 items-center gap-2 text-sm text-[var(--color-text-secondary)] hover:text-[var(--color-text-primary)]"
				>
					<ChevronRight
						size={14}
						class="transition-transform {isOpen ? 'rotate-90' : ''}"
					/>
					<span class="font-medium">Sub-issues</span>
					{#if subIssueCount > 0}
						<span class="inline-flex items-center gap-1.5 rounded-full bg-[var(--color-bg-tertiary)] px-2 py-0.5 text-xs text-[var(--color-text-tertiary)]">
							<svg class="h-3.5 w-3.5 -rotate-90" viewBox="0 0 12 12" aria-hidden="true">
								<circle cx="6" cy="6" r="5" fill="none" stroke="currentColor" stroke-width="2" class="text-[var(--color-text-tertiary)] opacity-70" />
								<circle
									cx="6"
									cy="6"
									r="5"
									fill="none"
									stroke="currentColor"
									stroke-width="2"
									stroke-linecap="round"
									stroke-dasharray="31.416"
									stroke-dashoffset={progressOffset}
									class="text-[var(--color-success)]"
								/>
							</svg>
							{subIssueDone}/{subIssueCount}
						</span>
					{/if}
				</Collapsible.Trigger>

				{#if onaddsubissue}
					<button
						onclick={onaddsubissue}
						class="ml-auto flex h-7 w-7 shrink-0 items-center justify-center rounded-full text-[var(--color-text-tertiary)] transition-colors hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]"
						title="Add sub-issue"
					>
						<Plus size={14} />
					</button>
				{/if}
			</div>

			<Collapsible.Content>
				<div class="border-t border-[var(--app-border)] py-1">
					{#if loading}
						<p class="px-3 py-3 text-xs text-[var(--color-text-tertiary)]">Loading sub-issues...</p>
					{/if}
					{#each subIssues as subIssue}
						{@const assignees = displayAssignees(subIssue)}
						{@const firstAssignee = assignees[0]}
						<div class="group flex w-full items-center gap-2 px-3 py-1.5 transition-colors hover:bg-[var(--color-bg-hover)]">
							<StatusSelector
								statuses={teamStatusesState.statusOrder}
								value={subIssue.status_id}
								width="w-44"
								onchange={(statusId) => updateSubIssue(subIssue, { status_id: statusId })}
							>
								{#snippet trigger()}
									<button
										type="button"
										class="flex h-6 w-6 shrink-0 items-center justify-center rounded-md text-[var(--color-text-tertiary)] transition-colors hover:bg-[var(--color-bg-tertiary)] hover:text-[var(--color-text-primary)]"
										title={subIssue.status_info?.name ?? subIssue.status}
									>
										<IssueStatusIcon status={subIssue.status} category={subIssue.status_info?.category} color={subIssue.status_info?.color} size={14} />
									</button>
								{/snippet}
							</StatusSelector>

							<button
								type="button"
								class="flex min-w-0 flex-1 items-center gap-2 rounded-md px-1.5 py-1 text-left transition-colors hover:bg-[var(--color-bg-tertiary)]"
								onclick={() => onclickissue?.(subIssue)}
							>
								<span class="shrink-0 text-xs tabular-nums text-[var(--color-text-tertiary)]">{subIssue.identifier}</span>
								<span class="min-w-0 flex-1 truncate text-sm text-[var(--color-text-primary)]">{subIssue.title}</span>
							</button>

							<PrioritySelector
								value={subIssue.priority}
								width="w-40"
								align="end"
								onchange={(priority: IssuePriority) => updateSubIssue(subIssue, { priority })}
							>
								{#snippet trigger()}
									<button
										type="button"
										class="flex h-6 w-6 shrink-0 items-center justify-center rounded-md text-[var(--color-text-tertiary)] transition-colors hover:bg-[var(--color-bg-tertiary)] hover:text-[var(--color-text-primary)]"
										title="Change priority"
									>
										<IssuePriorityIcon priority={subIssue.priority} size={14} />
									</button>
								{/snippet}
							</PrioritySelector>

							<AssigneeSelector
								{members}
								value={assigneeIds(subIssue)}
								width="w-52"
								align="end"
								onchange={(userId) => toggleAssignee(subIssue, userId)}
							>
								{#snippet trigger()}
									<button
										type="button"
										class="flex min-w-6 shrink-0 items-center justify-center rounded-full text-[var(--color-text-tertiary)] transition-colors hover:text-[var(--color-text-primary)]"
										title={assigneeTitle(subIssue)}
									>
										{#if assignees.length > 1 && firstAssignee}
											<span class="flex -space-x-2 rounded-full transition-all hover:ring-2 hover:ring-[var(--app-accent)]">
												<span class="flex h-5 w-5 items-center justify-center rounded-full bg-[var(--app-accent)] text-[9px] font-medium text-[var(--app-accent-foreground)] ring-1 ring-[var(--color-bg)]">
													{assigneeName(firstAssignee).charAt(0).toUpperCase()}
												</span>
												<span class="flex h-5 w-5 items-center justify-center rounded-full bg-[var(--color-bg-tertiary)] text-[8px] font-medium text-[var(--color-text-secondary)] ring-1 ring-[var(--color-bg)]">
													+{assignees.length - 1}
												</span>
											</span>
										{:else if firstAssignee}
											<span class="flex h-5 w-5 items-center justify-center rounded-full bg-[var(--app-accent)] text-[9px] font-medium text-[var(--app-accent-foreground)] transition-all hover:ring-2 hover:ring-[var(--app-accent)]">
												{assigneeName(firstAssignee).charAt(0).toUpperCase()}
											</span>
										{:else}
											<UserCircle size={15} />
										{/if}
									</button>
								{/snippet}
							</AssigneeSelector>
						</div>
					{/each}
					{#if subIssues.length === 0 && loaded}
						<p class="px-3 py-3 text-xs text-[var(--color-text-tertiary)]">No sub-issues yet</p>
					{/if}
				</div>
			</Collapsible.Content>
		</div>
	</Collapsible.Root>
{/if}
