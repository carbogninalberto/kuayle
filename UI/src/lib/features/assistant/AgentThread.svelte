<script lang="ts">
	import { setContext, tick, untrack } from 'svelte';
	import { ArrowLeft, ChevronDown, ChevronRight, Gavel, Layers, LoaderCircle, RefreshCw } from '@lucide/svelte';
	import { page } from '$app/state';
	import * as Breadcrumb from '$lib/components/ui/breadcrumb/index.js';
	import { Badge } from '$lib/components/ui/badge/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import * as Collapsible from '$lib/components/ui/collapsible/index.js';
	import { Toggle } from '$lib/components/ui/toggle/index.js';
	import SelectField from '$lib/components/shared/SelectField.svelte';
	import {
		agentTasks,
		type AgentLiveMessage as LiveMessage,
		type AgentSession,
		type AgentTask
	} from '$lib/api/agent-tasks';
	import type { ConversationState } from './conversation-state.svelte';
	import AgentStep from './AgentStep.svelte';
	import AgentLiveMessage from './AgentLiveMessage.svelte';
	import AgentReview from './AgentReview.svelte';
	import StepStatus from './StepStatus.svelte';
	import OwnerDecisionCard from './OwnerDecisionCard.svelte';
	import { decisionAnchor, roomBlocker } from './task-room';
	import TaskRoom from './TaskRoom.svelte';
	import TaskThreadMenu from './TaskThreadMenu.svelte';
	import UsageLine from './UsageLine.svelte';
	import { missedNotebook } from './task-notebook';
	import TaskStopButton from './TaskStopButton.svelte';
	import { openStep, type TimelineStep } from './agent-timeline';
	import { answerContext, placeConversation, sendOptions, type AnswerContext } from './agent-conversation';
	import { reviewSessions } from './agent-review';
	import { placeByTime } from './shell-rules';
	import { useTaskRun } from './task-run.svelte';
	import { useMessageScroller } from '$lib/components/ui/message-scroller/index.js';
	import { pipelineMembers, pipelinePhases } from './task-column';
	import { shownRows, stepThread, threadRoles, type StepRow, type ThreadFilter } from './thread-rows';
	import { stepAgent, stepTitle } from './step-labels';
	import { authState } from '$lib/features/auth/auth.state.svelte';
	import { decisionWaitLabel, taskLabel } from './task-labels';
	import { taskTone, toneBadge } from './task-tone';
	import { downloadTaskTrace } from './task-trace';
	import { m } from '$lib/features/i18n/messages';

	// One coding task as the centre conversation, one line per event
	// (KUA-286). The header says where it is (a breadcrumb back to the
	// assistant, the issue), what was asked, its phase, how many work on it
	// now, and stops it; the trace and a review request are in its menu. A
	// task that follows a workflow is filtered by role, or to its decisions.
	// A task on a pipeline shows its room (TaskRoom), and the composer writes
	// to it. Any other task shows its timeline: one line per workflow step in
	// the order the task took them, the running one open and streaming, and
	// the steps Kuayle ran in a row folded into one line; a task without a
	// workflow is one line per attempt. The conversation with the agent lives
	// in it: a message sits in the run that received it, at the moment it
	// was delivered, with the agent's reply as the next item; one that waits
	// while no step runs follows the last step, where the next iteration
	// picks it up. It is written in the composer under the view (the shared
	// chat composer). The task's notebook and its checks are side panel tabs.
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
	// fallback (see TaskRun). The notebook (its tab) is read by the task's
	// creator alone, and here only for the runs that did not update it; a
	// review has no notebook.
	const blocker = $derived(roomBlocker(task));
	const mine = $derived(task.creator_id === authState.user?.id);
	const run = useTaskRun(
		untrack(() => chat),
		() => task,
		untrack(() => ({ room: true, notebook: mine && task.kind !== 'review' }))
	);
	const steps = $derived(run.steps);
	const missed = $derived(missedNotebook(run.notebook));
	// Opening the details is reading from the top.
	const scroller = useMessageScroller();
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

	// Where the task is and who works on it now, for the header: its
	// pipeline phase, or the workflow step it is on.
	const phase = $derived.by(() => {
		if (run.pipeline) return pipelinePhases(run.pipeline, run.view, task).find((p) => p.state === 'current')?.label ?? '';
		const on = run.followsWorkflow ? steps.find((step) => step.current) : undefined;
		return on ? stepTitle(on) : '';
	});
	const working = $derived(
		run.pipeline
			? pipelineMembers(run.pipeline, run.view?.runs, run.view?.profiles ?? [], task).filter((member) => member.working).length
			: steps.filter((step) => step.current && step.status === 'running').length
	);
	const stateText = $derived(phase ? `${phase} · ${taskLabel(task.state)}` : taskLabel(task.state));

	// The lines a task that follows a workflow is filtered by: one role, and
	// only its decisions (phase moves, verdicts, waivers, approvals).
	const EVERYONE = '*';
	let role = $state(EVERYONE);
	let decisions = $state(false);
	const filter = $derived<ThreadFilter>({ role: role === EVERYONE ? '' : role, decisions });
	const resetFilter = () => {
		role = EVERYONE;
		decisions = false;
	};
	const stepRows = $derived(stepThread(steps, (step) => stepAgent(step) || stepTitle(step)));
	const roles = $derived(
		run.pipeline
			? [...run.pipeline.roster.map((member) => member.role), ...(run.pipeline.lead ? ['lead'] : [])]
			: threadRoles(stepRows, (row) => (row.role ? [row.role] : []))
	);
	// A task without a workflow is one line per attempt of one agent: nothing to filter.
	const filterable = $derived(run.followsWorkflow && !!run.view);
	const filtered = $derived(!!filter.role || filter.decisions);
	const shownSteps = $derived(shownRows(stepRows, filter, (row) => (row.role ? [row.role] : []), (row) => row.decision));

	// The running step is open and the finished ones are closed, until the
	// reader opens or closes one; a link to a step (#step-4) opens it, and
	// the folded line that holds it, and shows every line again if a filter
	// hid it.
	let chosen = $state<Record<string, boolean>>({});
	const current = $derived(openStep(steps.filter((step) => !step.reviewTaskId)));
	let followed = '';
	$effect(() => {
		const linked = page.url.hash.slice(1);
		if (!linked || linked === followed || !steps.some((step) => step.anchor === linked)) return;
		followed = linked;
		untrack(() => {
			chosen[linked] = true;
			const fold = stepRows.find((row) => row.type === 'fold' && row.steps.some((step) => step.anchor === linked));
			if (fold) chosen[fold.key] = true;
			if (!shownSteps.some((row) => row.key === (fold?.key ?? linked))) resetFilter();
		});
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
	// A requested review is a verdict of no role of the task's workflow.
	const reviewsShown = $derived(!filter.role);
	// The request is written for the assistant, which asks for approval.
	function requestReview(text: string) {
		chat.composerDraft = text;
		onback();
	}

	// Durations of running steps and turns tick.
	let now = $state(Date.now());
	const ticking = $derived(
		steps.some((step) => step.status === 'running') || (run.view?.runs ?? []).some((item) => item.status === 'running')
	);
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
	{#if reviewsShown}<li class="min-w-0"><AgentReview {session} /></li>{/if}
{/snippet}

{#snippet stepItem(step: TimelineStep)}
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
{/snippet}

<!-- Steps Kuayle ran one after another, as one line that opens to each. -->
{#snippet fold(row: Extract<StepRow, { type: 'fold' }>)}
	{@const failed = row.steps.find((step) => step.status === 'failed' || step.status === 'stopped')}
	<section class="min-w-0" data-step="fold" aria-label={row.steps.map((step) => stepTitle(step)).join(' · ')}>
		<Collapsible.Root bind:open={() => chosen[row.key] ?? false, (open) => (chosen[row.key] = open)}>
			<Collapsible.Trigger
				class="group flex w-full min-w-0 cursor-pointer items-center gap-2.5 rounded-md py-1 text-left text-xs text-muted-foreground outline-none hover:text-foreground focus-visible:ring-3 focus-visible:ring-ring/50"
			>
				<span class="inline-flex size-5 shrink-0 items-center justify-center"><Layers size={14} aria-hidden="true" /></span>
				<span class="min-w-0 flex-1 truncate">{row.steps.map((step) => stepTitle(step)).join(' · ')}</span>
				{#if failed}<StepStatus status={failed.status} />{/if}
				<ChevronRight
					size={14}
					aria-hidden="true"
					class="shrink-0 text-muted-foreground/60 transition-transform group-aria-expanded:rotate-90 motion-reduce:transition-none"
				/>
			</Collapsible.Trigger>
			<Collapsible.Content>
				<ol class="ml-2.5 flex min-w-0 flex-col border-l pl-3 sm:pl-4">
					{#each row.steps as step (step.anchor)}<li class="min-w-0">{@render stepItem(step)}</li>{/each}
				</ol>
			</Collapsible.Content>
		</Collapsible.Root>
	</section>
{/snippet}

<Collapsible.Root
	bind:open={detailsOpen}
	onOpenChange={(open) => {
		if (open) scroller.scrollToStart({ behavior: 'auto' });
	}}
	class="flex min-w-0 flex-col gap-3"
	data-testid="agent-thread"
>
	<header class="@container sticky top-0 z-20 flex flex-col" data-testid="agent-thread-header">
		<!-- The touch row can use 4px of the gap below the sticky header. -->
		<div class="mobile-touch-targets flex min-w-0 items-center gap-1.5 border-b bg-background py-1.5 max-md:-mb-1 max-md:py-0 pointer-coarse:-mb-1 pointer-coarse:py-0 @min-[36rem]:gap-2">
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
			<Collapsible.Trigger
				class="group flex h-7 min-w-0 flex-auto cursor-pointer items-center gap-1 rounded-md text-start outline-none hover:bg-muted focus-visible:ring-3 focus-visible:ring-ring/50"
				aria-label={detailsOpen ? m['tasks.transcript_show_less']() : m['sharedComponents.create_issue.expand']()}
				aria-describedby="thread-title-{task.id}"
				title={m['tasks.details']()}
			>
				<span id="thread-title-{task.id}" class="min-w-0 flex-1 truncate text-sm font-medium">{task.prompt.split('\n', 1)[0]}</span>
				<ChevronDown size={14} aria-hidden="true" class="mx-1.5 shrink-0 text-muted-foreground transition-transform group-aria-expanded:rotate-180 motion-reduce:transition-none" />
			</Collapsible.Trigger>
			{#if working}{@const text = m['tasks.working_count']({ count: working })}<span
					class="inline-flex shrink-0 items-center gap-1 text-xs text-[var(--status-running)] tabular-nums"
					title={text}
					data-testid="thread-working"
					><LoaderCircle size={12} aria-hidden="true" class="animate-spin motion-reduce:animate-none" /><span
						class="sr-only @min-[30rem]:not-sr-only">{text}</span
					></span
				>{/if}
			{#if task.pending_decisions?.length}<Badge variant="secondary" data-testid="thread-decision-count"
					>{m['tasks.decision_count']({ count: task.pending_decisions.length })}</Badge
				>{/if}
			<Badge variant="outline" class="min-w-0 max-w-28 shrink @min-[36rem]:max-w-52 {toneBadge[tone]}" title={stateText} role="status"
				><span class="truncate">{stateText}</span></Badge
			>
			<div class="flex shrink-0 items-center gap-0.5 @min-[36rem]:gap-1">
				<TaskStopButton {chat} {task} {identifier} onerror={(message) => (stopError = message)} />
				<TaskThreadMenu
					{chat}
					{task}
					{allowedAgents}
					onrequest={requestReview}
					ondownload={() => void downloadTrace()}
					downloading={traceBusy}
				/>
			</div>
		</div>
		{#if mine && task.pending_decisions?.length}
			<div class="mt-2 max-h-[46dvh] space-y-2 overflow-y-auto overscroll-contain pb-1">
				{#if blocker}<a
						class="block text-sm font-medium underline underline-offset-2 [overflow-wrap:anywhere]"
						href={'#' + decisionAnchor(task.id, blocker.kind, blocker.seq)}>{decisionWaitLabel(blocker)}</a
					>{/if}
				{#each task.pending_decisions as item (item.kind + item.seq)}<OwnerDecisionCard
						{chat}
						{run}
						{task}
						{item}
					/>{/each}
			</div>
		{/if}
	</header>

	{#if stopError}<p role="alert" class="text-sm break-words text-destructive">{stopError}</p>{/if}
	{#if traceError}<p role="alert" class="text-sm break-words text-destructive">{traceError}</p>{/if}

	<section aria-label={m['tasks.details']()} class="min-w-0">
		<Collapsible.Content class="space-y-2 pb-2">
			{#if detailsOpen}
				<p class="text-xs text-muted-foreground">{identifier} · {m['tasks.attempt']({ number: task.generation })}</p>
				<p class="break-words whitespace-pre-wrap text-sm">{task.prompt}</p>
				{#if task.reason && !blocker}<p class="break-words text-xs text-muted-foreground">{task.reason}</p>{/if}
				<UsageLine usage={run.usage} label={m['tasks.usage_total']()} />
			{/if}
		</Collapsible.Content>
	</section>

	{#if filterable}<div
			class="mobile-touch-targets flex min-w-0 flex-wrap items-center gap-1.5"
			role="group"
			aria-label={m['tasks.filter_label']()}
			data-testid="thread-filters"
		>
			<SelectField
				size="sm"
				aria-label={m['tasks.filter_role']()}
				bind:value={role}
				options={[
					{ value: EVERYONE, label: m['tasks.filter_everyone']() },
					...roles.map((value) => ({ value, label: value }))
				]}
			/>
			<Toggle variant="outline" size="sm" bind:pressed={decisions} class="text-xs font-normal"
				><Gavel aria-hidden="true" />{m['tasks.filter_decisions']()}</Toggle
			>
		</div>{/if}

	{#if run.pipeline}<TaskRoom {chat} {task} {run} {filter} {now} onreset={resetFilter} />{:else}<ol
			aria-label={m['workflows.task.path']()}
			class="flex min-w-0 flex-col gap-1"
		>
			{#each shownSteps as row (row.key)}
				{#each row.type === 'fold' ? row.steps : [row.step] as step (step.anchor)}{#each timeline.before[step.anchor] ?? [] as session (session.id)}{@render reviewRow(
							session
						)}{/each}{/each}
				<li class="min-w-0">{#if row.type === 'fold'}{@render fold(row)}{:else}{@render stepItem(row.step)}{/if}</li>
			{/each}
			{#each timeline.after as session (session.id)}{@render reviewRow(session)}{/each}
			<!-- Waiting with no step running: the next iteration picks these up here. -->
			{#if conversation.pending.length && steps.length && !filtered}<li class="flex min-w-0 flex-col gap-3 py-2" data-step="pending">
					{#each conversation.pending as placed (placed.message.id)}<AgentLiveMessage {placed} />{/each}
				</li>{/if}
		</ol>
		{#if filtered && stepRows.length && !shownSteps.length}<div class="flex flex-wrap items-center gap-2" role="status">
				<p class="text-sm text-muted-foreground">{m['tasks.filter_empty']()}</p>
				<Button class="mobile-touch-targets" variant="ghost" size="sm" onclick={resetFilter}>{m['tasks.filter_reset']()}</Button>
			</div>{/if}{/if}
	{#if run.failed}<div class="flex flex-wrap items-center gap-2">
			<p role="alert" class="text-sm text-destructive">
				{run.followsWorkflow ? m['workflows.task.load_failed']() : m['tasks.load_error']()}
			</p>
			<Button class="mobile-touch-targets" variant="ghost" size="sm" onclick={() => void run.load()}><RefreshCw />{m['tasks.refresh']()}</Button>
		</div>{/if}
</Collapsible.Root>
