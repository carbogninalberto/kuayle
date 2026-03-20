<script lang="ts">
	import { onMount } from 'svelte';
	import type { Issue, Comment, IssueHistory, IssuePriority } from '$lib/types/issue';
	import { PRIORITY_LABELS } from '$lib/types/issue';
	import { teamStatusesState } from './team-statuses.state.svelte';
	import { listComments, createComment, getIssueHistory } from '$lib/api/issues';
	import { issuesState } from './issues.state.svelte';
	import IssueStatusIcon from './IssueStatusIcon.svelte';
	import IssuePriorityIcon from './IssuePriorityIcon.svelte';
	import { formatRelativeTime } from '$lib/utils/format';
	import { toast } from 'svelte-sonner';
	import * as Popover from '$lib/components/ui/popover';
	import { X } from 'lucide-svelte';

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

	const priorityValues: IssuePriority[] = [0, 1, 2, 3, 4];

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
			toast.success('Comment added');
		} catch (err: any) {
			toast.error(err?.error?.message || 'Failed to add comment');
		}
	}

	async function updateStatus(statusId: string) {
		try {
			await issuesState.update(slug, issue.identifier, { status_id: statusId });
			toast.success('Status updated');
		} catch (err: any) {
			toast.error(err?.error?.message || 'Failed to update status');
		}
	}

	async function updatePriority(priority: number) {
		try {
			await issuesState.update(slug, issue.identifier, { priority: priority as any });
			toast.success('Priority updated');
		} catch (err: any) {
			toast.error(err?.error?.message || 'Failed to update priority');
		}
	}
</script>

<!-- svelte-ignore a11y_no_static_element_interactions -->
<div
	class="fixed inset-0 z-40 flex justify-end"
	onkeydown={(e) => e.key === 'Escape' && onclose()}
>
	<button class="flex-1 cursor-default" onclick={onclose} tabindex={-1}></button>

	<div
		class="w-full max-w-2xl overflow-y-auto border-l border-[var(--app-border)] bg-[var(--color-bg)] shadow-2xl"
	>
		<!-- Header -->
		<div
			class="sticky top-0 z-10 flex items-center justify-between border-b border-[var(--app-border)] bg-[var(--color-bg)] px-6 py-3"
		>
			<span class="text-sm text-[var(--color-text-tertiary)]">{issue.identifier}</span>
			<button
				onclick={onclose}
				class="text-[var(--color-text-tertiary)] hover:text-[var(--color-text-primary)]"
			>
				<X size={18} />
			</button>
		</div>

		<!-- Content -->
		<div class="p-6">
			<h1 class="text-xl font-semibold text-[var(--color-text-primary)]">{issue.title}</h1>

			{#if issue.description}
				<div class="mt-3 prose prose-invert prose-sm max-w-none text-sm text-[var(--color-text-secondary)]">
					{@html issue.description}
				</div>
			{/if}

			<!-- Properties -->
			<div class="mt-6 grid grid-cols-2 gap-3">
				<div class="flex items-center gap-2 text-sm">
					<span class="w-20 text-[var(--color-text-tertiary)]">Status</span>
					<Popover.Root bind:open={statusOpen}>
						<Popover.Trigger>
							<button class="flex items-center gap-1.5 rounded-md border border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-2.5 py-1 text-xs text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)]">
								<IssueStatusIcon status={issue.status} category={issue.status_info?.category} color={issue.status_info?.color} size={12} />
								{issue.status_info?.name ?? issue.status}
							</button>
						</Popover.Trigger>
						<Popover.Content class="w-44 p-1" align="start">
							{#each teamStatusesState.statusOrder as ts}
								<button
									onclick={() => { updateStatus(ts.id); statusOpen = false; }}
									class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)] {issue.status_id === ts.id ? 'bg-[var(--color-bg-hover)]' : ''}"
								>
									<IssueStatusIcon category={ts.category} color={ts.color} size={14} />
									{ts.name}
								</button>
							{/each}
						</Popover.Content>
					</Popover.Root>
				</div>
				<div class="flex items-center gap-2 text-sm">
					<span class="w-20 text-[var(--color-text-tertiary)]">Priority</span>
					<Popover.Root bind:open={priorityOpen}>
						<Popover.Trigger>
							<button class="flex items-center gap-1.5 rounded-md border border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-2.5 py-1 text-xs text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)]">
								<IssuePriorityIcon priority={issue.priority} size={12} />
								{PRIORITY_LABELS[issue.priority]}
							</button>
						</Popover.Trigger>
						<Popover.Content class="w-40 p-1" align="start">
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
				<div class="mt-4 relative">
					{#if history.length > 0}
						<div class="absolute left-[7px] top-2 bottom-0 w-px bg-[var(--app-border)]"></div>
					{/if}
					<div class="space-y-0">
						{#each history as entry}
							<div class="relative flex items-center gap-3 pb-3">
								<div class="relative z-10 flex h-4 w-4 shrink-0 items-center justify-center">
									<div class="h-1.5 w-1.5 rounded-full bg-[var(--color-text-tertiary)] ring-2 ring-[var(--color-bg)]"></div>
								</div>
								<div class="flex flex-wrap items-center gap-1.5 text-xs text-[var(--color-text-tertiary)] min-w-0">
									{#if entry.field === 'title' || entry.field === 'description'}
										<span>updated <strong class="text-[var(--color-text-secondary)]">{entry.field}</strong></span>
									{:else}
										<span>changed <strong class="text-[var(--color-text-secondary)]">{entry.field}</strong></span>
										{#if entry.old_value}
											<span>from</span>
											<code class="rounded bg-[var(--color-bg-tertiary)] px-1.5 py-0.5 text-[11px] text-[var(--color-text-secondary)]">{entry.old_value}</code>
										{/if}
										<span>to</span>
										<code class="rounded bg-[var(--color-bg-tertiary)] px-1.5 py-0.5 text-[11px] text-[var(--color-text-secondary)]">{entry.new_value}</code>
									{/if}
									<span class="text-[var(--color-text-tertiary)]">&middot;</span>
									<span>{formatRelativeTime(entry.created_at)}</span>
								</div>
							</div>
						{/each}
					</div>
					{#if history.length === 0}
						<p class="text-xs text-[var(--color-text-tertiary)]">No activity yet</p>
					{/if}
				</div>
			{/if}
		</div>
	</div>
</div>
