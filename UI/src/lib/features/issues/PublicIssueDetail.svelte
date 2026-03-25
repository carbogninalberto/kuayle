<script lang="ts">
	import type { PublicIssue } from '$lib/types/shared-link';
	import type { IssuePriority } from '$lib/types/issue';
	import { PRIORITY_LABELS } from '$lib/types/issue';
	import IssueStatusIcon from './IssueStatusIcon.svelte';
	import IssuePriorityIcon from './IssuePriorityIcon.svelte';
	import { formatRelativeTime } from '$lib/utils/format';
	import { sanitizeHtml } from '$lib/security/sanitize';
	import { X, CalendarDays, ChevronUp, ChevronDown } from 'lucide-svelte';
	import * as Tooltip from '$lib/components/ui/tooltip';
	import { onMount, onDestroy } from 'svelte';

	let {
		issue = $bindable(),
		issues = [],
		onclose
	}: {
		issue: PublicIssue;
		issues?: PublicIssue[];
		onclose: () => void;
	} = $props();

	const currentIndex = $derived(issues.findIndex(i => i.identifier === issue.identifier));
	const hasPrev = $derived(currentIndex > 0);
	const hasNext = $derived(currentIndex < issues.length - 1);

	function goUp() {
		if (hasPrev) issue = issues[currentIndex - 1];
	}

	function goDown() {
		if (hasNext) issue = issues[currentIndex + 1];
	}

	const ANIM_DURATION = 300;
	let visible = $state(false);
	let closing = false;

	onMount(() => {
		requestAnimationFrame(() => {
			requestAnimationFrame(() => {
				visible = true;
			});
		});
		document.addEventListener('keydown', handleKeydown);
	});

	onDestroy(() => {
		document.removeEventListener('keydown', handleKeydown);
	});

	function handleKeydown(e: KeyboardEvent) {
		if (e.key === 'Escape') {
			e.preventDefault();
			close();
		} else if (e.key === 'ArrowUp' || e.key === 'k') {
			e.preventDefault();
			goUp();
		} else if (e.key === 'ArrowDown' || e.key === 'j') {
			e.preventDefault();
			goDown();
		}
	}

	function close() {
		if (closing) return;
		closing = true;
		visible = false;
		setTimeout(onclose, ANIM_DURATION);
	}
</script>

<!-- Overlay -->
<div class="fixed inset-0 z-40 flex justify-end">
	<!-- Backdrop -->
	<button
		class="flex-1 cursor-default"
		style="background: transparent;"
		onclick={close}
		tabindex={-1}
		aria-label="Close issue detail"
	></button>

	<!-- Drawer -->
	<div
		class="w-full max-w-2xl overflow-y-auto rounded-l-2xl border-l border-[var(--app-border)] bg-[var(--color-bg)] shadow-2xl"
		style="transform: translateX({visible ? '0' : '100%'}); transition: transform {ANIM_DURATION}ms cubic-bezier(0.25, 1, 0.5, 1);"
	>
		<!-- Header -->
		<div class="sticky top-0 z-10 flex items-center justify-between bg-[var(--color-bg)] rounded-tl-2xl px-6 py-3">
			<span class="text-sm font-medium text-[var(--color-text-tertiary)]">{issue.identifier}</span>
			<div class="flex items-center gap-1">
				<span class="text-xs text-[var(--color-text-tertiary)] mr-1">{formatRelativeTime(issue.created_at)}</span>
				{#if issues.length > 1}
					<button
						onclick={goUp}
						disabled={!hasPrev}
						class="shrink-0 rounded-md p-1 text-[var(--color-text-tertiary)] hover:text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)] transition-colors disabled:opacity-25 disabled:pointer-events-none"
						title="Previous issue"
					>
						<ChevronUp size={16} />
					</button>
					<button
						onclick={goDown}
						disabled={!hasNext}
						class="shrink-0 rounded-md p-1 text-[var(--color-text-tertiary)] hover:text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)] transition-colors disabled:opacity-25 disabled:pointer-events-none"
						title="Next issue"
					>
						<ChevronDown size={16} />
					</button>
				{/if}
				<button
					onclick={close}
					class="shrink-0 rounded-md p-1 text-[var(--color-text-tertiary)] hover:text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)] transition-colors"
				>
					<X size={16} />
				</button>
			</div>
		</div>

		<!-- Body -->
		<div class="px-6 pb-6">
			<h1 class="text-xl font-semibold text-[var(--color-text-primary)]">{issue.title}</h1>

			<!-- Properties row -->
			<div class="flex flex-wrap items-center gap-1.5 mt-3">
				<!-- Status -->
				<Tooltip.Root>
					<Tooltip.Trigger>
						<span class="flex items-center gap-1.5 rounded-md border border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-2 py-1 text-xs text-[var(--color-text-secondary)]">
							<IssueStatusIcon status={issue.status} category={issue.status_info?.category} color={issue.status_info?.color} size={12} />
							{issue.status_info?.name ?? issue.status}
						</span>
					</Tooltip.Trigger>
					<Tooltip.Content>Status</Tooltip.Content>
				</Tooltip.Root>

				<!-- Priority -->
				<Tooltip.Root>
					<Tooltip.Trigger>
						<span class="flex items-center gap-1.5 rounded-md border border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-2 py-1 text-xs text-[var(--color-text-secondary)]">
							<IssuePriorityIcon priority={issue.priority as IssuePriority} size={12} />
							{PRIORITY_LABELS[issue.priority as IssuePriority]}
						</span>
					</Tooltip.Trigger>
					<Tooltip.Content>Priority</Tooltip.Content>
				</Tooltip.Root>

				<!-- Assignees -->
				{#if issue.assignees && issue.assignees.length > 0}
					<Tooltip.Root>
						<Tooltip.Trigger>
							<span class="flex items-center gap-1 rounded-md border border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-2 py-1">
								{#each issue.assignees.slice(0, 3) as assignee}
									<span class="flex h-4 w-4 items-center justify-center rounded-full bg-[var(--app-accent)] text-[8px] font-medium text-[var(--app-accent-foreground)]">
										{(assignee.name ?? 'U').charAt(0).toUpperCase()}
									</span>
								{/each}
								{#if issue.assignees.length > 3}
									<span class="text-[10px] text-[var(--color-text-tertiary)] ml-0.5">+{issue.assignees.length - 3}</span>
								{/if}
							</span>
						</Tooltip.Trigger>
						<Tooltip.Content>{issue.assignees.map(a => a.display_name || a.name).join(', ')}</Tooltip.Content>
					</Tooltip.Root>
				{/if}

				<!-- Due date -->
				{#if issue.due_date}
					{@const due = new Date(issue.due_date)}
					{@const now = new Date()}
					{@const diffDays = Math.ceil((due.getTime() - now.getTime()) / 86400000)}
					<Tooltip.Root>
						<Tooltip.Trigger>
							<span class="flex items-center gap-1 rounded-md border border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-2 py-1 text-xs text-[var(--color-text-secondary)]">
								<CalendarDays size={11} class={diffDays < 0 ? 'text-red-500' : diffDays === 0 ? 'text-orange-500' : 'text-[var(--color-text-tertiary)]'} />
								{due.toLocaleDateString('en-US', { month: 'short', day: 'numeric' })}
							</span>
						</Tooltip.Trigger>
						<Tooltip.Content>Due date</Tooltip.Content>
					</Tooltip.Root>
				{/if}

				<!-- Estimate -->
				{#if issue.estimate != null}
					<Tooltip.Root>
						<Tooltip.Trigger>
							<span class="rounded-md border border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-2 py-1 text-xs text-[var(--color-text-secondary)]">
								{issue.estimate}pt
							</span>
						</Tooltip.Trigger>
						<Tooltip.Content>Estimate</Tooltip.Content>
					</Tooltip.Root>
				{/if}

				<!-- Labels -->
				{#if issue.labels && issue.labels.length > 0}
					{#each issue.labels.slice(0, 3) as label}
						<span class="flex items-center gap-1 rounded-full border border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-2 py-0.5 text-[11px] text-[var(--color-text-secondary)]">
							<span class="h-1.5 w-1.5 rounded-full shrink-0" style="background-color: {label.color}"></span>
							{label.name}
						</span>
					{/each}
					{#if issue.labels.length > 3}
						<Tooltip.Root>
							<Tooltip.Trigger>
								<span class="text-[10px] text-[var(--color-text-tertiary)]">+{issue.labels.length - 3}</span>
							</Tooltip.Trigger>
							<Tooltip.Content>{issue.labels.slice(3).map(l => l.name).join(', ')}</Tooltip.Content>
						</Tooltip.Root>
					{/if}
				{/if}
			</div>

			{#if issue.description}
				<div class="description-content mt-5 max-w-none text-sm text-[var(--color-text-secondary)] leading-relaxed">
					{@html sanitizeHtml(issue.description)}
				</div>
			{/if}
		</div>
	</div>
</div>

<style>
	:global(.description-content p) {
		margin: 0.25rem 0;
	}
	:global(.description-content h1) {
		font-size: 1.25rem;
		font-weight: 600;
		margin: 0.75rem 0 0.25rem;
		color: var(--color-text-primary);
	}
	:global(.description-content h2) {
		font-size: 1.1rem;
		font-weight: 600;
		margin: 0.5rem 0 0.25rem;
		color: var(--color-text-primary);
	}
	:global(.description-content h3) {
		font-size: 1rem;
		font-weight: 600;
		margin: 0.5rem 0 0.25rem;
		color: var(--color-text-primary);
	}
	:global(.description-content ul,
	.description-content ol) {
		padding-left: 1.5rem;
		margin: 0.25rem 0;
	}
	:global(.description-content ul) {
		list-style: disc;
	}
	:global(.description-content ol) {
		list-style: decimal;
	}
	:global(.description-content ul[data-type="taskList"]) {
		list-style: none;
		padding-left: 0;
	}
	:global(.description-content ul[data-type="taskList"] li) {
		display: flex;
		align-items: flex-start;
		gap: 0.5rem;
	}
	:global(.description-content ul[data-type="taskList"] li input) {
		margin-top: 0.25rem;
		pointer-events: none;
	}
	:global(.description-content code) {
		background: var(--color-bg-tertiary);
		padding: 0.125rem 0.25rem;
		border-radius: 0.25rem;
		font-size: 0.8em;
	}
	:global(.description-content pre) {
		background: var(--color-bg-tertiary);
		padding: 0.75rem 1rem;
		border-radius: 0.375rem;
		margin: 0.5rem 0;
		overflow-x: auto;
	}
	:global(.description-content pre code) {
		background: none;
		padding: 0;
	}
	:global(.description-content blockquote) {
		border-left: 3px solid var(--app-border);
		padding-left: 1rem;
		margin: 0.5rem 0;
		color: var(--color-text-secondary);
	}
	:global(.description-content a) {
		color: var(--app-accent-light);
		text-decoration: underline;
	}
	:global(.description-content hr) {
		border: none;
		border-top: 1px solid var(--app-border);
		margin: 1rem 0;
	}
	:global(.description-content img) {
		max-width: 100%;
		height: auto;
		border-radius: 0.375rem;
		margin: 0.5rem 0;
	}
	:global(.description-content strong) {
		font-weight: 600;
		color: var(--color-text-primary);
	}
	:global(.description-content em) {
		font-style: italic;
	}
	:global(.description-content s) {
		text-decoration: line-through;
	}
</style>
