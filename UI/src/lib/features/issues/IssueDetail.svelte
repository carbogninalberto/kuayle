<script lang="ts">
	import { onMount } from 'svelte';
	import type { Issue, Comment, IssueHistory } from '$lib/types/issue';
	import { STATUS_LABELS, PRIORITY_LABELS } from '$lib/types/issue';
	import { listComments, createComment, getIssueHistory } from '$lib/api/issues';
	import { issuesState } from './issues.state.svelte';
	import IssueStatusIcon from './IssueStatusIcon.svelte';
	import IssuePriorityIcon from './IssuePriorityIcon.svelte';
	import { formatRelativeTime } from '$lib/utils/format';
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
		const comment = await createComment(slug, issue.identifier, newComment);
		comments = [...comments, comment];
		newComment = '';
	}

	async function updateStatus(status: string) {
		await issuesState.update(slug, issue.identifier, { status: status as any });
	}

	async function updatePriority(priority: number) {
		await issuesState.update(slug, issue.identifier, { priority: priority as any });
	}
</script>

<!-- svelte-ignore a11y_no_static_element_interactions -->
<div
	class="fixed inset-0 z-40 flex justify-end"
	onkeydown={(e) => e.key === 'Escape' && onclose()}
>
	<!-- svelte-ignore a11y_no_static_element_interactions -->
	<div class="flex-1" onclick={onclose}></div>

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
				<p class="mt-3 whitespace-pre-wrap text-sm text-[var(--color-text-secondary)]">
					{issue.description}
				</p>
			{/if}

			<!-- Properties -->
			<div class="mt-6 grid grid-cols-2 gap-3">
				<div class="flex items-center gap-2 text-sm">
					<span class="w-20 text-[var(--color-text-tertiary)]">Status</span>
					<select
						value={issue.status}
						onchange={(e) => updateStatus((e.target as HTMLSelectElement).value)}
						class="rounded border border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-2 py-1 text-sm text-[var(--color-text-secondary)]"
					>
						{#each Object.entries(STATUS_LABELS) as [value, label]}
							<option {value}>{label}</option>
						{/each}
					</select>
				</div>
				<div class="flex items-center gap-2 text-sm">
					<span class="w-20 text-[var(--color-text-tertiary)]">Priority</span>
					<select
						value={issue.priority}
						onchange={(e) => updatePriority(Number((e.target as HTMLSelectElement).value))}
						class="rounded border border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-2 py-1 text-sm text-[var(--color-text-secondary)]"
					>
						{#each Object.entries(PRIORITY_LABELS) as [value, label]}
							<option value={Number(value)}>{label}</option>
						{/each}
					</select>
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
							<p class="mt-1 whitespace-pre-wrap text-[var(--color-text-secondary)]">
								{comment.body}
							</p>
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
							<span class="text-[var(--color-text-tertiary)]"
								>{formatRelativeTime(entry.created_at)}</span
							>
							<span class="text-[var(--color-text-secondary)]">
								changed <strong>{entry.field}</strong>
								{#if entry.old_value}from <code
										class="rounded bg-[var(--color-bg-tertiary)] px-1"
										>{entry.old_value}</code
									>{/if}
								to <code class="rounded bg-[var(--color-bg-tertiary)] px-1"
									>{entry.new_value}</code
								>
							</span>
						</div>
					{/each}
					{#if history.length === 0}
						<p class="text-sm text-[var(--color-text-tertiary)]">No activity yet</p>
					{/if}
				</div>
			{/if}
		</div>
	</div>
</div>
