<script lang="ts">
	import { onDestroy } from 'svelte';
	import { CircleAlert, CircleCheck, LoaderCircle, MessagesSquare, RotateCcw, Hourglass } from '@lucide/svelte';
	import { Button } from '$lib/components/ui/button/index.js';
	import AgentLogo from '$lib/features/agents/AgentLogo.svelte';
	import { agentInfo } from '$lib/features/agents/agents';
	import type { AgentTask } from '$lib/api/agent-tasks';
	import { taskLabel } from './task-labels';
	import { elapsed, formatDuration, taskIndicator } from './shell-rules';
	import { m } from '$lib/features/i18n/messages';

	// A coding task where it happened in the conversation: one compact card
	// with its agent, state and time, which opens the agent's own thread. It
	// stays in the transcript after the task ends, failed tasks included,
	// with their reason and a retry.
	let {
		task,
		identifier,
		onopen,
		onretry
	}: {
		task: AgentTask;
		identifier: string;
		onopen: () => void;
		onretry?: () => void;
	} = $props();
	const indicator = $derived(taskIndicator(task.state));
	const failed = $derived(task.state === 'failed' || task.state === 'blocked');
	// Running tasks count up; finished ones show how long they took.
	let now = $state(Date.now());
	const timer = setInterval(() => (now = Date.now()), 30_000);
	onDestroy(() => clearInterval(timer));
	const time = $derived.by(() => {
		const end = indicator === 'running' ? new Date(now).toISOString() : task.updated_at;
		const ms = elapsed(task.created_at, end);
		return ms === undefined ? '' : formatDuration(ms);
	});
	const agent = $derived(agentInfo(task.provider).name);
</script>

<div
	role="group"
	aria-label={m['assistant.task_step_label']({ identifier })}
	class="flex min-w-0 items-start gap-2.5 rounded-xl border bg-muted/20 px-3 py-2"
	data-state={task.state}
>
	<AgentLogo agent={task.provider} size="sm" class="mt-0.5 shrink-0" />
	<div class="min-w-0 flex-1">
		<p class="truncate text-sm font-medium">{m['assistant.task_step_title']({ agent, identifier })}</p>
		<p class="flex min-w-0 items-center gap-1.5 text-xs text-muted-foreground">
			{#if indicator === 'running'}<LoaderCircle
					size={12}
					class="shrink-0 animate-spin text-[var(--status-running)]"
					aria-hidden="true"
				/>{:else if indicator === 'failed'}<CircleAlert
					size={12}
					class="shrink-0 text-[var(--status-error)]"
					aria-hidden="true"
				/>{:else if indicator === 'waiting'}<Hourglass
					size={12}
					class="shrink-0 text-[var(--status-warning)]"
					aria-hidden="true"
				/>{:else}<CircleCheck size={12} class="shrink-0 text-[var(--status-success)]" aria-hidden="true" />{/if}
			<span class="truncate"
				>{m['assistant.task_step_status']({
					state: taskLabel(task.state),
					attempt: String(task.generation),
					time
				})}</span
			>
		</p>
		{#if failed && task.reason}<p class="mt-1 line-clamp-2 text-xs break-words text-destructive">
				{m['assistant.task_step_error']({ reason: task.reason })}
			</p>{/if}
	</div>
	<div class="flex shrink-0 items-center gap-1">
		{#if failed && onretry}<Button variant="ghost" size="sm" class="h-7 px-2 text-xs" onclick={onretry}
				><RotateCcw />{m['assistant.task_step_retry']()}</Button
			>{/if}
		<Button variant="outline" size="sm" class="h-7 px-2 text-xs" onclick={onopen}
			><MessagesSquare />{m['assistant.task_step_open']()}</Button
		>
	</div>
</div>
