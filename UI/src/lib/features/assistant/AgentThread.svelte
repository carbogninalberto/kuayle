<script lang="ts">
	import { setContext, tick, untrack } from 'svelte';
	import { ArrowLeft, ChevronDown, FileDown, RefreshCw } from '@lucide/svelte';
	import { page } from '$app/state';
	import * as Breadcrumb from '$lib/components/ui/breadcrumb/index.js';
	import { Badge } from '$lib/components/ui/badge/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import * as Collapsible from '$lib/components/ui/collapsible/index.js';
	import {
		agentTasks,
		type AgentLiveMessage as LiveMessage,
		type AgentSession,
		type AgentTask
	} from '$lib/api/agent-tasks';
	import type { ConversationState } from './conversation-state.svelte';
	import AgentStep from './AgentStep.svelte';
	import AgentLiveMessage from './AgentLiveMessage.svelte';
	import AgentReviewRequest from './AgentReviewRequest.svelte';
	import AgentReview from './AgentReview.svelte';
	import TaskNotebook from './TaskNotebook.svelte';
	import UsageLine from './UsageLine.svelte';
	import { missedNotebook } from './task-notebook';
	import TaskStopButton from './TaskStopButton.svelte';
	import { openStep } from './agent-timeline';
	import { answerContext, placeConversation, sendOptions, type AnswerContext } from './agent-conversation';
	import { reviewSessions } from './agent-review';
	import { placeByTime } from './shell-rules';
	import { useTaskRun } from './task-run.svelte';
	import { useMessageScroller } from '$lib/components/ui/message-scroller/index.js';
	import { authState } from '$lib/features/auth/auth.state.svelte';
	import { taskLabel } from './task-labels';
	import { taskTone, toneBadge } from './task-tone';
	import { downloadTaskTrace } from './task-trace';
	import { m } from '$lib/features/i18n/messages';

	// One coding task as the centre conversation: where it is (a breadcrumb
	// back to the assistant), what was asked, and its timeline: one section
	// per workflow step, in the order the task took them, the running one
	// open and streaming. A task without a workflow is one step per attempt.
	// The conversation with the agent lives in it: a message sits in the run
	// that received it, at the moment it was delivered, with the agent's
	// reply as the next item; one that waits while no step runs follows the
	// last step, where the next iteration picks it up. It is written in the
	// composer under the view (the shared chat composer). A review of the task's pull
	// request is asked for from its header and appears in the timeline.
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
	let traceError = $state('');
	let traceBusy = $state(false);
	let detailsOpen = $state(false);
	// A refused stop is about the state the task was in.
	$effect(() => {
		void task.state;
		stopError = '';
		traceError = '';
	});
	async function downloadTrace() {
		if (traceBusy) return;
		traceBusy = true;
		traceError = '';
		try {
			await downloadTaskTrace(chat.slug, task.id, identifier);
		} catch {
			if (!disposed) traceError = m['tasks.load_error']();
		} finally {
			if (!disposed) traceBusy = false;
		}
	}

	let disposed = false;
	$effect(() => () => {
		disposed = true;
	});

	// What the task did is loaded once for this view, the sidebar's path and
	// the workflow chart; it reloads on the task's events and is polled as a
	// fallback (see TaskRun).
	// The notebook and a run's prompt are read by the task's creator alone; a
	// review has no notebook.
	const mine = $derived(task.creator_id === authState.user?.id);
	const run = useTaskRun(
		untrack(() => chat),
		() => task,
		untrack(() => ({ notebook: mine && task.kind !== 'review' }))
	);
	const steps = $derived(run.steps);
	const missed = $derived(missedNotebook(run.notebook));
	let notebookOpen = $state(false);
	// The transcript follows its live end while the reader is there, and a
	// short one is at its end and its start at once: the opened notebook
	// would send the reader below it. Opening it is reading from the top.
	const scroller = useMessageScroller();
	const personName = (id: string | undefined) => (id && id === authState.user?.id ? authState.user.display_name || authState.user.name : '');
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
		const cursor = chat.taskLiveCursor[task.id];
		void task.id;
		if (task.kind === 'review') return;
		untrack(() => {
			void loadLive();
			// A message to the agent is an instruction in its notebook.
			if (cursor !== undefined) void run.loadNotebook();
		});
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
	const current = $derived(openStep(steps.filter((step) => !step.reviewTaskId)));
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
		let stale = false;
		untrack(() => {
			agentTasks
				.sessions(chat.slug, task.id)
				.then((view) => {
					if (!stale && !disposed) sessions = view.sessions ?? [];
				})
				.catch(() => {
					/* The timeline still shows the task's own work. */
				});
		});
		return () => (stale = true);
	});
	const reviews = $derived(reviewSessions(sessions));
	const reviewsByTask = $derived(new Map(reviews.map((session) => [session.task_id, session])));
	const timeline = $derived.by(() => {
		const linked = new Set(steps.map((step) => step.reviewTaskId));
		// Workflow sequence is authoritative. Place independently requested reviews by time.
		return placeByTime(
			steps.map((step) => ({ id: step.anchor, created_at: step.startedAt })),
			reviews.filter((session) => !linked.has(session.task_id))
		);
	});
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
		const url = new URL(page.url.href);
		url.searchParams.set('agent', id);
		url.hash = '';
		return `${url.pathname}${url.search}`;
	};
</script>

{#snippet reviewRow(session: (typeof reviews)[number])}
	<li class="min-w-0"><AgentReview {session} /></li>
{/snippet}

<Collapsible.Root
	bind:open={detailsOpen}
	onOpenChange={(open) => {
		if (open) scroller.scrollToStart({ behavior: 'auto' });
	}}
	class="flex min-w-0 flex-col gap-3"
	data-testid="agent-thread"
>
	<header class="@container sticky top-0 z-10 border-b bg-background py-1.5" data-testid="agent-thread-header">
		<div class="flex min-w-0 items-center gap-1.5 @min-[36rem]:gap-2">
			<Breadcrumb.Root class="shrink-0" aria-label={m['assistant.agent_view_breadcrumb']()}>
				<Breadcrumb.List class="flex-nowrap gap-1 text-xs">
					<Breadcrumb.Item>
						<Button
							variant="ghost"
							size="icon-sm"
							href={backHref}
							aria-label={m['assistant.title']()}
							title={m['assistant.target_back']()}
							onclick={(event) => {
								if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey || event.button !== 0) return;
								event.preventDefault();
								onback();
							}}><ArrowLeft /></Button
						>
					</Breadcrumb.Item>
					<Breadcrumb.Item class="hidden @min-[40rem]:inline-flex">
						<Breadcrumb.Page class="whitespace-nowrap text-muted-foreground"
							>{identifier} · {m['tasks.attempt']({ number: task.generation })}</Breadcrumb.Page
						>
					</Breadcrumb.Item>
				</Breadcrumb.List>
			</Breadcrumb.Root>
			<div class="flex min-w-0 flex-1 items-center gap-1">
				<p class="min-w-0 flex-1 truncate text-sm font-medium">{task.prompt.split('\n', 1)[0]}</p>
				<Collapsible.Trigger
					class="group inline-flex size-7 shrink-0 cursor-pointer items-center justify-center rounded-md text-muted-foreground outline-none hover:bg-muted focus-visible:ring-3 focus-visible:ring-ring/50"
					aria-label={detailsOpen ? m['tasks.transcript_show_less']() : m['sharedComponents.create_issue.expand']()}
					title={m['tasks.details']()}
				><ChevronDown size={14} aria-hidden="true" class="transition-transform group-aria-expanded:rotate-180 motion-reduce:transition-none" /></Collapsible.Trigger>
			</div>
			<div class="flex shrink-0 items-center gap-1 @min-[36rem]:gap-1.5">
				<AgentReviewRequest {chat} {task} {allowedAgents} onrequest={requestReview} />
				<Button variant="outline" size="sm" class="@max-[36rem]:w-7 @max-[36rem]:px-0" disabled={traceBusy}
					aria-label={m['tasks.download_trace']()} title={m['tasks.download_trace']()} onclick={() => void downloadTrace()}
					><FileDown /><span class="hidden @min-[36rem]:inline">{m['tasks.download_trace']()}</span></Button
				>
				<TaskStopButton {chat} {task} {identifier} onerror={(message) => (stopError = message)} />
			</div>
			<Badge variant="outline" class="max-w-24 shrink-0 @min-[36rem]:max-w-40 {toneBadge[tone]}"
				title={taskLabel(task.state)} role="status"><span class="truncate">{taskLabel(task.state)}</span></Badge>
		</div>
	</header>

	{#if stopError}<p role="alert" class="text-sm break-words text-destructive">{stopError}</p>{/if}
	{#if traceError}<p role="alert" class="text-sm break-words text-destructive">{traceError}</p>{/if}

	<section aria-label={m['tasks.details']()} class="min-w-0">
		<Collapsible.Content class="space-y-2 pb-2">
			{#if detailsOpen}
				<p class="text-xs text-muted-foreground">{identifier} · {m['tasks.attempt']({ number: task.generation })}</p>
				<p class="break-words whitespace-pre-wrap text-sm">{task.prompt}</p>
				{#if task.reason}<p class="break-words text-xs text-muted-foreground">{task.reason}</p>{/if}
				<UsageLine usage={run.usage} label={m['tasks.usage_total']()} />
			{/if}
		</Collapsible.Content>
		{#if run.notebook}<TaskNotebook view={run.notebook} request={task.prompt} name={personName} bind:open={notebookOpen} onopened={() => scroller.scrollToStart({ behavior: 'auto' })} />{/if}
	</section>

	<ol aria-label={m['workflows.task.path']()} class="flex min-w-0 flex-col gap-1">
		{#each steps as step (step.anchor)}
			{#each timeline.before[step.anchor] ?? [] as session (session.id)}{@render reviewRow(session)}{/each}
			<li class="min-w-0">
				<AgentStep
					{chat}
					{step}
					{now}
					reviewHref={step.reviewTaskId ? reviewHref(step.reviewTaskId) : undefined}
					reviewSession={step.reviewTaskId ? reviewsByTask.get(step.reviewTaskId) : undefined}
					{conversation}
					promptable={mine}
					{missed}
					bind:open={() => chosen[step.anchor] ?? step.anchor === current, (open) => (chosen[step.anchor] = open)}
				/>
			</li>
		{/each}
		{#each timeline.after as session (session.id)}{@render reviewRow(session)}{/each}
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
</Collapsible.Root>
