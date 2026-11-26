<script lang="ts">
	import type { Snippet } from 'svelte';
	import { Button } from '$lib/components/ui/button/index.js';
	import type { AgentTask, TaskMessage } from '$lib/api/agent-tasks';
	import type { PipelineDefinition, TaskWorkflow } from '$lib/api/workflows';
	import { taskLabel } from '$lib/features/assistant/task-labels';
	import { turnSummary } from '$lib/features/assistant/thread-rows';
	import { m } from '$lib/features/i18n/messages';
	import { roomBlocker } from '../assistant/task-room';
	import { statusLabel } from './labels';
	import { ownerLabel } from './pipeline-labels';
	import { pipelineRunMembers } from './pipeline-run-members';

	let { task, run, pipeline, room, onopenworkflow, children }: {
		task: AgentTask;
		run: TaskWorkflow;
		pipeline: PipelineDefinition;
		room: readonly TaskMessage[];
		onopenworkflow: () => void;
		children: Snippet;
	} = $props();

	const status = $derived.by(() => {
		if (task.workflow_paused_at || run.paused_at || roomBlocker(task)) return m['tasks.state.paused']();
		if (run.status === 'paused') return statusLabel(run.status);
		if (task.state === 'blocked' || run.task_state === 'blocked') return m['tasks.state.blocked']();
		return statusLabel(run.status) ?? taskLabel(task.state);
	});
	const phase = $derived(pipeline.phases.find((phase) => phase.id === run.node)?.label || run.node);
	const name = $derived(`${run.name} v${run.version}`);
	const iteration = $derived(m['tasks.attempt']({ number: run.iterations }));
	const summary = $derived([name, status, phase, iteration].filter(Boolean).join(' · '));
	// Earlier runs may only identify their role in a room post, or as the root task's writer.
	const runs = $derived.by(() => {
		const roles = new Map(room.filter((post) => post.author_kind === 'member' && post.run_id).map((post) => [post.run_id, post.author_role]));
		const writer = pipeline.roster.find((member) => member.writer)?.role;
		return (run.runs ?? []).map((item) => ({
			...item, role: item.role ?? roles.get(item.id) ?? (item.task_id === task.id ? writer : undefined)
		}));
	});
	const members = $derived(pipelineRunMembers(pipeline, runs));
</script>

<Button
	id="task-workflow-{task.id}"
	variant="link"
	size="sm"
	class="max-w-full justify-start gap-1 px-0 text-[11px] text-foreground underline decoration-muted-foreground/50 underline-offset-4 sm:text-xs"
	title={summary}
	aria-label={`${summary} · ${m['workflows.open_named']({ name: m['assistant.panel_workflow']() })}`}
	onclick={onopenworkflow}
>
	<span class="truncate">{run.name}</span>
	<span>v{run.version} · {status}</span>
	{#if phase}<span class="truncate">· {phase}</span>{/if}
	<span>· {iteration}</span>
</Button>

{@render children()}

<section class="space-y-1.5" aria-label={m['common.members']()}>
	<h4 class="text-xs font-medium text-muted-foreground">{m['common.members']()}</h4>
	<ul class="space-y-2">
		{#each members as member (member.role)}
			{@const last = runs.findLast((item) => item.role === member.role)}
			{@const waiting = last?.status === 'waiting_input' || last?.status === 'queued'}
			{@const working = member.working || last?.status === 'starting'}
			{@const output = turnSummary(last, room.filter((post) => post.author_role === member.role && (!last || post.run_id === last.id)))}
			<li data-testid="run-member-{member.role}">
				<div class="flex items-baseline justify-between gap-2">
					<span class="min-w-0 break-all text-sm font-medium">{ownerLabel(member.role)}</span>
					<span class="shrink-0 text-xs {working ? 'text-[var(--status-running)]' : 'text-muted-foreground'}">
						{waiting ? m['tasks.check_waiting']() : working ? m['tasks.member_working']() : m['tasks.member_idle']()}
					</span>
				</div>
				{#if output}<p class="text-xs break-words text-muted-foreground" data-selectable="text">{output}</p>{/if}
			</li>
		{/each}
	</ul>
</section>
