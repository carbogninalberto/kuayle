<script lang="ts">
	import { onDestroy } from 'svelte';
	import { CircleCheck, CircleDot, ListChecks, ListTodo, CircleAlert } from '@lucide/svelte';
	import * as Trace from '$lib/components/ui/trace/index.js';
	import { agentTasks, type AgentQueueItem } from '$lib/api/agent-tasks';
	import { taskLabel } from './task-labels';
	import { taskTone, toneStatus } from './task-tone';
	import { useTerminalDock } from '$lib/features/dev-machines/terminal-dock-context.svelte';
	import { m } from '$lib/paraglide/messages.js';

	let { slug }: { slug: string } = $props();
	const dock = useTerminalDock();
	let items = $state<AgentQueueItem[]>([]);
	let timer: ReturnType<typeof setInterval> | undefined;
	let disposed = false;
	const finished = ['completed', 'cancelled', 'failed'];
	const todo = $derived(items.filter((item) => !finished.includes(item.state)));
	const done = $derived(items.filter((item) => finished.includes(item.state)));

	async function load() {
		if (typeof document !== 'undefined' && document.hidden) return;
		try {
			const next = await agentTasks.queue(slug);
			if (!disposed) items = next;
			if (next.length) dock?.trackAgents();
		} catch {
			// The queue is a convenience; the conversations remain authoritative.
		}
	}
	$effect(() => {
		void slug;
		void load();
		clearInterval(timer);
		timer = setInterval(() => void load(), 20_000);
	});
	onDestroy(() => {
		disposed = true;
		clearInterval(timer);
	});
</script>

{#snippet itemLine(item: AgentQueueItem)}
	<a
		href="/{slug}/assistant?conversation={encodeURIComponent(item.conversation_id)}"
		class="block rounded-md px-2 py-1 hover:bg-muted"
		title="{item.issue_identifier} · {item.issue_title} — {taskLabel(item.state)}"
	>
		<!-- The narrow sidebar shows the issue; the coloured icon and the tooltip
		     carry the state, which the screen reader label also includes. -->
		<Trace.Line status={toneStatus(taskTone(item.state))} class="w-full">
			<Trace.Icon
				>{#if item.state === 'completed'}<CircleCheck
					/>{:else if item.state === 'failed' || item.state === 'blocked'}<CircleAlert />{:else}<CircleDot
					/>{/if}</Trace.Icon
			>
			<Trace.Title class="min-w-0 flex-1 truncate">{item.issue_identifier} · {item.issue_title}</Trace.Title>
			<span class="sr-only">{taskLabel(item.state)}</span>
		</Trace.Line>
	</a>
{/snippet}

<!-- Official AI Elements Queue composition for the user's agent work across projects. -->
{#if items.length}
	<section aria-label={m['tasks.queue']()} class="min-w-0">
		<!-- Aligned with the conversation links above (px-2). -->
		<p class="px-2 pt-1 pb-2 text-xs font-medium text-muted-foreground">{m['tasks.queue']()}</p>
		<Trace.Root class="min-w-0 px-2">
			{#if todo.length}
				<Trace.Collapsible open>
					<Trace.CollapsibleTrigger
						><Trace.Line
							><Trace.Icon><ListTodo /></Trace.Icon><Trace.Title
								>{m['tasks.queue_todo']({ count: todo.length })}</Trace.Title
							></Trace.Line
						></Trace.CollapsibleTrigger
					>
					<Trace.CollapsibleContent
						><Trace.Root
							>{#each todo as item (item.id)}{@render itemLine(item)}{/each}</Trace.Root
						></Trace.CollapsibleContent
					>
				</Trace.Collapsible>
			{/if}
			{#if done.length}
				<Trace.Collapsible>
					<Trace.CollapsibleTrigger
						><Trace.Line
							><Trace.Icon><ListChecks /></Trace.Icon><Trace.Title
								>{m['tasks.queue_done']({ count: done.length })}</Trace.Title
							></Trace.Line
						></Trace.CollapsibleTrigger
					>
					<Trace.CollapsibleContent
						><Trace.Root
							>{#each done as item (item.id)}{@render itemLine(item)}{/each}</Trace.Root
						></Trace.CollapsibleContent
					>
				</Trace.Collapsible>
			{/if}
		</Trace.Root>
	</section>
{/if}
