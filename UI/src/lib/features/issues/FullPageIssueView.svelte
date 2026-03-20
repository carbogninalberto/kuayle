<script lang="ts">
	import { onMount } from 'svelte';
	import type { Issue, Comment, IssueHistory, IssueStatus, IssuePriority } from '$lib/types/issue';
	import { STATUS_LABELS, PRIORITY_LABELS, STATUS_ORDER } from '$lib/types/issue';
	import type { WorkspaceMember } from '$lib/types/workspace';
	import type { Label } from '$lib/types/label';
	import { listComments, createComment, getIssueHistory, getIssue } from '$lib/api/issues';
	import { listMembers } from '$lib/api/members';
	import { listLabels } from '$lib/api/labels';
	import { issuesState } from './issues.state.svelte';
	import IssueStatusIcon from './IssueStatusIcon.svelte';
	import IssuePriorityIcon from './IssuePriorityIcon.svelte';
	import DatePickerPopover from '$lib/components/shared/DatePickerPopover.svelte';
	import RichEditor from '$lib/components/shared/RichEditor.svelte';
	import { formatRelativeTime } from '$lib/utils/format';
	import { toast } from 'svelte-sonner';
	import * as Popover from '$lib/components/ui/popover';
	import { Checkbox } from '$lib/components/ui/checkbox';
	import { ChevronUp, ChevronDown, User } from 'lucide-svelte';
	import { listCycles } from '$lib/api/cycles';
	import type { Cycle } from '$lib/types/cycle';
	import IssueRelations from './IssueRelations.svelte';
	import SubIssuesList from './SubIssuesList.svelte';
	import { goto } from '$app/navigation';

	let {
		issue,
		slug,
		onnavigate
	}: {
		issue: Issue;
		slug: string;
		onnavigate?: (direction: 'prev' | 'next') => void;
	} = $props();

	let comments = $state<Comment[]>([]);
	let history = $state<IssueHistory[]>([]);
	let members = $state<WorkspaceMember[]>([]);
	let labels = $state<Label[]>([]);
	let newComment = $state('');
	let tab = $state<'comments' | 'activity'>('comments');
	let editingTitle = $state(false);
	let titleValue = $state('');
	let statusOpen = $state(false);
	let priorityOpen = $state(false);
	let assigneeOpen = $state(false);
	let labelsOpen = $state(false);
	let cycles = $state<Cycle[]>([]);
	let cycleOpen = $state(false);
	let estimateOpen = $state(false);

	const priorityValues: IssuePriority[] = [0, 1, 2, 3, 4];

	onMount(async () => {
		const [c, h, m, l] = await Promise.all([
			listComments(slug, issue.identifier),
			getIssueHistory(slug, issue.identifier),
			listMembers(slug),
			listLabels(slug)
		]);
		comments = c;
		history = h;
		members = m;
		labels = l;

		// Fetch cycles for the issue's team
		listCycles(slug, issue.team_id).then(c => cycles = c).catch(() => {});
	});

	$effect(() => {
		titleValue = issue.title;
	});

	async function saveTitle() {
		editingTitle = false;
		if (titleValue.trim() && titleValue !== issue.title) {
			try {
				await issuesState.update(slug, issue.identifier, { title: titleValue.trim() });
			} catch {
				titleValue = issue.title;
				toast.error('Failed to update title');
			}
		} else {
			titleValue = issue.title;
		}
	}

	async function saveDescription(html: string) {
		try {
			await issuesState.update(slug, issue.identifier, { description: html });
		} catch {
			toast.error('Failed to update description');
		}
	}

	async function updateStatus(status: string) {
		try {
			await issuesState.update(slug, issue.identifier, { status: status as any });
			toast.success('Status updated');
		} catch {
			toast.error('Failed to update status');
		}
	}

	async function updatePriority(priority: number) {
		try {
			await issuesState.update(slug, issue.identifier, { priority: priority as any });
			toast.success('Priority updated');
		} catch {
			toast.error('Failed to update priority');
		}
	}

	async function updateAssignee(assigneeId: string | null) {
		try {
			await issuesState.update(slug, issue.identifier, { assignee_id: assigneeId ?? undefined });
			toast.success('Assignee updated');
		} catch {
			toast.error('Failed to update assignee');
		}
	}

	async function updateDueDate(date: string | null) {
		try {
			await issuesState.update(slug, issue.identifier, { due_date: date ?? undefined });
			toast.success('Due date updated');
		} catch {
			toast.error('Failed to update due date');
		}
	}

	function formatHistoryValue(field: string, value: string | null): string {
		if (!value) return '';
		switch (field) {
			case 'status':
				return STATUS_LABELS[value as IssueStatus] ?? value;
			case 'priority':
				return PRIORITY_LABELS[Number(value) as IssuePriority] ?? value;
			case 'assignee_id': {
				const member = members.find(m => m.user_id === value);
				return member ? (member.name || member.email) : value;
			}
			default:
				return value;
		}
	}

	async function handleAddComment(e: Event) {
		e.preventDefault();
		if (!newComment.trim()) return;
		try {
			const comment = await createComment(slug, issue.identifier, newComment);
			comments = [...comments, comment];
			newComment = '';
			toast.success('Comment added');
		} catch (err: any) {
			toast.error(err?.error?.message || 'Failed to add comment');
		}
	}
</script>

<div class="flex h-full flex-col">
	<!-- Top bar -->
	<div class="flex h-[49px] items-center justify-between border-b border-[var(--app-border)] px-6">
		<div class="flex items-center gap-1.5 text-sm">
			<a
				href="/{slug}/teams/{issue.team_id}"
				class="text-[var(--color-text-tertiary)] hover:text-[var(--color-text-secondary)]"
			>
				{issue.identifier.split('-')[0]}
			</a>
			<span class="text-[var(--color-text-tertiary)]">/</span>
			<span class="text-[var(--color-text-primary)] font-medium">{issue.identifier}</span>
		</div>
		<div class="flex items-center gap-1">
			{#if onnavigate}
				<button
					onclick={() => onnavigate?.('prev')}
					class="rounded p-1 text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]"
					title="Previous issue"
				>
					<ChevronUp size={18} />
				</button>
				<button
					onclick={() => onnavigate?.('next')}
					class="rounded p-1 text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]"
					title="Next issue"
				>
					<ChevronDown size={18} />
				</button>
			{/if}
		</div>
	</div>

	<!-- Main content -->
	<div class="flex flex-1 overflow-hidden">
		<!-- Left column -->
		<div class="flex-1 overflow-y-auto p-6">
			<!-- Title -->
			<!-- svelte-ignore a11y_autofocus -->
			{#if editingTitle}
				<input
					type="text"
					bind:value={titleValue}
					onblur={saveTitle}
					onkeydown={(e) => { if (e.key === 'Enter') saveTitle(); if (e.key === 'Escape') { titleValue = issue.title; editingTitle = false; } }}
					autofocus
					class="w-full bg-transparent text-xl font-semibold text-[var(--color-text-primary)] outline-none"
				/>
			{:else}
				<button
					onclick={() => (editingTitle = true)}
					class="w-full text-left text-xl font-semibold text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)] rounded px-1 -mx-1"
				>
					{issue.title}
				</button>
			{/if}

			<!-- Description -->
			<div class="mt-4">
				<RichEditor
					content={issue.description ?? ''}
					placeholder="Add description..."
					minimal={false}
					onupdate={saveDescription}
				/>
			</div>

			<!-- Relations -->
			<div class="mt-6">
				<IssueRelations {slug} identifier={issue.identifier} />
			</div>

			<!-- Sub-issues -->
			<div class="mt-6">
				<SubIssuesList
					{slug}
					identifier={issue.identifier}
					subIssueCount={issue.sub_issue_count ?? 0}
					subIssueDone={issue.sub_issue_done ?? 0}
					onclickissue={(sub) => goto(`/${slug}/issue/${sub.identifier}`)}
				/>
			</div>

			<!-- Tabs -->
			<div class="mt-8 flex gap-4 border-b border-[var(--app-border)]">
				<button
					onclick={() => (tab = 'comments')}
					class="pb-2 text-sm {tab === 'comments'
						? 'border-b-2 border-[var(--app-accent)] text-[var(--color-text-primary)]'
						: 'text-[var(--color-text-tertiary)]'}"
				>
					Comments ({comments.length})
				</button>
				<button
					onclick={() => (tab = 'activity')}
					class="pb-2 text-sm {tab === 'activity'
						? 'border-b-2 border-[var(--app-accent)] text-[var(--color-text-primary)]'
						: 'text-[var(--color-text-tertiary)]'}"
				>
					Activity ({history.length})
				</button>
			</div>

			{#if tab === 'comments'}
				<div class="mt-4 space-y-4">
					{#each comments as comment}
						<div class="text-sm">
							<div class="flex items-center gap-2">
								<span class="font-medium text-[var(--color-text-primary)]">{comment.user?.name ?? 'User'}</span>
								<span class="text-[var(--color-text-tertiary)]">{formatRelativeTime(comment.created_at)}</span>
							</div>
							<div class="mt-1 prose prose-invert prose-sm max-w-none text-[var(--color-text-secondary)]">
								{@html comment.body}
							</div>
						</div>
					{/each}

					<form onsubmit={handleAddComment} class="flex gap-2">
						<input
							type="text"
							bind:value={newComment}
							placeholder="Write a comment..."
							class="flex-1 rounded border border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-3 py-2 text-sm text-[var(--color-text-primary)] outline-none focus:border-[var(--app-accent)]"
						/>
						<button
							type="submit"
							disabled={!newComment.trim()}
							class="rounded bg-[var(--app-accent)] px-3 py-2 text-sm text-white hover:bg-[var(--app-accent-hover)] disabled:opacity-50"
						>
							Send
						</button>
					</form>
				</div>
			{:else}
				<div class="mt-4 space-y-3">
					{#each history as entry}
						<div class="flex items-start gap-2 text-sm">
							<span class="text-[var(--color-text-tertiary)]">{formatRelativeTime(entry.created_at)}</span>
							<span class="text-[var(--color-text-secondary)]">
								changed <strong>{entry.field}</strong>
								{#if entry.old_value}from <code class="rounded bg-[var(--color-bg-tertiary)] px-1">{formatHistoryValue(entry.field, entry.old_value)}</code>{/if}
								to <code class="rounded bg-[var(--color-bg-tertiary)] px-1">{formatHistoryValue(entry.field, entry.new_value)}</code>
							</span>
						</div>
					{/each}
					{#if history.length === 0}
						<p class="text-sm text-[var(--color-text-tertiary)]">No activity yet</p>
					{/if}
				</div>
			{/if}
		</div>

		<!-- Right column (properties sidebar) -->
		<div class="w-72 shrink-0 overflow-y-auto border-l border-[var(--app-border)] p-4">
			<h3 class="mb-4 text-xs font-medium uppercase text-[var(--color-text-tertiary)]">Properties</h3>

			<div class="space-y-3">
				<!-- Status -->
				<div class="flex items-center justify-between">
					<span class="text-xs text-[var(--color-text-tertiary)]">Status</span>
					<Popover.Root bind:open={statusOpen}>
						<Popover.Trigger>
							<button class="flex items-center gap-1.5 rounded-md px-2 py-1 text-xs text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)]">
								<IssueStatusIcon status={issue.status} size={12} />
								{STATUS_LABELS[issue.status]}
							</button>
						</Popover.Trigger>
						<Popover.Content class="w-40 p-1" align="end">
							{#each STATUS_ORDER as value}
								<button
									onclick={() => { updateStatus(value); statusOpen = false; }}
									class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)] {issue.status === value ? 'bg-[var(--color-bg-hover)]' : ''}"
								>
									<IssueStatusIcon status={value} size={14} />
									{STATUS_LABELS[value]}
								</button>
							{/each}
						</Popover.Content>
					</Popover.Root>
				</div>

				<!-- Priority -->
				<div class="flex items-center justify-between">
					<span class="text-xs text-[var(--color-text-tertiary)]">Priority</span>
					<Popover.Root bind:open={priorityOpen}>
						<Popover.Trigger>
							<button class="flex items-center gap-1.5 rounded-md px-2 py-1 text-xs text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)]">
								<IssuePriorityIcon priority={issue.priority} size={12} />
								{PRIORITY_LABELS[issue.priority]}
							</button>
						</Popover.Trigger>
						<Popover.Content class="w-40 p-1" align="end">
							{#each priorityValues as value}
								<button
									onclick={() => { updatePriority(value); priorityOpen = false; }}
									class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)] {issue.priority === value ? 'bg-[var(--color-bg-hover)]' : ''}"
								>
									<IssuePriorityIcon priority={value} size={14} />
									{PRIORITY_LABELS[value]}
								</button>
							{/each}
						</Popover.Content>
					</Popover.Root>
				</div>

				<!-- Assignee -->
				<div class="flex items-center justify-between">
					<span class="text-xs text-[var(--color-text-tertiary)]">Assignee</span>
					<Popover.Root bind:open={assigneeOpen}>
						<Popover.Trigger>
							<button class="flex items-center gap-1.5 rounded-md px-2 py-1 text-xs text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)]">
								<User size={12} />
								{#if issue.assignee}
									{issue.assignee.name}
								{:else}
									Unassigned
								{/if}
							</button>
						</Popover.Trigger>
						<Popover.Content class="w-48 p-1" align="end">
							<button
								onclick={() => { updateAssignee(null); assigneeOpen = false; }}
								class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)]"
							>
								Unassigned
							</button>
							{#each members as member}
								<button
									onclick={() => { updateAssignee(member.user_id); assigneeOpen = false; }}
									class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)] {issue.assignee_id === member.user_id ? 'bg-[var(--color-bg-hover)]' : ''}"
								>
									<User size={14} class="text-[var(--color-text-tertiary)]" />
									{member.name || member.email}
								</button>
							{/each}
						</Popover.Content>
					</Popover.Root>
				</div>

				<!-- Labels -->
				<div class="flex items-center justify-between">
					<span class="text-xs text-[var(--color-text-tertiary)]">Labels</span>
					<Popover.Root bind:open={labelsOpen}>
						<Popover.Trigger>
							<button class="flex items-center gap-1 rounded-md px-2 py-1 text-xs text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)]">
								{#if issue.labels && issue.labels.length > 0}
									{issue.labels.length} label{issue.labels.length > 1 ? 's' : ''}
								{:else}
									None
								{/if}
							</button>
						</Popover.Trigger>
						<Popover.Content class="w-48 p-1" align="end">
							{#each labels as label}
								<button
									onclick={async () => {
										const currentIds = (issue.labels ?? []).map(l => l.id);
										const newIds = currentIds.includes(label.id)
											? currentIds.filter(id => id !== label.id)
											: [...currentIds, label.id];
										try {
											await issuesState.update(slug, issue.identifier, { label_ids: newIds });
											// Re-fetch to get full label objects
											const fresh = await getIssue(slug, issue.identifier);
											const idx = issuesState.issues.findIndex(i => i.identifier === issue.identifier);
											if (idx >= 0) issuesState.issues[idx] = fresh;
											if (issuesState.selectedIssue?.identifier === issue.identifier) {
												issuesState.selectedIssue = fresh;
											}
										} catch { toast.error('Failed to update labels'); }
									}}
									class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)]"
								>
									<Checkbox checked={(issue.labels ?? []).some(l => l.id === label.id)} />
									<div class="h-2.5 w-2.5 rounded-full shrink-0" style="background-color: {label.color}"></div>
									<span class="truncate">{label.name}</span>
								</button>
							{/each}
							{#if labels.length === 0}
								<p class="px-2 py-3 text-center text-xs text-[var(--color-text-tertiary)]">No labels</p>
							{/if}
						</Popover.Content>
					</Popover.Root>
				</div>

				<!-- Due date -->
				<div class="flex items-center justify-between">
					<span class="text-xs text-[var(--color-text-tertiary)]">Due date</span>
					<DatePickerPopover
						value={issue.due_date}
						onchange={updateDueDate}
						placeholder="Set date"
					/>
				</div>

				<!-- Cycle -->
				<div class="flex items-center justify-between">
					<span class="text-xs text-[var(--color-text-tertiary)]">Cycle</span>
					<Popover.Root bind:open={cycleOpen}>
						<Popover.Trigger>
							<button class="flex items-center gap-1.5 rounded-md px-2 py-1 text-xs text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)]">
								{#if issue.cycle_id}
									{cycles.find(c => c.id === issue.cycle_id)?.name ?? 'Cycle'}
								{:else}
									No cycle
								{/if}
							</button>
						</Popover.Trigger>
						<Popover.Content class="w-48 p-1" align="end">
							<button
								onclick={async () => {
									try {
										await issuesState.update(slug, issue.identifier, { cycle_id: undefined });
										cycleOpen = false;
									} catch { toast.error('Failed to update cycle'); }
								}}
								class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)]"
							>
								No cycle
							</button>
							{#each cycles as cycle}
								<button
									onclick={async () => {
										try {
											await issuesState.update(slug, issue.identifier, { cycle_id: cycle.id });
											cycleOpen = false;
										} catch { toast.error('Failed to update cycle'); }
									}}
									class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)] {issue.cycle_id === cycle.id ? 'bg-[var(--color-bg-hover)]' : ''}"
								>
									{cycle.name}
								</button>
							{/each}
							{#if cycles.length === 0}
								<p class="px-2 py-3 text-center text-xs text-[var(--color-text-tertiary)]">No cycles</p>
							{/if}
						</Popover.Content>
					</Popover.Root>
				</div>

				<!-- Estimate -->
				<div class="flex items-center justify-between">
					<span class="text-xs text-[var(--color-text-tertiary)]">Estimate</span>
					<Popover.Root bind:open={estimateOpen}>
						<Popover.Trigger>
							<button class="flex items-center gap-1.5 rounded-md px-2 py-1 text-xs text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)]">
								{issue.estimate !== null && issue.estimate !== undefined ? issue.estimate : 'No estimate'}
							</button>
						</Popover.Trigger>
						<Popover.Content class="w-36 p-1" align="end">
							<button
								onclick={async () => {
									try {
										await issuesState.update(slug, issue.identifier, { estimate: undefined });
										estimateOpen = false;
										toast.success('Estimate cleared');
									} catch { toast.error('Failed to update estimate'); }
								}}
								class="flex w-full items-center rounded-md px-2 py-1.5 text-sm text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)]"
							>
								Clear
							</button>
							{#each [0, 1, 2, 3, 5, 8, 13, 21] as est}
								<button
									onclick={async () => {
										try {
											await issuesState.update(slug, issue.identifier, { estimate: est });
											estimateOpen = false;
											toast.success('Estimate updated');
										} catch { toast.error('Failed to update estimate'); }
									}}
									class="flex w-full items-center rounded-md px-2 py-1.5 text-sm text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)] {issue.estimate === est ? 'bg-[var(--color-bg-hover)]' : ''}"
								>
									{est}
								</button>
							{/each}
						</Popover.Content>
					</Popover.Root>
				</div>
			</div>
		</div>
	</div>
</div>
