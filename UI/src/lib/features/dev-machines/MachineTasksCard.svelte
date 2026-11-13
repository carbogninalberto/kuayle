<script lang="ts">
	import { untrack } from 'svelte';
	import { ChevronRight, Bot } from '@lucide/svelte';
	import * as Card from '$lib/components/ui/card';
	import { Badge } from '$lib/components/ui/badge/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Slider } from '$lib/components/ui/slider/index.js';
	import AgentLogo from '$lib/features/agents/AgentLogo.svelte';
	import { agentInfo } from '$lib/features/agents/agents';
	import { taskLabel } from '$lib/features/assistant/task-labels';
	import { taskTone, toneBadge } from '$lib/features/assistant/task-tone';
	import type { AgentQueueItem } from '$lib/api/agent-tasks';
	import { updateDevMachine } from '$lib/api/dev-machines';
	import { DEFAULT_TASK_MACHINE_MAX_TASKS, MAX_TASK_MACHINE_MAX_TASKS } from './machine-size';
	import { appToast } from '$lib/features/toast/toast';
	import RecentList from './RecentList.svelte';
	import { m } from '$lib/features/i18n/messages';
	import type { DevMachine } from '$lib/types/dev-machine';

	// The agent tasks that used this machine (active first, then recent), each
	// linking to the issue chat where the task runs. The task slot limit is the
	// machine's own override, or the workspace default when it has none; it
	// only affects where later tasks are placed, so it can change while running.
	let {
		slug,
		machineId,
		tasks,
		failed = false,
		maxTasks = null,
		workspaceLimit = DEFAULT_TASK_MACHINE_MAX_TASKS,
		canEdit = false,
		onchanged
	}: {
		slug: string;
		machineId: string;
		tasks: AgentQueueItem[];
		failed?: boolean;
		maxTasks?: number | null;
		workspaceLimit?: number;
		canEdit?: boolean;
		onchanged?: (machine: DevMachine) => void;
	} = $props();

	const effectiveLimit = $derived(maxTasks ?? workspaceLimit);
	let draft = $state(DEFAULT_TASK_MACHINE_MAX_TASKS);
	let saving = $state(false);

	// Background refreshes update the server value; keep an unsaved draft while
	// the user drags or waits to save.
	$effect(() => {
		const current = effectiveLimit;
		untrack(() => {
			draft = current;
		});
	});

	async function saveLimit(limit: number | null) {
		saving = true;
		try {
			const updated = await updateDevMachine(
				slug,
				machineId,
				limit === null ? { clear_max_tasks: true } : { max_tasks: limit }
			);
			onchanged?.(updated);
			appToast.success(m['machines.task_slots_saved']());
		} catch (error) {
			appToast.apiError(error, m['machines.task_slots_failed']());
		} finally {
			saving = false;
		}
	}
</script>

<Card.Root id="tasks" class="scroll-mt-4">
	<Card.Header>
		<Card.Title class="flex items-center gap-2 text-sm"><Bot class="size-4 text-muted-foreground" aria-hidden="true" />{m['machines.tasks_title']()}</Card.Title>
		<Card.Description>{m['machines.tasks_desc']()}</Card.Description>
	</Card.Header>
	<Card.Content class="space-y-3">
		<div class="rounded-lg border p-3" data-testid="machine-task-slots">
			<div class="flex flex-wrap items-center justify-between gap-2">
				<div class="min-w-0">
					<p class="text-xs font-medium">{m['machines.task_slots']()}</p>
					<p class="text-[11px] text-muted-foreground" data-testid="machine-task-slots-value">
						{maxTasks == null
							? m['machines.task_slots_workspace_default']({ count: effectiveLimit })
							: m['machines.task_slots_machine']({ count: effectiveLimit })}
					</p>
				</div>
				{#if canEdit && maxTasks != null}
					<Button variant="ghost" size="sm" disabled={saving} onclick={() => saveLimit(null)}>
						{m['machines.task_slots_use_default']()}
					</Button>
				{/if}
			</div>
			{#if canEdit}
				<div class="mt-3 flex items-center gap-3">
					<Slider
						type="single"
						min={1}
						max={MAX_TASK_MACHINE_MAX_TASKS}
						step={1}
						bind:value={draft}
						disabled={saving}
						class="flex-1"
						aria-label={m['machines.task_slots']()}
						aria-valuetext={m['machines.task_slots_value']({ count: draft })}
					/>
					<span class="w-6 text-right text-sm tabular-nums" aria-hidden="true">{draft}</span>
					<Button size="sm" variant="outline" disabled={saving || draft === effectiveLimit} onclick={() => saveLimit(draft)}>
						{m['machines.task_slots_save']()}
					</Button>
				</div>
			{/if}
		</div>
		{#if failed && tasks.length === 0}
			<p class="text-xs text-muted-foreground">{m['machines.tasks_failed']()}</p>
		{:else if tasks.length === 0}
			<p class="text-xs text-muted-foreground">{m['machines.tasks_empty']()}</p>
		{:else}
			<RecentList items={tasks} title={m['machines.tasks_title']()} description={m['machines.tasks_desc']()}>
				{#snippet content(shown)}
				<ul class="divide-y rounded-lg border" aria-label={m['machines.tasks_title']()}>
					{#each shown as task (task.id)}
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
				{/snippet}
			</RecentList>
		{/if}
	</Card.Content>
</Card.Root>
