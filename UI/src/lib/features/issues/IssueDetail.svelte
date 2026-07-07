<script lang="ts">
	import { onMount } from 'svelte';
	import BellIcon from '@lucide/svelte/icons/bell';
	import type { Issue, Comment, IssueHistory } from '$lib/types/issue';
	import { PRIORITY_LABELS } from '$lib/types/issue';
	import { teamStatusesState } from './team-statuses.state.svelte';
	import { listComments, createComment, getIssueHistory, subscribeToIssue, unsubscribeFromIssue } from '$lib/api/issues';
	import { issuesState } from './issues.state.svelte';
	import IssueStatusIcon from './IssueStatusIcon.svelte';
	import IssuePriorityIcon from './IssuePriorityIcon.svelte';
	import { formatRelativeTime } from '$lib/utils/format';
	import { appToast } from '$lib/features/toast/toast';
	import * as Sheet from '$lib/components/ui/sheet';
	import { StatusSelector, PrioritySelector } from './selectors';
	import { sanitizeHtml } from '$lib/security/sanitize';

	let {
		issue,
		slug,
		onclose
	}: { issue: Issue; slug: string; onclose: () => void } = $props();

	let comments = $state<Comment[]>([]);
	let history = $state<IssueHistory[]>([]);
	let newComment = $state('');
	let tab = $state<'comments' | 'activity'>('comments');
	let statusOpen = $state(false);
	let priorityOpen = $state(false);
	let sheetOpen = $state(true);
	let showAllActivity = $state(false);
	let isSubscribed = $state(false);
	let subscriptionBusy = $state(false);

	const RECENT_ACTIVITY_COUNT = 3;
	let sortedHistory = $derived([...history].sort((a, b) => new Date(a.created_at).getTime() - new Date(b.created_at).getTime()));
	let visibleHistory = $derived(showAllActivity ? sortedHistory : sortedHistory.slice(-RECENT_ACTIVITY_COUNT));
	let hiddenHistoryCount = $derived(sortedHistory.length - visibleHistory.length);

	$effect(() => {
		if (!sheetOpen) onclose();
	});

	$effect(() => {
		isSubscribed = issue.is_subscribed ?? false;
	});

	onMount(async () => {
		const [c, h] = await Promise.all([
			listComments(slug, issue.identifier),
			getIssueHistory(slug, issue.identifier)
		]);
		comments = c;
		history = h;
	});

	async function handleAddComment(e: Event) {
		e.preventDefault();
		if (!newComment.trim()) return;
		try {
			const comment = await createComment(slug, issue.identifier, newComment);
			comments = [...comments, comment];
			newComment = '';
			appToast.success('Comment added');
		} catch (err: any) {
			appToast.apiError(err, 'Failed to add comment');
		}
	}

	async function updateStatus(statusId: string) {
		try {
			await issuesState.update(slug, issue.identifier, { status_id: statusId });
			appToast.success('Status updated');
		} catch (err: any) {
			appToast.apiError(err, 'Failed to update status');
		}
	}

	async function updatePriority(priority: number) {
		try {
			await issuesState.update(slug, issue.identifier, { priority: priority as any });
			appToast.success('Priority updated');
		} catch (err: any) {
			appToast.apiError(err, 'Failed to update priority');
		}
	}

	async function toggleSubscription() {
		if (subscriptionBusy) return;
		subscriptionBusy = true;
		const nextValue = !isSubscribed;
		isSubscribed = nextValue;
		try {
			const res = nextValue
				? await subscribeToIssue(slug, issue.identifier)
				: await unsubscribeFromIssue(slug, issue.identifier);
			isSubscribed = res.is_subscribed;
			issuesState.setSubscription(issue.identifier, res.is_subscribed);
			appToast.success(isSubscribed ? 'Notifications enabled' : 'Notifications disabled');
		} catch (err: any) {
			isSubscribed = !nextValue;
			appToast.apiError(err, 'Failed to update notifications');
		} finally {
			subscriptionBusy = false;
		}
	}

	function formatHistoryValue(field: string, value: string | null, displayValue?: string | null): string {
		if (displayValue?.trim()) return displayValue;
		if (!value) return 'None';
		if (field === 'priority') return PRIORITY_LABELS[Number(value) as keyof typeof PRIORITY_LABELS] ?? value;
		return value;
	}

	function historyFieldLabel(field: string): string {
		switch (field) {
			case 'assignee_id': return 'assignee';
			case 'due_date': return 'due date';
			case 'parent_id': return 'parent';
			case 'project_id': return 'project';
			case 'cycle_id': return 'cycle';
			case 'status_id': return 'status';
			default: return field;
		}
	}
</script>

<Sheet.Root bind:open={sheetOpen}>
	<Sheet.Content side="right" class="h-dvh w-screen max-w-none gap-0 overflow-hidden border-[var(--app-border)] bg-[var(--color-bg)] p-0 sm:w-full sm:max-w-2xl">
		<Sheet.Header class="sticky top-0 z-10 border-b border-[var(--app-border)] bg-[var(--color-bg)] px-4 py-3 text-left sm:px-6">
			<div class="flex items-center justify-between gap-3 pr-8">
				<Sheet.Title class="truncate text-sm font-medium text-[var(--color-text-tertiary)]">{issue.identifier}</Sheet.Title>
				<button
					type="button"
					onclick={toggleSubscription}
					disabled={subscriptionBusy}
					aria-pressed={isSubscribed}
					aria-label={isSubscribed ? 'Disable issue notifications' : 'Enable issue notifications'}
					title={isSubscribed ? 'Disable issue notifications' : 'Notify me about changes'}
					class="inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-md border border-[var(--app-border)] transition-colors hover:bg-[var(--color-bg-hover)] disabled:opacity-50 {isSubscribed ? 'bg-[var(--app-accent)] text-[var(--app-accent-foreground)] border-[var(--app-accent)]' : 'bg-[var(--color-bg-secondary)] text-[var(--color-text-tertiary)]'}"
				>
					<BellIcon size={16} />
				</button>
				<Sheet.Description class="sr-only">Issue details for {issue.identifier}</Sheet.Description>
			</div>
		</Sheet.Header>

		<div class="min-h-0 flex-1 overflow-y-auto">
			<!-- Content -->
			<div class="p-4 sm:p-6">
			<h1 class="text-xl font-semibold text-[var(--color-text-primary)]">{issue.title}</h1>

			{#if issue.description}
				<div class="mt-3 prose prose-invert prose-sm max-w-none text-sm text-[var(--color-text-secondary)]">
					{@html sanitizeHtml(issue.description ?? '')}
				</div>
			{/if}

			<!-- Properties -->
			<div class="mt-6 grid grid-cols-1 gap-3 sm:grid-cols-2">
				<div class="flex items-center gap-2 text-sm">
					<span class="w-20 text-[var(--color-text-tertiary)]">Status</span>
					<StatusSelector
						bind:open={statusOpen}
						statuses={teamStatusesState.statusOrder}
						value={issue.status_id}
						onchange={(id) => { updateStatus(id); }}
					>
						{#snippet trigger()}
							<button class="flex items-center gap-1.5 rounded-md border border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-2.5 py-1 text-xs text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)]">
								<IssueStatusIcon status={issue.status} category={issue.status_info?.category} color={issue.status_info?.color} size={12} />
								{issue.status_info?.name ?? issue.status}
							</button>
						{/snippet}
					</StatusSelector>
				</div>
				<div class="flex items-center gap-2 text-sm">
					<span class="w-20 text-[var(--color-text-tertiary)]">Priority</span>
					<PrioritySelector
						bind:open={priorityOpen}
						value={issue.priority}
						onchange={(p) => { updatePriority(p); }}
					>
						{#snippet trigger()}
							<button class="flex items-center gap-1.5 rounded-md border border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-2.5 py-1 text-xs text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)]">
								<IssuePriorityIcon priority={issue.priority} size={12} />
								{PRIORITY_LABELS[issue.priority]}
							</button>
						{/snippet}
					</PrioritySelector>
				</div>
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
								<span class="font-medium text-[var(--color-text-primary)]"
									>{comment.user?.name ?? 'User'}</span
								>
								<span class="text-[var(--color-text-tertiary)]"
									>{formatRelativeTime(comment.created_at)}</span
								>
							</div>
							<div class="mt-1 prose prose-invert prose-sm max-w-none text-[var(--color-text-secondary)]">
								{@html sanitizeHtml(comment.body ?? '')}
							</div>
						</div>
					{/each}

					<form onsubmit={handleAddComment} class="sticky bottom-0 -mx-4 flex gap-2 border-t border-[var(--app-border)] bg-[var(--color-bg)] px-4 py-3 pb-[calc(0.75rem+env(safe-area-inset-bottom))] sm:static sm:mx-0 sm:border-0 sm:bg-transparent sm:p-0">
						<input
							type="text"
							bind:value={newComment}
							placeholder="Write a comment..."
							class="flex-1 rounded border border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-3 py-2 text-sm text-[var(--color-text-primary)] outline-none focus:border-[var(--app-accent)]"
						/>
						<button
							type="submit"
							disabled={!newComment.trim()}
							class="rounded bg-[var(--app-accent)] px-3 py-2 text-sm text-[var(--app-accent-foreground)] hover:bg-[var(--app-accent-hover)] disabled:opacity-50"
						>
							Send
						</button>
					</form>
				</div>
			{:else}
				<div class="mt-4 relative">
					{#if visibleHistory.length > 0}
						<div class="absolute left-[7px] top-2 bottom-0 w-px bg-[var(--app-border)]"></div>
					{/if}

					{#if hiddenHistoryCount > 0}
						<button
							onclick={() => showAllActivity = true}
							class="relative z-10 mb-2 rounded-full border border-[var(--app-border)] bg-[var(--color-bg)] px-2.5 py-1 text-xs text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-secondary)] transition-colors"
						>
							Show {hiddenHistoryCount} earlier {hiddenHistoryCount === 1 ? 'event' : 'events'}
						</button>
					{/if}

					<div class="space-y-0">
						{#each visibleHistory as entry}
							<div class="relative flex items-center gap-3 pb-3">
								<div class="relative z-10 flex h-4 w-4 shrink-0 items-center justify-center">
									<div class="h-1.5 w-1.5 rounded-full bg-[var(--color-text-tertiary)] ring-2 ring-[var(--color-bg)]"></div>
								</div>
								<div class="flex flex-wrap items-center gap-1.5 text-xs text-[var(--color-text-tertiary)] min-w-0">
									{#if entry.field === 'title' || entry.field === 'description'}
										<span>updated <strong class="text-[var(--color-text-secondary)]">{historyFieldLabel(entry.field)}</strong></span>
									{:else}
										<span>changed <strong class="text-[var(--color-text-secondary)]">{historyFieldLabel(entry.field)}</strong></span>
										{#if entry.old_value || entry.old_display_value}
											<span>from</span>
											<code class="rounded bg-[var(--color-bg-tertiary)] px-1.5 py-0.5 text-[11px] text-[var(--color-text-secondary)]">{formatHistoryValue(entry.field, entry.old_value, entry.old_display_value)}</code>
										{/if}
										<span>to</span>
										<code class="rounded bg-[var(--color-bg-tertiary)] px-1.5 py-0.5 text-[11px] text-[var(--color-text-secondary)]">{formatHistoryValue(entry.field, entry.new_value, entry.new_display_value)}</code>
									{/if}
									<span class="text-[var(--color-text-tertiary)]">&middot;</span>
									<span>{formatRelativeTime(entry.created_at)}</span>
								</div>
							</div>
						{/each}
					</div>

					{#if showAllActivity && sortedHistory.length > RECENT_ACTIVITY_COUNT}
						<button
							onclick={() => showAllActivity = false}
							class="relative z-10 mt-2 rounded-full border border-[var(--app-border)] bg-[var(--color-bg)] px-2.5 py-1 text-xs text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-secondary)] transition-colors"
						>
							Show less
						</button>
					{/if}

					{#if history.length === 0}
						<p class="text-xs text-[var(--color-text-tertiary)]">No activity yet</p>
					{/if}
				</div>
			{/if}
		</div>
	</div>
	</Sheet.Content>
</Sheet.Root>
