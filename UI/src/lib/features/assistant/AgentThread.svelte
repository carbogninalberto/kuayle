<script lang="ts">
	import { setContext, tick, untrack } from 'svelte';
	import { RefreshCw } from '@lucide/svelte';
	import { page } from '$app/state';
	import * as Breadcrumb from '$lib/components/ui/breadcrumb/index.js';
	import { Badge } from '$lib/components/ui/badge/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import {
		agentTasks,
		type AgentLiveMessage as LiveMessage,
		type AgentSession,
		type AgentTask
	} from '$lib/api/agent-tasks';
	import type { ConversationState } from './conversation-state.svelte';
	import AgentStep from './AgentStep.svelte';
	import ClampedText from './ClampedText.svelte';
	import AgentLiveMessage from './AgentLiveMessage.svelte';
	import AgentReviewRequest from './AgentReviewRequest.svelte';
	import AgentReviews from './AgentReviews.svelte';
	import TaskStopButton from './TaskStopButton.svelte';
	import { openStep } from './agent-timeline';
	import { answerContext, placeConversation, sendOptions, type AnswerContext } from './agent-conversation';
	import { reviewSessions } from './agent-review';
	import { useTaskRun } from './task-run.svelte';
	import { authState } from '$lib/features/auth/auth.state.svelte';
	import { taskLabel } from './task-labels';
	import { taskTone, toneBadge } from './task-tone';
	import { m } from '$lib/features/i18n/messages';

	// One coding task as the centre conversation: where it is (a breadcrumb
	// back to the assistant), what was asked, and its timeline: one section
	// per workflow step, in the order the task took them, the running one
	// open and streaming. A task without a workflow is one step per attempt.
	// The conversation with the agent lives in it: a message sits in the run
	// that received it, at the moment it was delivered, with the agent's
	// reply as the next item; one that waits while no step runs follows the
	// last step, where the next iteration picks it up. It is written in the
	// composer under the view (AgentComposer). A review of the task's pull
	// request is asked for from its header, and the requested reviews are
	// read under the timeline.
	let {
		chat,
		task,
		backHref,
		onback,
		allowedAgents = []
	}: {
		chat: ConversationState;
		task: AgentTask;
		backHref: string;
		onback: () => void;
		// Coding agents the workspace allows, offered for a requested review.
		allowedAgents?: string[];
	} = $props();
	const identifier = $derived(chat.issue?.identifier ?? '');
	const tone = $derived(taskTone(task.state));
	let stopError = $state('');
	// A refused stop is about the state the task was in.
	$effect(() => {
		void task.state;
		stopError = '';
	});

	let disposed = false;
	$effect(() => () => {
		disposed = true;
	});

	// What the task did is loaded once for this view, the sidebar's path and
	// the workflow chart; it reloads on the task's events and is polled as a
	// fallback (see TaskRun).
	const run = useTaskRun(untrack(() => chat), () => task);
	const steps = $derived(run.steps);
	// The task's live channel, reloaded on each of its events. A review has
	// none; another member's task answers "not found" and shows nothing.
	let live = $state<LiveMessage[]>([]);
	let liveRequest = 0;
	async function loadLive() {
		const current = ++liveRequest;
		try {
			const result = await agentTasks.liveMessages(chat.slug, task.id);
			if (!disposed && current === liveRequest) live = result.messages ?? [];
		} catch {
			/* The timeline still shows the agent's work. */
		}
	}
	$effect(() => {
		void [task.id, chat.taskLiveCursor[task.id]];
		if (task.kind !== 'review') untrack(() => void loadLive());
	});
	const conversation = $derived(placeConversation(live, steps, task));
	// An agent's question is answered in the composer under the view: it
	// takes the question as what the next message answers, and the cursor.
	const writable = $derived(sendOptions(task, authState.user?.id).open);
	setContext<AnswerContext>(answerContext, () =>
		writable
			? (question) => {
					chat.agentQuestion = { taskId: task.id, id: question.id, body: question.body };
					chat.composerFocus++;
				}
			: undefined
	);
	// The running step is open and the finished ones are closed, until the
	// reader opens or closes one; a link to a step (#step-4) opens it.
	let chosen = $state<Record<string, boolean>>({});
	const current = $derived(openStep(steps));
	let followed = '';
	$effect(() => {
		const linked = page.url.hash.slice(1);
		if (!linked || linked === followed || !steps.some((step) => step.anchor === linked)) return;
		followed = linked;
		untrack(() => (chosen[linked] = true));
		void tick().then(() => document.getElementById(linked)?.scrollIntoView({ block: 'start' }));
	});

	// The reviews asked of the task's pull request; their changes arrive as
	// task.review_updated events.
	let sessions = $state<AgentSession[]>([]);
	$effect(() => {
		void [task.id, task.state, chat.taskReviewCursor[task.id]];
		if (task.kind === 'review') return;
		untrack(() => {
			agentTasks
				.sessions(chat.slug, task.id)
				.then((view) => {
					if (!disposed) sessions = view.sessions ?? [];
				})
				.catch(() => {
					/* The timeline still shows the task's own work. */
				});
		});
	});
	const reviews = $derived(reviewSessions(sessions));
	// The request is written for the assistant, which asks for approval.
	function requestReview(text: string) {
		chat.composerDraft = text;
		onback();
	}

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
			<div class="flex shrink-0 items-center gap-2">
				<AgentReviewRequest {chat} {task} {allowedAgents} onrequest={requestReview} />
				<TaskStopButton {chat} {task} {identifier} onerror={(message) => (stopError = message)} />
				<Badge variant="outline" class={toneBadge[tone]} role="status">{taskLabel(task.state)}</Badge>
			</div>
		</div>
		{#if task.reason}<p class="text-xs break-words text-muted-foreground">{task.reason}</p>{/if}
		{#if stopError}<p role="alert" class="text-sm break-words text-destructive">{stopError}</p>{/if}
	</div>

	<ol aria-label={m['workflows.task.path']()} class="flex min-w-0 flex-col gap-1">
		{#each steps as step (step.anchor)}
			<li class="min-w-0">
				<AgentStep
					{chat}
					{step}
					{now}
					reviewHref={step.reviewTaskId ? reviewHref(step.reviewTaskId) : undefined}
					{conversation}
					bind:open={() => chosen[step.anchor] ?? step.anchor === current, (open) => (chosen[step.anchor] = open)}
				/>
			</li>
		{/each}
		<!-- Waiting with no step running: the next iteration picks these up here. -->
		{#if conversation.pending.length && steps.length}<li class="flex min-w-0 flex-col gap-3 py-2" data-step="pending">
				{#each conversation.pending as placed (placed.message.id)}<AgentLiveMessage {placed} />{/each}
			</li>{/if}
	</ol>
	{#if run.failed}<div class="flex flex-wrap items-center gap-2">
			<p role="alert" class="text-sm text-destructive">
				{run.followsWorkflow ? m['workflows.task.load_failed']() : m['tasks.load_error']()}
			</p>
			<Button variant="ghost" size="sm" onclick={() => void run.load()}><RefreshCw />{m['tasks.refresh']()}</Button>
		</div>{/if}
	<AgentReviews {reviews} />
</div>
