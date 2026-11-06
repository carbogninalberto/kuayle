<script lang="ts">
	import { ChevronRight } from '@lucide/svelte';
	import { Button } from '$lib/components/ui/button/index.js';
	import * as Collapsible from '$lib/components/ui/collapsible/index.js';
	import AgentLogo from '$lib/features/agents/AgentLogo.svelte';
	import type { AgentQueueItem, AgentTask } from '$lib/api/agent-tasks';
	import { taskLabel } from './task-labels';
	import { formatDuration, splitBackgroundTasks, taskIndicator, type TaskIndicator } from './shell-rules';
	import { m } from '$lib/paraglide/messages.js';

	// The person's agent work across conversations, one row per task. Running,
	// waiting and failed tasks stay listed; only completed or cancelled ones
	// roll into the "N finished" row. Each row opens that task's agent thread.
	let {
		slug,
		items,
		live = {},
		limit = 5
	}: {
		slug: string;
		items: AgentQueueItem[];
		// Live task states of the open conversation, fresher than the polled queue.
		live?: Record<string, Pick<AgentTask, 'state'>>;
		limit?: number;
	} = $props();
	let showAll = $state(false);
	let finishedOpen = $state(false);
	const split = $derived(splitBackgroundTasks(items, live));
	const shown = $derived(showAll ? split.open : split.open.slice(0, limit));
	const dot: Record<TaskIndicator | 'done', string> = {
		running: 'bg-[var(--status-running)] animate-pulse motion-reduce:animate-none',
		waiting: 'bg-[var(--status-warning)]',
		failed: 'bg-[var(--status-error)]',
		done: 'bg-muted-foreground/40'
	};
	const href = (item: AgentQueueItem) =>
		`/${slug}/assistant?conversation=${encodeURIComponent(item.conversation_id)}&task=${encodeURIComponent(item.id)}`;
	const ago = (value: string) => {
		const ms = Date.now() - Date.parse(value);
		return Number.isFinite(ms) && ms >= 0 ? formatDuration(Math.max(ms, 60_000)).replace(/ \d+s$/, '') : '';
	};
</script>

{#snippet row(item: AgentQueueItem)}
	{@const indicator = taskIndicator(item.state)}
	<li>
		<a
			href={href(item)}
			class="group flex h-8 min-w-0 items-center gap-2 rounded-md px-2 text-sm text-muted-foreground outline-none hover:bg-muted hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/50"
			title="{item.issue_identifier} · {item.issue_title} — {taskLabel(item.state)}"
		>
			<AgentLogo agent={item.provider || 'custom'} size="sm" class="shrink-0" />
			<span class="min-w-0 flex-1 truncate">{item.issue_identifier} · {item.issue_title}</span>
			<span class="shrink-0 text-[11px] tabular-nums">{ago(item.updated_at)}</span>
			<span
				role="img"
				aria-label={taskLabel(item.state)}
				class="size-2 shrink-0 rounded-full {dot[indicator ?? 'done']}"
			></span>
		</a>
	</li>
{/snippet}

<section aria-label={m['assistant.background_tasks']()} class="min-w-0">
	{#if !items.length}
		<p class="px-2 py-1 text-xs text-muted-foreground">{m['assistant.background_tasks_empty']()}</p>
	{:else}
		{#if split.open.length}
			<p class="sr-only">{m['tasks.queue_todo']({ count: split.open.length })}</p>
			<ul class="flex flex-col">
				{#each shown as item (item.id)}{@render row(item)}{/each}
			</ul>
			{#if split.open.length > limit}<Button
					variant="ghost"
					size="sm"
					class="h-7 px-2 text-xs text-muted-foreground"
					onclick={() => (showAll = !showAll)}
					>{showAll
						? m['assistant.show_less']()
						: m['assistant.show_more']({ count: split.open.length - limit })}</Button
				>{/if}
		{/if}
		{#if split.finished.length}
			<Collapsible.Root bind:open={finishedOpen}>
				<Collapsible.Trigger>
					{#snippet child({ props })}
						<Button
							{...props}
							variant="plain"
							class="flex h-8 w-full items-center gap-2 rounded-md px-2 text-left text-xs text-muted-foreground hover:bg-muted hover:text-foreground"
							><ChevronRight
								size={14}
								class="shrink-0 transition-transform {finishedOpen ? 'rotate-90' : ''}"
								aria-hidden="true"
							/>{m['tasks.queue_done']({ count: split.finished.length })}</Button
						>
					{/snippet}
				</Collapsible.Trigger>
				<Collapsible.Content>
					<ul class="flex flex-col">
						{#each split.finished as item (item.id)}{@render row(item)}{/each}
					</ul>
				</Collapsible.Content>
			</Collapsible.Root>
		{/if}
	{/if}
</section>
