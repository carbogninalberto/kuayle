<script lang="ts">
	import { untrack } from 'svelte';
	import { Circle, LoaderCircle, PanelLeftClose, RefreshCw } from '@lucide/svelte';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Progress } from '$lib/components/ui/progress/index.js';
	import AgentLogo from '$lib/features/agents/AgentLogo.svelte';
	import type { AgentTask } from '$lib/api/agent-tasks';
	import type { ConversationState } from './conversation-state.svelte';
	import StepStatus from './StepStatus.svelte';
	import TaskPath from './TaskPath.svelte';
	import UsageLine from './UsageLine.svelte';
	import { usageParts } from './run-usage';
	import { budgetPercent, budgetUse, pipelineMembers, pipelinePhases, type BudgetUse } from './task-column';
	import { useTaskRun } from './task-run.svelte';
	import { getLocale, m } from '$lib/features/i18n/messages';

	// The task column of the agent view (KUA-281): where the task is, who
	// works on it and what it used, beside the room. A task on a pipeline
	// shows its phases with the current one and what it waits for, and its
	// roster with who works now; any other task shows the steps it took (its
	// path). The budget is the turns or iterations used of the workflow's
	// limit and what the runs cost. It reads the task's shared run
	// (useTaskRun), so it adds no request of its own. Mounted per task.
	let { chat, task, onclose }: { chat: ConversationState; task: AgentTask; onclose: () => void } = $props();
	const uid = $props.id();

	const run = useTaskRun(untrack(() => chat), () => task);
	const pipeline = $derived(run.pipeline);
	const phases = $derived(pipeline ? pipelinePhases(pipeline, run.view, task) : []);
	const members = $derived(
		pipeline ? pipelineMembers(pipeline, run.view?.runs, run.view?.profiles ?? [], task) : []
	);
	const budget = $derived(
		budgetUse(run.followsWorkflow ? run.view : null, pipeline, task.workflow_budget_extensions ?? 0)
	);
	const spent = $derived(usageParts(run.usage, getLocale()).length > 0);
	const name = $derived(run.view && run.followsWorkflow ? (run.view.builtin ? m['workflows.standard']() : run.view.name) : '');

	const budgetText = (use: BudgetUse) =>
		use.max
			? (use.unit === 'turns' ? m['tasks.budget_turns'] : m['tasks.budget_iterations'])({ used: use.used, max: use.max })
			: use.unit === 'turns'
				? m['tasks.member_turns']({ count: use.used })
				: m['workflows.task.iterations']({ count: use.used });
	const heading = 'text-xs font-medium text-muted-foreground';
</script>

<aside class="flex h-full min-h-0 min-w-0 flex-col bg-background" aria-labelledby="{uid}-title" data-testid="task-column">
	<div class="flex h-12 shrink-0 items-center gap-1 border-b ps-3 pe-2">
		<h2 id="{uid}-title" class="min-w-0 flex-1 truncate text-sm font-medium">{m['assistant.task_column']()}</h2>
		<Button
			variant="ghost"
			size="icon-sm"
			aria-label={m['assistant.task_column_hide']()}
			title={m['assistant.task_column_hide']()}
			onclick={onclose}><PanelLeftClose /></Button
		>
	</div>
	<div class="min-h-0 flex-1 space-y-5 overflow-y-auto px-3 py-3" data-testid="task-column-scroll">
		<section class="min-w-0 space-y-2" aria-labelledby="{uid}-where">
			<div class="flex min-w-0 items-baseline gap-2">
				<h3 id="{uid}-where" class={heading}>
					{pipeline || (task.pipeline && !run.view) ? m['workflows.pipeline_phases']() : m['workflows.task.path']()}
				</h3>
				{#if name}<span class="min-w-0 truncate text-xs text-muted-foreground/80">{name}</span>{/if}
			</div>
			{#if pipeline}
				<ol aria-label={m['workflows.pipeline_phases']()} class="min-w-0">
					{#each phases as phase, index (phase.id)}
						{@const last = index === phases.length - 1}
						<li
							class="relative min-w-0 pl-6 {last ? '' : 'pb-2'}"
							aria-current={phase.state === 'current' ? 'step' : undefined}
							data-testid="task-phase-{phase.id}"
						>
							<!-- The line the phases sit on, as on a task's path. -->
							{#if phases.length > 1}<span
									aria-hidden="true"
									class="absolute left-[7px] w-px bg-border {index === 0 ? 'top-2.5' : 'top-0'} {last ? 'h-2.5' : 'bottom-0'}"
								></span>{/if}
							<span class="absolute top-[2.5px] left-0 inline-flex rounded-full bg-background">
								{#if phase.state === 'passed'}<StepStatus status="done" />{:else if phase.status}<StepStatus
										status={phase.status}
									/>{:else}<Circle size={15} aria-hidden="true" class="text-muted-foreground/50" />{/if}
							</span>
							<p class="flex min-w-0 items-baseline gap-1.5 text-[13px] leading-5">
								<span
									class="min-w-0 truncate {phase.state === 'current'
										? 'font-medium'
										: phase.state === 'ahead'
											? 'text-muted-foreground'
											: ''}">{phase.label}</span
								>
								<span class="shrink-0 text-xs text-muted-foreground">{phase.owner}</span>
							</p>
							{#if phase.state === 'current' && task.reason}<p
									class="line-clamp-3 text-xs leading-4 break-words text-muted-foreground"
								>
									{task.reason}
								</p>{/if}
						</li>
					{/each}
				</ol>
			{:else if task.pipeline && run.failed}
				<div class="flex items-center gap-1" role="alert">
					<p class="min-w-0 flex-1 text-xs text-destructive">{m['workflows.task.load_failed']()}</p>
					<Button
						variant="ghost"
						size="icon-xs"
						aria-label={m['tasks.refresh']()}
						title={m['tasks.refresh']()}
						onclick={() => void run.load()}><RefreshCw /></Button
					>
				</div>
			{:else if task.pipeline && !run.view}
				<p class="text-xs text-muted-foreground" role="status">{m['assistant.loading']()}</p>
			{:else}
				<TaskPath {chat} {task} />
			{/if}
		</section>

		{#if members.length}
			<section class="min-w-0 space-y-2" aria-labelledby="{uid}-members">
				<h3 id="{uid}-members" class={heading}>{m['tasks.column_members']()}</h3>
				<ul aria-label={m['tasks.column_members']()} class="min-w-0 space-y-2">
					{#each members as member (member.role)}
						<li class="flex min-w-0 items-center gap-2" data-testid="task-member-{member.role}">
							{#if member.harness}<AgentLogo agent={member.harness} size="sm" />{:else}<span
									aria-hidden="true"
									class="size-5 shrink-0 rounded border bg-muted/40"
								></span>{/if}
							<div class="min-w-0 flex-1">
								<p class="flex min-w-0 items-baseline gap-1.5 text-[13px] leading-5">
									<span class="min-w-0 truncate font-medium">{member.role}</span>
									{#if member.profile}<span class="min-w-0 truncate text-xs text-muted-foreground">{member.profile}</span>{/if}
								</p>
								<p class="truncate text-xs leading-4 text-muted-foreground tabular-nums">
									{m['tasks.member_turns']({ count: member.turns })}{#if member.writer}{` · ${m['workflows.pipeline_writer']()}`}{/if}
								</p>
							</div>
							{#if member.working}<span class="inline-flex shrink-0 items-center gap-1 text-xs text-[var(--status-running)]"
									><LoaderCircle size={12} aria-hidden="true" class="animate-spin motion-reduce:animate-none" />{m[
										'tasks.member_working'
									]()}</span
								>{:else}<span class="shrink-0 text-xs text-muted-foreground">{m['tasks.member_idle']()}</span>{/if}
						</li>
					{/each}
				</ul>
			</section>
		{/if}

		{#if budget || spent}
			<section class="min-w-0 space-y-1.5" aria-labelledby="{uid}-budget" data-testid="task-column-budget">
				<h3 id="{uid}-budget" class={heading}>{m['tasks.column_budget']()}</h3>
				{#if budget}
					{@const text = budgetText(budget)}
					{@const percent = budgetPercent(budget)}
					<p class="text-[13px] leading-5 tabular-nums">{text}</p>
					{#if percent !== null}<Progress value={percent} aria-label={text} />{/if}
				{/if}
				<UsageLine usage={run.usage} label={m['tasks.usage_total']()} />
			</section>
		{/if}
	</div>
</aside>
