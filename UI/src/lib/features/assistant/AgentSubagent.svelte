<script lang="ts">
	import { onDestroy } from 'svelte';
	import { CircleAlert, CircleCheck, LoaderCircle } from '@lucide/svelte';
	import * as Trace from '$lib/components/ui/trace/index.js';
	import { Badge } from '$lib/components/ui/badge/index.js';
	import { agentTasks, type AgentTask } from '$lib/api/agent-tasks';
	import { taskLabel } from './task-labels';
	import { m } from '$lib/paraglide/messages.js';
	let { slug, taskId, identifier }: { slug: string; taskId: string; identifier: string } = $props();
	let task = $state<AgentTask | null>(null);
	let timer: ReturnType<typeof setInterval> | undefined;
	let disposed = false;
	const finished = (state?: string) => state === 'completed' || state === 'cancelled' || state === 'failed';
	const status = $derived(
		!task
			? 'running'
			: task.state === 'failed' || task.state === 'blocked'
				? 'error'
				: finished(task.state) || task.state === 'awaiting_review'
					? 'done'
					: 'running'
	);
	async function load() {
		try {
			const next = await agentTasks.get(slug, taskId);
			if (disposed) return;
			task = next;
			if (finished(next.state)) clearInterval(timer);
		} catch {
			clearInterval(timer);
		}
	}
	$effect(() => {
		void taskId;
		void load();
		clearInterval(timer);
		timer = setInterval(() => void load(), 15_000);
	});
	onDestroy(() => {
		disposed = true;
		clearInterval(timer);
	});
</script>

<!-- Official AI Elements Subagent composition: the coding agent this approval started. -->
<Trace.Root class="min-w-0">
	<Trace.Collapsible>
		<Trace.CollapsibleTrigger>
			<Trace.Line status={status === 'done' ? undefined : status}>
				<Trace.Icon
					>{#if status === 'done'}<CircleCheck />{:else if status === 'error'}<CircleAlert />{:else}<LoaderCircle
							class="animate-spin"
						/>{/if}</Trace.Icon
				>
				<Trace.Title class="whitespace-normal break-words">{m['assistant.subagent']({ identifier })}</Trace.Title>
				{#if task}<Trace.Description>{taskLabel(task.state)}</Trace.Description>{/if}
			</Trace.Line>
		</Trace.CollapsibleTrigger>
		<Trace.CollapsibleContent class="flex flex-col gap-2 text-xs">
			{#if task}
				<div class="flex flex-wrap gap-1.5">
					<Badge variant="secondary">{task.provider}</Badge>
					{#if task.model}<Badge variant="outline" class="font-mono">{task.model}</Badge>{/if}
				</div>
				<p class="whitespace-pre-wrap break-words">{task.reason}</p>
				{#if task.conversation_id}<a
						class="underline underline-offset-2"
						href="/{slug}/assistant?conversation={encodeURIComponent(task.conversation_id)}"
						>{m['assistant.subagent_open']()}</a
					>{/if}
			{/if}
		</Trace.CollapsibleContent>
	</Trace.Collapsible>
</Trace.Root>
