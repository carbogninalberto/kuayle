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
	import { ChevronUp, ChevronDown, User, Plus, CalendarDays } from 'lucide-svelte';
	import { listCycles } from '$lib/api/cycles';
	import type { Cycle } from '$lib/types/cycle';
	import IssueRelations from './IssueRelations.svelte';
	import SubIssuesList from './SubIssuesList.svelte';
	import { goto } from '$app/navigation';

	let {
		issue,
		slug,
		onnavigate,
		onupdated
	}: {
		issue: Issue;
		slug: string;
		onnavigate?: (direction: 'prev' | 'next') => void;
		onupdated?: (issue: Issue) => void;
	} = $props();

	let comments = $state<Comment[]>([]);
	let history = $state<IssueHistory[]>([]);
	let members = $state<WorkspaceMember[]>([]);
	let labels = $state<Label[]>([]);
	let newComment = $state('');
	let editingTitle = $state(false);
	let titleValue = $state('');
	let statusOpen = $state(false);
	let priorityOpen = $state(false);
	let assigneeOpen = $state(false);
	let labelsOpen = $state(false);
	let cycles = $state<Cycle[]>([]);
	let cycleOpen = $state(false);
	let estimateOpen = $state(false);
	let loaded = $state(false);

	const priorityValues: IssuePriority[] = [0, 1, 2, 3, 4];

	onMount(async () => {
		const [c, h, m, l] = await Promise.all([
			listComments(slug, issue.identifier),
			getIssueHistory(slug, issue.identifier),
			listMembers(slug),
			listLabels(slug)
		]);
		comments = c ?? [];
		history = h ?? [];
		members = m ?? [];
		labels = l ?? [];
		loaded = true;
		listCycles(slug, issue.team_id).then(c => cycles = c).catch(() => {});
	});

	$effect(() => {
		titleValue = issue.title;
	});

	async function saveTitle() {
		editingTitle = false;
		if (titleValue.trim() && titleValue !== issue.title) {
			try {
				const updated = await issuesState.update(slug, issue.identifier, { title: titleValue.trim() });
				onupdated?.(updated);
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

	async function updateField(field: string, value: any) {
		try {
			await issuesState.update(slug, issue.identifier, { [field]: value });
			const fresh = await getIssue(slug, issue.identifier);
			onupdated?.(fresh);
		} catch {
			toast.error(`Failed to update ${field}`);
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
		} catch (err: any) {
			toast.error(err?.error?.message || 'Failed to add comment');
		}
	}

	async function refreshLabels() {
		try {
			const fresh = await getIssue(slug, issue.identifier);
			const idx = issuesState.issues.findIndex(i => i.identifier === issue.identifier);
			if (idx >= 0) issuesState.issues[idx] = fresh;
			if (issuesState.selectedIssue?.identifier === issue.identifier) {
				issuesState.selectedIssue = fresh;
			}
			onupdated?.(fresh);
		} catch { /* ignore */ }
	}
</script>

<div class="flex h-full flex-col animate-in fade-in duration-150">
	<!-- Top bar -->
	<div class="flex h-11 items-center justify-between border-b border-[var(--app-border)] px-4">
		<div class="flex items-center gap-1.5 text-xs">
			<a
				href="/{slug}/teams/{issue.team_id}"
				class="text-[var(--color-text-tertiary)] hover:text-[var(--color-text-primary)] transition-colors"
			>
				{issue.identifier.split('-')[0]}
			</a>
			<span class="text-[var(--color-text-tertiary)]">&rsaquo;</span>
			<span class="font-medium text-[var(--color-text-primary)]">{issue.identifier}</span>
		</div>
		{#if onnavigate}
			<div class="flex items-center gap-0.5">
				<button
					onclick={() => onnavigate?.('prev')}
					class="rounded p-1 text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)] transition-colors"
					title="Previous issue (K)"
				>
					<ChevronUp size={16} />
				</button>
				<button
					onclick={() => onnavigate?.('next')}
					class="rounded p-1 text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)] transition-colors"
					title="Next issue (J)"
				>
					<ChevronDown size={16} />
				</button>
			</div>
		{/if}
	</div>

	<!-- Main content -->
	<div class="flex flex-1 overflow-hidden">
		<!-- Left column — main content -->
		<div class="flex-1 overflow-y-auto px-10 py-6">
			<!-- Title -->
			<!-- svelte-ignore a11y_autofocus -->
			{#if editingTitle}
				<input
					type="text"
					bind:value={titleValue}
					onblur={saveTitle}
					onkeydown={(e) => { if (e.key === 'Enter') saveTitle(); if (e.key === 'Escape') { titleValue = issue.title; editingTitle = false; } }}
					autofocus
					class="w-full bg-transparent text-lg font-semibold text-[var(--color-text-primary)] outline-none"
				/>
			{:else}
				<button
					onclick={() => (editingTitle = true)}
					class="w-full text-left text-lg font-semibold text-[var(--color-text-primary)] hover:text-[var(--color-text-primary)] transition-colors"
				>
					{issue.title}
				</button>
			{/if}

			<!-- Description -->
			<div class="mt-3">
				<RichEditor
					content={issue.description ?? ''}
					placeholder="Add description..."
					minimal={false}
					onupdate={saveDescription}
				/>
			</div>

			<!-- Sub-issues (inline like Linear) -->
			{#if (issue.sub_issue_count ?? 0) > 0}
				<div class="mt-5">
					<SubIssuesList
						{slug}
						identifier={issue.identifier}
						subIssueCount={issue.sub_issue_count ?? 0}
						subIssueDone={issue.sub_issue_done ?? 0}
						onclickissue={(sub) => goto(`/${slug}/issue/${sub.identifier}`)}
					/>
				</div>
			{/if}

			<!-- Relations -->
			<div class="mt-5">
				<IssueRelations {slug} identifier={issue.identifier} />
			</div>

			<!-- Activity timeline -->
			<div class="mt-8">
				<h3 class="text-sm font-medium text-[var(--color-text-primary)] mb-4">Activity</h3>

				{#if loaded}
					{@const allActivity = [
						...history.map(h => ({ type: 'history' as const, data: h, time: h.created_at })),
						...comments.map(c => ({ type: 'comment' as const, data: c, time: c.created_at }))
					].sort((a, b) => new Date(a.time).getTime() - new Date(b.time).getTime())}

					<div class="relative">
						<!-- Timeline connector line -->
						{#if allActivity.length > 0}
							<div class="absolute left-[11px] top-3 bottom-0 w-px bg-[var(--app-border)]"></div>
						{/if}

						<div class="space-y-0">
							{#each allActivity as item, idx}
								{#if item.type === 'comment'}
									<!-- Comment entry -->
									<div class="relative flex gap-3 pb-4">
										<div class="relative z-10 flex h-6 w-6 shrink-0 items-center justify-center rounded-full bg-[var(--app-accent)] text-[9px] font-medium text-white ring-2 ring-[var(--color-bg)]">
											{(item.data.user?.name ?? 'U').charAt(0).toUpperCase()}
										</div>
										<div class="flex-1 min-w-0 pt-0.5">
											<div class="flex items-center gap-2 mb-1.5">
												<span class="text-xs font-medium text-[var(--color-text-primary)]">{item.data.user?.name ?? 'User'}</span>
												<span class="text-[11px] text-[var(--color-text-tertiary)]">{formatRelativeTime(item.data.created_at)}</span>
											</div>
											<div class="prose prose-invert prose-sm max-w-none text-[13px] text-[var(--color-text-secondary)] rounded-md border border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-3 py-2">
												{@html item.data.body}
											</div>
										</div>
									</div>
								{:else}
									<!-- History entry -->
									<div class="relative flex items-center gap-3 pb-3">
										<div class="relative z-10 flex h-6 w-6 shrink-0 items-center justify-center">
											<div class="h-2 w-2 rounded-full bg-[var(--color-text-tertiary)] ring-2 ring-[var(--color-bg)]"></div>
										</div>
										<div class="flex flex-wrap items-center gap-1.5 text-xs text-[var(--color-text-tertiary)] min-w-0">
											{#if item.data.field === 'title' || item.data.field === 'description'}
												<span>updated <strong class="text-[var(--color-text-secondary)]">{item.data.field}</strong></span>
											{:else}
												<span>changed <strong class="text-[var(--color-text-secondary)]">{item.data.field}</strong></span>
												{#if item.data.old_value}
													<span>from</span>
													<code class="rounded bg-[var(--color-bg-tertiary)] px-1.5 py-0.5 text-[11px] text-[var(--color-text-secondary)]">{formatHistoryValue(item.data.field, item.data.old_value)}</code>
												{/if}
												<span>to</span>
												<code class="rounded bg-[var(--color-bg-tertiary)] px-1.5 py-0.5 text-[11px] text-[var(--color-text-secondary)]">{formatHistoryValue(item.data.field, item.data.new_value)}</code>
											{/if}
											<span class="text-[var(--color-text-tertiary)]">&middot;</span>
											<span>{formatRelativeTime(item.data.created_at)}</span>
										</div>
									</div>
								{/if}
							{/each}
						</div>

						{#if allActivity.length === 0}
							<p class="text-xs text-[var(--color-text-tertiary)]">No activity yet</p>
						{/if}
					</div>
				{/if}

				<!-- Comment input -->
				<form onsubmit={handleAddComment} class="relative mt-2">
					<div class="flex gap-3">
						<div class="relative z-10 flex h-6 w-6 shrink-0 items-center justify-center rounded-full bg-[var(--app-accent)] text-[9px] font-medium text-white">
							U
						</div>
						<div class="flex-1 rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)] transition-colors focus-within:border-[var(--app-accent)]">
							<textarea
								bind:value={newComment}
								placeholder="Leave a comment..."
								rows="1"
								onkeydown={(e) => { if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); handleAddComment(e); } }}
								oninput={(e) => { const t = e.target as HTMLTextAreaElement; t.style.height = 'auto'; t.style.height = Math.min(t.scrollHeight, 120) + 'px'; }}
								class="w-full resize-none bg-transparent px-3 py-2 text-[13px] text-[var(--color-text-primary)] outline-none placeholder:text-[var(--color-text-tertiary)]"
							></textarea>
							{#if newComment.trim()}
								<div class="flex items-center justify-end gap-1 border-t border-[var(--app-border)] px-2 py-1">
									<button
										type="submit"
										class="rounded px-2 py-0.5 text-xs font-medium text-[var(--app-accent)] hover:bg-[var(--color-bg-hover)] transition-colors"
									>
										Comment
									</button>
								</div>
							{/if}
						</div>
					</div>
				</form>
			</div>
		</div>

		<!-- Right column — properties (Linear-style) -->
		<div class="w-64 shrink-0 overflow-y-auto border-l border-[var(--app-border)] px-4 py-5">
			<h3 class="mb-3 text-[11px] font-medium uppercase tracking-wider text-[var(--color-text-tertiary)]">Properties</h3>

			<div class="space-y-2.5">
				<!-- Status -->
				<div class="flex items-center justify-between py-0.5">
					<span class="text-xs text-[var(--color-text-tertiary)]">Status</span>
					<Popover.Root bind:open={statusOpen}>
						<Popover.Trigger>
							<button class="flex items-center gap-1.5 rounded px-1.5 py-0.5 text-xs text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)] transition-colors">
								<IssueStatusIcon status={issue.status} size={12} />
								{STATUS_LABELS[issue.status]}
							</button>
						</Popover.Trigger>
						<Popover.Content class="w-40 p-1" align="end">
							{#each STATUS_ORDER as value}
								<button
									onclick={() => { updateField('status', value); statusOpen = false; }}
									class="flex w-full items-center gap-2 rounded px-2 py-1 text-xs text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)] transition-colors {issue.status === value ? 'bg-[var(--color-bg-hover)]' : ''}"
								>
									<IssueStatusIcon status={value} size={13} />
									{STATUS_LABELS[value]}
								</button>
							{/each}
						</Popover.Content>
					</Popover.Root>
				</div>

				<!-- Priority -->
				<div class="flex items-center justify-between py-0.5">
					<span class="text-xs text-[var(--color-text-tertiary)]">Priority</span>
					<Popover.Root bind:open={priorityOpen}>
						<Popover.Trigger>
							<button class="flex items-center gap-1.5 rounded px-1.5 py-0.5 text-xs text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)] transition-colors">
								<IssuePriorityIcon priority={issue.priority} size={12} />
								{PRIORITY_LABELS[issue.priority]}
							</button>
						</Popover.Trigger>
						<Popover.Content class="w-40 p-1" align="end">
							{#each priorityValues as value}
								<button
									onclick={() => { updateField('priority', value); priorityOpen = false; }}
									class="flex w-full items-center gap-2 rounded px-2 py-1 text-xs text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)] transition-colors {issue.priority === value ? 'bg-[var(--color-bg-hover)]' : ''}"
								>
									<IssuePriorityIcon priority={value} size={13} />
									{PRIORITY_LABELS[value]}
								</button>
							{/each}
						</Popover.Content>
					</Popover.Root>
				</div>

				<!-- Assignees -->
				<div class="py-0.5">
					<div class="flex items-center justify-between">
						<span class="text-xs text-[var(--color-text-tertiary)]">Assignees</span>
						<Popover.Root bind:open={assigneeOpen}>
							<Popover.Trigger>
								<button class="rounded px-1.5 py-0.5 text-xs text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)] transition-colors">
									<Plus size={12} />
								</button>
							</Popover.Trigger>
							<Popover.Content class="w-48 p-1" align="end">
								{#each members as member}
									{@const isAssigned = (issue.assignees ?? []).some(a => a.id === member.user_id)}
									<button
										onclick={async () => {
											const currentIds = (issue.assignees ?? []).map(a => a.id);
											const newIds = isAssigned
												? currentIds.filter(id => id !== member.user_id)
												: [...currentIds, member.user_id];
											try {
												await issuesState.update(slug, issue.identifier, { assignee_ids: newIds });
												await refreshLabels();
											} catch { toast.error('Failed to update assignees'); }
										}}
										class="flex w-full items-center gap-2 rounded px-2 py-1 text-xs text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)]"
									>
										<Checkbox checked={isAssigned} />
										<div class="flex h-4 w-4 items-center justify-center rounded-full bg-[var(--app-accent)] text-[8px] text-white">
											{(member.name || member.email).charAt(0).toUpperCase()}
										</div>
										{member.name || member.email}
									</button>
								{/each}
								{#if members.length === 0}
									<p class="px-2 py-3 text-center text-xs text-[var(--color-text-tertiary)]">No members</p>
								{/if}
							</Popover.Content>
						</Popover.Root>
					</div>
					{#if issue.assignees && issue.assignees.length > 0}
						<div class="flex flex-wrap gap-1 mt-1">
							{#each issue.assignees as a}
								<span class="flex items-center gap-1.5 rounded-full border border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-2 py-0.5 text-[11px] text-[var(--color-text-secondary)]">
									<div class="flex h-3.5 w-3.5 items-center justify-center rounded-full bg-[var(--app-accent)] text-[7px] text-white shrink-0">
										{(a.name ?? 'U').charAt(0).toUpperCase()}
									</div>
									{a.name}
								</span>
							{/each}
						</div>
					{:else if issue.assignee}
						<div class="flex items-center gap-1.5 mt-1 text-xs text-[var(--color-text-secondary)]">
							<div class="flex h-4 w-4 items-center justify-center rounded-full bg-[var(--app-accent)] text-[8px] text-white">
								{(issue.assignee.name ?? 'U').charAt(0).toUpperCase()}
							</div>
							{issue.assignee.name}
						</div>
					{:else}
						<p class="mt-1 text-[11px] text-[var(--color-text-tertiary)]">No assignees</p>
					{/if}
				</div>

				<!-- Labels -->
				<div class="py-0.5">
					<div class="flex items-center justify-between">
						<span class="text-xs text-[var(--color-text-tertiary)]">Labels</span>
						<Popover.Root bind:open={labelsOpen}>
							<Popover.Trigger>
								<button class="rounded px-1.5 py-0.5 text-xs text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)] transition-colors">
									<Plus size={12} />
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
											await refreshLabels();
										} catch { toast.error('Failed to update labels'); }
									}}
									class="flex w-full items-center gap-2 rounded px-2 py-1 text-xs text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)]"
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
					{#if issue.labels && issue.labels.length > 0}
						<div class="flex flex-wrap gap-1 mt-1">
							{#each issue.labels as lbl}
								<span class="flex items-center gap-1.5 rounded-full border border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-2 py-0.5 text-[11px] text-[var(--color-text-secondary)]">
									<span class="h-2 w-2 rounded-full shrink-0" style="background-color: {lbl.color}"></span>
									{lbl.name}
								</span>
							{/each}
						</div>
					{/if}
				</div>

				<!-- Due date (shown as tag like Linear) -->
				<div class="flex items-center justify-between py-0.5">
					<span class="text-xs text-[var(--color-text-tertiary)]">Due date</span>
					<div class="flex items-center gap-1">
						{#if issue.due_date}
							{@const due = new Date(issue.due_date)}
							{@const now = new Date()}
							{@const diffDays = Math.ceil((due.getTime() - now.getTime()) / 86400000)}
							{@const dateLabel = diffDays === 0 ? 'Today' : diffDays === 1 ? 'Tomorrow' : diffDays === -1 ? 'Yesterday' : due.toLocaleDateString('en-US', { month: 'short', day: 'numeric' })}
							{@const color = diffDays < 0 ? 'text-red-500 bg-red-500/10' : diffDays === 0 ? 'text-orange-500 bg-orange-500/10' : 'text-[var(--color-text-secondary)] bg-[var(--color-bg-secondary)]'}
							<span class="flex items-center gap-1 rounded px-1.5 py-0.5 text-[11px] font-medium {color}">
								<CalendarDays size={11} />
								{dateLabel}
							</span>
						{/if}
						<DatePickerPopover
							value={issue.due_date}
							onchange={(d) => updateField('due_date', d ?? undefined)}
							placeholder={issue.due_date ? '' : 'None'}
						/>
					</div>
				</div>

				<!-- Cycle -->
				<div class="flex items-center justify-between py-0.5">
					<span class="text-xs text-[var(--color-text-tertiary)]">Cycle</span>
					<Popover.Root bind:open={cycleOpen}>
						<Popover.Trigger>
							<button class="flex items-center gap-1.5 rounded px-1.5 py-0.5 text-xs text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)] transition-colors">
								{issue.cycle_id ? (cycles.find(c => c.id === issue.cycle_id)?.name ?? 'Cycle') : 'None'}
							</button>
						</Popover.Trigger>
						<Popover.Content class="w-48 p-1" align="end">
							<button
								onclick={() => { updateField('cycle_id', undefined); cycleOpen = false; }}
								class="flex w-full items-center gap-2 rounded px-2 py-1 text-xs text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)]"
							>
								No cycle
							</button>
							{#each cycles as cycle}
								<button
									onclick={() => { updateField('cycle_id', cycle.id); cycleOpen = false; }}
									class="flex w-full items-center gap-2 rounded px-2 py-1 text-xs text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)] {issue.cycle_id === cycle.id ? 'bg-[var(--color-bg-hover)]' : ''}"
								>
									{cycle.name}
								</button>
							{/each}
							{#if cycles.length === 0}
								<p class="px-2 py-2 text-center text-[11px] text-[var(--color-text-tertiary)]">No cycles</p>
							{/if}
						</Popover.Content>
					</Popover.Root>
				</div>

				<!-- Estimate -->
				<div class="flex items-center justify-between py-0.5">
					<span class="text-xs text-[var(--color-text-tertiary)]">Estimate</span>
					<Popover.Root bind:open={estimateOpen}>
						<Popover.Trigger>
							<button class="rounded px-1.5 py-0.5 text-xs text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)] transition-colors">
								{issue.estimate !== null && issue.estimate !== undefined ? issue.estimate : 'None'}
							</button>
						</Popover.Trigger>
						<Popover.Content class="w-28 p-1" align="end">
							<button
								onclick={() => { updateField('estimate', undefined); estimateOpen = false; }}
								class="flex w-full items-center rounded px-2 py-1 text-xs text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)]"
							>
								Clear
							</button>
							{#each [0, 1, 2, 3, 5, 8, 13, 21] as est}
								<button
									onclick={() => { updateField('estimate', est); estimateOpen = false; }}
									class="flex w-full items-center rounded px-2 py-1 text-xs text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)] {issue.estimate === est ? 'bg-[var(--color-bg-hover)]' : ''}"
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
