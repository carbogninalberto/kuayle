<script lang="ts">
	import { tick, untrack } from 'svelte';
	import { RefreshCw } from '@lucide/svelte';
	import { page } from '$app/state';
	import * as Breadcrumb from '$lib/components/ui/breadcrumb/index.js';
	import { Badge } from '$lib/components/ui/badge/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { agentTasks, type AgentTask } from '$lib/api/agent-tasks';
	import { workflows, type TaskWorkflow } from '$lib/api/workflows';
	import type { AgentRun } from '$lib/types/dev-machine';
	import type { ConversationState } from './conversation-state.svelte';
	import AgentStep from './AgentStep.svelte';
	import ClampedText from './ClampedText.svelte';
	import TaskLiveThread from './TaskLiveThread.svelte';
	import { attemptTimeline, openStep, workflowTimeline } from './agent-timeline';
	import { taskLabel } from './task-labels';
	import { taskTone, toneBadge } from './task-tone';
	import { m } from '$lib/features/i18n/messages';

	// One coding task as the centre conversation: where it is (a breadcrumb
	// back to the assistant), what was asked, and its timeline: one section
	// per workflow step, in the order the task took them, the running one
	// open and streaming. A task without a workflow is one step per attempt.
	// Below it, the live thread with the agent (its questions, your replies);
	// replies are written in the page's one composer, which aims at this
	// agent while this view is open.
	let {
		chat,
		task,
		backHref,
		onback
	}: {
		chat: ConversationState;
		task: AgentTask;
		backHref: string;
		onback: () => void;
	} = $props();
	const identifier = $derived(chat.issue?.identifier ?? '');
	const tone = $derived(taskTone(task.state));
	const followsWorkflow = $derived(!!task.workflow_version_id);

	let view = $state<TaskWorkflow | null>(null),
		attempts = $state<AgentRun[]>([]),
		failed = $state(false);
	let request = 0,
		disposed = false;
	// A finished run's summary is final: the view keeps the ones it has and
	// asks only for those of runs finished since (knownUntil).
	const summaries = new Map<string, string>();
	let knownUntil = '';
	$effect(() => () => {
		disposed = true;
	});
	async function load() {
		const current = ++request;
		try {
			if (followsWorkflow) {
				const loaded = await workflows.task(chat.slug, task.id, knownUntil || undefined);
				if (disposed || current !== request) return;
				const runs = (loaded.runs ?? []).map((run) => {
					if (run.summary) summaries.set(run.id, run.summary);
					if (run.completed_at && (!knownUntil || Date.parse(run.completed_at) > Date.parse(knownUntil)))
						knownUntil = run.completed_at;
					return run.summary || !summaries.has(run.id) ? run : { ...run, summary: summaries.get(run.id) };
				});
				view = { ...loaded, runs };
			} else {
				const loaded = await agentTasks.runs(chat.slug, task.id);
				if (disposed || current !== request) return;
				attempts = loaded.runs;
			}
			failed = false;
		} catch {
			if (!disposed && current === request) failed = true;
		}
	}
	// The task moving (state, attempt, workflow step) or one of its reviews
	// changing arrives as an event and reloads the steps; each open step
	// streams its own output.
	$effect(() => {
		void [
			task.id,
			task.state,
			task.run_id,
			task.generation,
			task.workflow_node,
			task.workflow_status,
			task.updated_at,
			chat.taskWorkflowCursor[task.id],
			chat.taskReviewCursor[task.id]
		];
		untrack(() => void load());
	});

	const steps = $derived(
		followsWorkflow ? (view ? workflowTimeline(view, task) : []) : attemptTimeline(task, attempts)
	);
	// Events can be missed (a dropped connection), so the workflow is also
	// polled: modestly while a step runs, rarely while it waits (an approval
	// can take days), never once it has ended.
	const pace = $derived(
		!followsWorkflow || !view || view.status === 'done' || view.status === 'failed'
			? 0
			: steps.some((step) => step.status === 'running')
				? 15_000
				: 120_000
	);
	$effect(() => {
		if (!pace) return;
		const timer = setInterval(() => void load(), pace);
		return () => clearInterval(timer);
	});
	// The running step is open and the finished ones are closed, until the
	// reader opens or closes one; a link to a step (#step-4) opens it.
	const linked = untrack(() => page.url.hash.slice(1));
	let chosen = $state<Record<string, boolean>>(linked ? { [linked]: true } : {});
	const current = $derived(openStep(steps));
	let scrolled = false;
	$effect(() => {
		if (scrolled || !linked || !steps.some((step) => step.anchor === linked)) return;
		scrolled = true;
		void tick().then(() => document.getElementById(linked)?.scrollIntoView({ block: 'start' }));
	});

	// Durations of running steps tick.
	let now = $state(Date.now());
	const ticking = $derived(steps.some((step) => step.status === 'running'));
	$effect(() => {
		if (!ticking) return;
		now = Date.now();
		const timer = setInterval(() => (now = Date.now()), 1000);
		return () => clearInterval(timer);
	});

	const reviewHref = (id: string) => {
		const url = new URL(page.url);
		url.searchParams.set('agent', id);
		url.hash = '';
		return `${url.pathname}${url.search}`;
	};
</script>

<div class="flex min-w-0 flex-col gap-5" data-testid="agent-thread">
	<Breadcrumb.Root aria-label={m['assistant.agent_view_breadcrumb']()}>
		<Breadcrumb.List>
			<Breadcrumb.Item>
				<Breadcrumb.Link
					href={backHref}
					onclick={(event) => {
						if (event.metaKey || event.ctrlKey || event.shiftKey || event.button !== 0) return;
						event.preventDefault();
						onback();
					}}>{m['assistant.title']()}</Breadcrumb.Link
				>
			</Breadcrumb.Item>
			<Breadcrumb.Separator />
			<Breadcrumb.Item>
				<Breadcrumb.Page>{identifier} · {m['tasks.attempt']({ number: task.generation })}</Breadcrumb.Page>
			</Breadcrumb.Item>
		</Breadcrumb.List>
	</Breadcrumb.Root>

	<div class="min-w-0 space-y-1">
		<div class="flex min-w-0 items-start justify-between gap-3">
			<ClampedText text={task.prompt} class="text-sm font-medium" />
			<Badge variant="outline" class="shrink-0 {toneBadge[tone]}" role="status">{taskLabel(task.state)}</Badge>
		</div>
		{#if task.reason}<p class="text-xs break-words text-muted-foreground">{task.reason}</p>{/if}
	</div>

	<ol aria-label={m['workflows.task.path']()} class="flex min-w-0 flex-col gap-1">
		{#each steps as step (step.anchor)}
			<li class="min-w-0">
				<AgentStep
					{chat}
					{step}
					{now}
					reviewHref={step.reviewTaskId ? reviewHref(step.reviewTaskId) : undefined}
					bind:open={() => chosen[step.anchor] ?? step.anchor === current, (open) => (chosen[step.anchor] = open)}
				/>
			</li>
		{/each}
	</ol>
	{#if failed}<div class="flex flex-wrap items-center gap-2">
			<p role="alert" class="text-sm text-destructive">
				{followsWorkflow ? m['workflows.task.load_failed']() : m['tasks.load_error']()}
			</p>
			<Button variant="ghost" size="sm" onclick={() => void load()}><RefreshCw />{m['tasks.refresh']()}</Button>
		</div>{/if}

	{#if task.kind !== 'review'}<TaskLiveThread {chat} {task} />{/if}
</div>
