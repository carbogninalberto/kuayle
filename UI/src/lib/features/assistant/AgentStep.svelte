<script lang="ts">
	import {
		ChevronRight,
		Flag,
		GitMerge,
		GitPullRequestArrow,
		ListChecks,
		MessageCircleWarning,
		NotebookPen,
		Play,
		ScrollText,
		SearchCheck,
		UserCheck
	} from '@lucide/svelte';
	import * as Collapsible from '$lib/components/ui/collapsible/index.js';
	import * as Tooltip from '$lib/components/ui/tooltip/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import AgentLogo from '$lib/features/agents/AgentLogo.svelte';
	import { agentInfo } from '$lib/features/agents/agents';
	import type { WorkflowBranchRecord, WorkflowNodeType } from '$lib/api/workflows';
	import { conditionLabel } from '$lib/features/workflows/labels';
	import type { ConversationState } from './conversation-state.svelte';
	import AgentReview from './AgentReview.svelte';
	import AgentStepRun from './AgentStepRun.svelte';
	import Markdown from './Markdown.svelte';
	import RunPrompt from './RunPrompt.svelte';
	import StepStatus from './StepStatus.svelte';
	import TurnLine from './TurnLine.svelte';
	import { stepDuration, type TimelineStep } from './agent-timeline';
	import { firstLine, workTokens } from './thread-rows';
	import { stepAgent, stepTitle } from './step-labels';
	import type { Conversation } from './agent-conversation';
	import type { ReviewSession } from './agent-review';
	import { formatDuration } from './shell-rules';
	import { getLocale, m } from '$lib/features/i18n/messages';

	// One step of a task's timeline. An agent's step is a single quiet line,
	// as a role's turn is in a room (KUA-286): what, who, what it ended with,
	// how long, the tokens and how it went. It opens to its work: the harness,
	// model and flags, the result, the trace below. Other steps disclose their
	// recorded details. The section id is the step's anchor, so other views
	// can link to it.
	let {
		chat,
		step,
		open = $bindable(false),
		now,
		reviewHref,
		reviewSession,
		conversation,
		promptable = false,
		missed
	}: {
		chat: ConversationState;
		step: TimelineStep;
		open?: boolean;
		// The current time, ticking while a step runs.
		now: number;
		// Where the step's review task opens as an agent view of its own.
		reviewHref?: string;
		reviewSession?: ReviewSession;
		// What was said to the agent and what it answered, by run.
		conversation?: Conversation;
		// The reader may open the prompt the step's agent received.
		promptable?: boolean;
		// The runs that ended without updating the task's notebook.
		missed?: Set<string>;
	} = $props();

	const rowIcon: Record<WorkflowNodeType, typeof Play> = {
		start: Play,
		agent: Play,
		check: ListChecks,
		approval: UserCheck,
		publish: GitPullRequestArrow,
		merge: GitMerge,
		end: Flag
	};
	const harness = $derived(step.harness ? agentInfo(step.harness).name : '');
	const title = $derived(stepTitle(step));
	const who = $derived(stepAgent(step));
	const ms = $derived(stepDuration(step, now));
	// A step passed through at once (start, end) has no time worth showing.
	const time = $derived(ms === undefined || ms < 1000 ? '' : formatDuration(ms));
	const started = $derived(
		new Date(step.startedAt).toLocaleString(getLocale(), { dateStyle: 'medium', timeStyle: 'short' })
	);
	const running = $derived(step.current && step.status === 'running');
	// The task is still on this step: its newest run may not be recorded yet.
	const pending = $derived(step.current && (step.status === 'running' || step.status === 'waiting'));
	// Which harness ran and when it started are not on the line: they are
	// read out with it, and shown on hover or keyboard focus.
	const aside = $derived(harness ? `${harness} · ${started}` : started);
	// A run of this step ended without replying to a message.
	const unanswered = $derived(step.runs.some((run) => conversation?.unanswered.has(run.id)));
	// A run of this step ended without updating the task's notebook.
	const unrecorded = $derived(step.runs.some((run) => missed?.has(run.id)));
	// The prompt the step's agent received opens from the opened step, so a
	// closed line stays one control. A run the task was only just given has
	// no prompt recorded yet.
	const prompted = $derived(step.runs.filter((run) => run.status !== 'queued'));
	let prompt = $state(false);
	// What a compact row says when opened: what the runner recorded for it.
	const note = $derived(step.detail || step.reason);
	const findings = $derived(
		reviewHref ? reviewSession?.review.findings || (step.status === 'done' || step.status === 'changes' ? step.detail : '') : ''
	);
	// What the step did on its pull request's branch, from the facts Kuayle
	// read (KUA-265): why it moved or stayed without a person.
	const branchText = (branch: WorkflowBranchRecord) => {
		const facts = { head: (branch.head ?? '').slice(0, 7), base: branch.base ?? '', behind: branch.behind };
		switch (branch.action) {
			case 'updated':
				return m['tasks.branch.updated'](facts);
			case 'update_limit':
				return m['tasks.branch.update_limit']({ ...facts, updates: branch.updates ?? 0 });
			case 'conflict':
				return m['tasks.branch.conflict'](facts);
			case 'migration_collision':
				return m['tasks.branch.migration_collision'](facts);
			case 'judged_update':
				return m['tasks.branch.judged_update']({ ...facts, published: (branch.published ?? '').slice(0, 7) });
			case 'merge_refused':
				return m['tasks.branch.merge_refused']({ ...facts, refusal: branch.refusal ?? '' });
		}
		return '';
	};
	const branchReason = $derived(step.branch ? branchText(step.branch) : '');
	// What the step ended with: its last run's final message, its plan or
	// findings, or what the runner recorded once it is over.
	const summary = $derived(
		firstLine(step.runs.at(-1)?.summary || step.plan || findings || (step.status === 'running' ? '' : step.detail))
	);
	const tokens = $derived(workTokens(step.runs));
</script>

{#snippet status()}<StepStatus status={step.status} />{/snippet}

{#snippet tail()}
	{#if step.visit > 1}<span class="shrink-0 text-xs text-muted-foreground tabular-nums">×{step.visit}</span>{/if}
	{#if time}<time datetime={step.startedAt} class="shrink-0 text-xs text-muted-foreground tabular-nums">{time}</time
		>{/if}
	<span class="sr-only">{aside}</span>
{/snippet}

<!-- Which harness ran it and when are read out with the line, shown on hover or focus. -->
{#snippet spoken()}<span class="sr-only">{aside}</span>{/snippet}

{#snippet chevron()}
	<ChevronRight
		size={14}
		aria-hidden="true"
		class="shrink-0 text-muted-foreground/60 transition-transform group-aria-expanded:rotate-90 motion-reduce:transition-none"
	/>
{/snippet}

<section
	id={step.anchor}
	aria-label={step.kind !== 'agent' ? undefined : who ? `${title} · ${who}` : title}
	class="min-w-0 scroll-mt-14"
	data-step={step.kind}
	data-status={step.status}
>
	{#if step.kind === 'agent'}
		<Collapsible.Root bind:open>
			<Tooltip.Provider delayDuration={500}>
				<Tooltip.Root>
					<Tooltip.Trigger>
						{#snippet child({ props })}
							<Collapsible.Trigger
								{...props}
								class="group flex w-full min-w-0 cursor-pointer items-center gap-2.5 rounded-md py-1.5 text-left outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
							>
									<TurnLine
										role={title}
										{who}
										{summary}
										duration={time}
										startedAt={step.startedAt}
										{tokens}
										status={step.status}
										visit={step.visit}
										marks={spoken}
									/>
								</Collapsible.Trigger>
						{/snippet}
					</Tooltip.Trigger>
					<Tooltip.Content side="top" align="end">{aside}</Tooltip.Content>
				</Tooltip.Root>
			</Tooltip.Provider>
			<Collapsible.Content>
				{#if open}<div class="ml-2.5 flex min-w-0 flex-col gap-4 border-l py-3 pl-4 sm:pl-5">
						<!-- Who ran it and what went wrong besides its status: inside, so the line stays quiet. -->
						<p class="flex min-w-0 flex-wrap items-center gap-x-1.5 gap-y-1 text-xs text-muted-foreground" data-testid="step-details">
							{#if step.harness}<AgentLogo agent={step.harness} size="sm" class="size-4" /><span>{harness}</span>{:else}<SearchCheck
									size={13}
									aria-hidden="true"
								/>{/if}
							{#if step.model}<span class="min-w-0 truncate">· {step.model}</span>{/if}
							<time datetime={step.startedAt}>· {started}</time>
							{#if unanswered}<span class="inline-flex items-center gap-1 text-[var(--status-warning)]" data-flag="reply"
									><MessageCircleWarning size={13} aria-hidden="true" />{m['tasks.live_state_unanswered']()}</span
								>{/if}
							{#if unrecorded}<span class="inline-flex items-center gap-1 text-[var(--status-warning)]" data-flag="notebook"
									><NotebookPen size={13} aria-hidden="true" />{m['notebook.missed']()}</span
								>{/if}
						</p>
						{#if promptable && prompted.length}<Button
								variant="ghost"
								size="xs"
								class="-my-1 -ml-2 self-start text-muted-foreground"
								aria-label="{m['tasks.transcript_from_you']()} · {title}"
								aria-haspopup="dialog"
								onclick={() => (prompt = true)}><ScrollText />{m['tasks.transcript_from_you']()}</Button
							>{/if}
						{#if step.plan}<article aria-label={m['tasks.plan']()} class="min-w-0 text-sm">
								<Markdown source={step.plan} />
							</article>{/if}
						{#if reviewSession}<AgentReview session={reviewSession} embedded />{/if}
						{#if findings && !reviewSession?.review.findings}<Markdown source={findings} class="text-sm" />{/if}
						{#each step.runs as run, index (run.id)}<AgentStepRun
								{chat}
								{run}
								active={running && index === step.runs.length - 1}
								pending={pending && index === step.runs.length - 1}
								concluded={!!(step.plan || findings) && index === step.runs.length - 1}
								messages={conversation?.byRun.get(run.id)}
							/>{/each}
						{#if step.detail && !step.plan && !findings}
							<p class="text-sm break-words whitespace-pre-wrap text-muted-foreground">{step.detail}</p>
						{/if}
						{#if branchReason}<p class="text-sm break-words text-muted-foreground" data-branch={step.branch?.action}>
								{branchReason}
							</p>{/if}
						{#if reviewHref}<a
								href={reviewHref}
								class="inline-flex w-fit items-center gap-1.5 rounded-sm text-sm text-muted-foreground underline-offset-4 outline-none hover:text-foreground hover:underline focus-visible:ring-3 focus-visible:ring-ring/50"
								><SearchCheck size={14} aria-hidden="true" />{m['tasks.review_by']({
									agent: who || harness
								})}</a
							>{/if}
					</div>{/if}
			</Collapsible.Content>
		</Collapsible.Root>
		{#if prompt}<RunPrompt slug={chat.slug} runs={prompted} {title} onclose={() => (prompt = false)} />{/if}
	{:else}
		{@const Icon = rowIcon[step.nodeType]}
		{#snippet row()}
			<span class="inline-flex size-5 shrink-0 items-center justify-center"><Icon size={14} aria-hidden="true" /></span>
			<span class="min-w-0 flex-1 truncate">{title}</span>
			{@render tail()}
			{#if step.status !== 'done'}{@render status()}{/if}
		{/snippet}
		<Collapsible.Root bind:open>
			<Collapsible.Trigger
				class="group flex w-full min-w-0 cursor-pointer items-center gap-2.5 rounded-md py-1 text-left text-xs text-muted-foreground outline-none hover:text-foreground focus-visible:ring-3 focus-visible:ring-ring/50"
				>{@render row()}{@render chevron()}</Collapsible.Trigger
			>
			<Collapsible.Content>
				<div class="ml-2.5 space-y-1 border-l py-2 pl-4 text-xs text-muted-foreground sm:pl-5">
					<div class="flex flex-wrap items-center gap-2">
						{#if step.status === 'done'}{@render status()}{/if}
						<time datetime={step.startedAt}>{started}</time>
						{#if step.condition}<span>· {conditionLabel(step.condition)}</span>{/if}
					</div>
					{#if step.question && step.question !== note}<p class="break-words whitespace-pre-wrap">{step.question}</p>{/if}
					{#if note}<p class="break-words whitespace-pre-wrap">{note}</p>{/if}
					{#if branchReason}<p class="break-words" data-branch={step.branch?.action}>{branchReason}</p>{/if}
				</div>
			</Collapsible.Content>
		</Collapsible.Root>
	{/if}
</section>
