<script lang="ts">
	import { Code, RefreshCw } from '@lucide/svelte';
	import { Button } from '$lib/components/ui/button/index.js';
	import type { ConversationState } from './conversation-state.svelte';
	import type { TaskBoard } from './task-board.svelte';
	import TaskCard from './TaskCard.svelte';
	import { m } from '$lib/features/i18n/messages';
	import { useTerminalDock } from '$lib/features/dev-machines/terminal-dock-context.svelte';

	// The issue conversation's coding tasks, newest first. Each task (TaskCard)
	// is its state, its actions, the path it took and the execution details;
	// talking to its agent and reviews are in the agent view. Finished and
	// failed tasks stay listed.
	let { chat, board }: { chat: ConversationState; board: TaskBoard } = $props();
	const dock = useTerminalDock();
	// Seeing agent work turns on the dock's Agents tab for this workspace.
	$effect(() => {
		if (board.tasks.length) dock?.trackAgents();
	});
</script>

<section aria-label={m['tasks.title']()} class="min-w-0 space-y-1">
	<div class="flex items-center justify-between gap-3">
		<h2 class="text-[11px] font-medium tracking-wide text-muted-foreground uppercase">{m['tasks.title']()}</h2>
		{#if !board.denied}<Button
				size="sm"
				variant="outline"
				disabled={!board.options?.providers.length || board.loading}
				onclick={() => board.startTask()}><Code size={14} />{m['tasks.start']()}</Button
			>{/if}
	</div>
	{#if board.denied}<p class="text-sm text-muted-foreground" role="status">{m['tasks.unavailable']()}</p>
	{:else}
		{#if board.loading}<p class="text-xs text-muted-foreground">{m['assistant.loading']()}</p>{/if}
		{#if board.failed}<p role="alert" class="text-sm text-destructive">{m['tasks.load_error']()}</p>
			<Button variant="ghost" size="sm" onclick={() => void board.load()}
				><RefreshCw size={14} />{m['tasks.refresh']()}</Button
			>{/if}
		{#if board.optionsUnavailable}<p class="text-xs text-muted-foreground">
				{m['tasks.configuration_unavailable']()}
			</p>{/if}
		{#if !board.loading && !board.failed && !board.tasks.length}<p class="py-2 text-xs text-muted-foreground">
				{m['assistant.agent_panel_empty']()}
			</p>{/if}
		<div class="divide-y divide-border">
			{#each board.tasks as task, i (task.id)}<TaskCard
					{task}
					{chat}
					pinned={i === 0}
					onretry={(value) => board.retry(value)}
					ondenied={(e) => board.fail(e)}
				/>{/each}
		</div>
		{#if board.before}<Button
				variant="ghost"
				size="sm"
				disabled={board.loadingOlder}
				onclick={() => void board.older()}>{m['tasks.older']()}</Button
			>{/if}
	{/if}
</section>
