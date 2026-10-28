<script lang="ts">
	import { ChevronRight } from '@lucide/svelte';
	import * as Card from '$lib/components/ui/card';
	import { Badge } from '$lib/components/ui/badge/index.js';
	import AgentLogo from '$lib/features/agents/AgentLogo.svelte';
	import { agentInfo } from '$lib/features/agents/agents';
	import { taskLabel } from '$lib/features/assistant/task-labels';
	import { taskTone, toneBadge } from '$lib/features/assistant/task-tone';
	import type { AgentQueueItem } from '$lib/api/agent-tasks';
	import { m } from '$lib/paraglide/messages.js';

	// The agent tasks that used this machine (active first, then recent), each
	// linking to the issue chat where the task runs.
	let { slug, tasks, failed = false }: { slug: string; tasks: AgentQueueItem[]; failed?: boolean } = $props();
</script>

<Card.Root id="tasks" class="scroll-mt-4">
	<Card.Header>
		<Card.Title class="text-sm">{m['machines.tasks_title']()}</Card.Title>
		<Card.Description>{m['machines.tasks_desc']()}</Card.Description>
	</Card.Header>
	<Card.Content>
		{#if failed && tasks.length === 0}
			<p class="text-xs text-muted-foreground">{m['machines.tasks_failed']()}</p>
		{:else if tasks.length === 0}
			<p class="text-xs text-muted-foreground">{m['machines.tasks_empty']()}</p>
		{:else}
			<ul class="divide-y rounded-lg border" aria-label={m['machines.tasks_title']()}>
				{#each tasks as task (task.id)}
					{@const tone = taskTone(task.state)}
					<li>
						<a
							href="/{slug}/assistant?conversation={encodeURIComponent(task.conversation_id)}"
							class="flex min-w-0 items-center gap-3 px-3 py-2.5 transition-colors hover:bg-muted focus-visible:bg-muted focus-visible:outline-none"
							aria-label={m['machines.task_open_chat']({ issue: `${task.issue_identifier} ${task.issue_title}`, state: taskLabel(task.state), agent: agentInfo(task.provider).name })}
						>
							<AgentLogo agent={task.provider} size="sm" />
							<span class="min-w-0 flex-1">
								<span class="block truncate text-xs font-medium"
									><span class="text-muted-foreground">{task.issue_identifier}</span> · {task.issue_title}</span
								>
								<span class="block truncate text-[11px] text-muted-foreground"
									>{agentInfo(task.provider).name} · {new Date(task.updated_at).toLocaleString()}</span
								>
							</span>
							<Badge variant="outline" class="shrink-0 {toneBadge[tone]}">{taskLabel(task.state)}</Badge>
							<ChevronRight size={14} class="shrink-0 text-muted-foreground" aria-hidden="true" />
						</a>
					</li>
				{/each}
			</ul>
		{/if}
	</Card.Content>
</Card.Root>
