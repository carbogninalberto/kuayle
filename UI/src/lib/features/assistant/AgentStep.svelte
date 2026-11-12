<script lang="ts">
	import {
		ChevronRight,
		Flag,
		GitMerge,
		GitPullRequestArrow,
		ListChecks,
		MessageCircleWarning,
		Play,
		SearchCheck,
		UserCheck
	} from '@lucide/svelte';
	import * as Collapsible from '$lib/components/ui/collapsible/index.js';
	import * as Tooltip from '$lib/components/ui/tooltip/index.js';
	import AgentLogo from '$lib/features/agents/AgentLogo.svelte';
	import { agentInfo } from '$lib/features/agents/agents';
	import type { WorkflowNodeType } from '$lib/api/workflows';
	import type { ConversationState } from './conversation-state.svelte';
	import AgentStepRun from './AgentStepRun.svelte';
	import Markdown from './Markdown.svelte';
	import StepStatus from './StepStatus.svelte';
	import { stepDuration, type TimelineStep } from './agent-timeline';
	import { stepAgent, stepTitle } from './step-labels';
	import type { Conversation } from './agent-conversation';
	import { formatDuration } from './shell-rules';
	import { getLocale, m } from '$lib/features/i18n/messages';

	// One step of a task's timeline. An agent's step is a single quiet line
	// (what, who, how long, how it went) that opens to its work: the result
	// first, the trace below. Every other step is a compact row. The section
	// id is the step's anchor, so other views can link to it.
	let {
		chat,
		step,
		open = $bindable(false),
		now,
		reviewHref,
		conversation
	}: {
		chat: ConversationState;
		step: TimelineStep;
		open?: boolean;
		// The current time, ticking while a step runs.
		now: number;
		// Where the step's review task opens as an agent view of its own.
		reviewHref?: string;
		// What was said to the agent and what it answered, by run.
		conversation?: Conversation;
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
	// What a compact row says when opened: what the runner recorded for it.
	const note = $derived(step.detail || (step.status === 'done' ? '' : step.reason));
</script>

{#snippet status()}<StepStatus status={step.status} />{/snippet}

{#snippet tail()}
	{#if step.visit > 1}<span class="shrink-0 text-xs text-muted-foreground tabular-nums">×{step.visit}</span>{/if}
	{#if time}<time datetime={step.startedAt} class="shrink-0 text-xs text-muted-foreground tabular-nums">{time}</time
		>{/if}
	<span class="sr-only">{aside}</span>
{/snippet}

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
	class="min-w-0 scroll-mt-4"
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
				{#if step.harness}<AgentLogo agent={step.harness} size="sm" />{:else}<span class="inline-flex size-5 shrink-0 items-center justify-center text-muted-foreground"
						><SearchCheck size={14} aria-hidden="true" /></span
					>{/if}
				<span class="min-w-0 flex-1 truncate text-sm">
					<span class="font-medium">{title}</span>{#if who}<span class="text-muted-foreground">{` · ${who}`}</span>{/if}
				</span>
				{#if step.model}<span class="hidden max-w-40 shrink truncate text-xs text-muted-foreground sm:inline"
						>{step.model}</span
					>{/if}
				{#if unanswered}<span
						role="img"
						aria-label={m['tasks.live_state_unanswered']()}
						title={m['tasks.live_state_unanswered']()}
						class="inline-flex shrink-0 text-[var(--status-warning)]"><MessageCircleWarning size={14} /></span
					>{/if}
				{@render tail()}
				{@render status()}
				{@render chevron()}
							</Collapsible.Trigger>
						{/snippet}
					</Tooltip.Trigger>
					<Tooltip.Content side="top" align="end">{aside}</Tooltip.Content>
				</Tooltip.Root>
			</Tooltip.Provider>
			<Collapsible.Content>
				{#if open}<div class="ml-2.5 flex min-w-0 flex-col gap-4 border-l py-3 pl-4 sm:pl-5">
						{#if step.model}<p class="truncate text-xs text-muted-foreground sm:hidden">{step.model}</p>{/if}
						{#if step.plan}<article aria-label={m['tasks.plan']()} class="min-w-0 text-sm">
								<Markdown source={step.plan} />
							</article>{/if}
						{#each step.runs as run, index (run.id)}<AgentStepRun
								{chat}
								{run}
								active={running && index === step.runs.length - 1}
								pending={pending && index === step.runs.length - 1}
								concluded={!!step.plan && index === step.runs.length - 1}
								messages={conversation?.byRun.get(run.id)}
							/>{/each}
						{#if step.detail && !step.plan}<p class="text-sm break-words whitespace-pre-wrap text-muted-foreground">
								{step.detail}
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
	{:else}
		{@const Icon = rowIcon[step.nodeType]}
		{#snippet row()}
			<span class="inline-flex size-5 shrink-0 items-center justify-center"><Icon size={14} aria-hidden="true" /></span>
			<span class="min-w-0 flex-1 truncate">{title}</span>
			{@render tail()}
			{#if step.status !== 'done'}{@render status()}{/if}
		{/snippet}
		{#if note}
			<Collapsible.Root bind:open>
				<Collapsible.Trigger
					class="group flex w-full min-w-0 cursor-pointer items-center gap-2.5 rounded-md py-1 text-left text-xs text-muted-foreground outline-none hover:text-foreground focus-visible:ring-3 focus-visible:ring-ring/50"
					>{@render row()}{@render chevron()}</Collapsible.Trigger
				>
				<Collapsible.Content>
					<p class="ml-2.5 border-l py-2 pl-4 text-xs break-words whitespace-pre-wrap text-muted-foreground sm:pl-5">
						{note}
					</p>
				</Collapsible.Content>
			</Collapsible.Root>
		{:else}
			<div class="flex min-w-0 items-center gap-2.5 py-1 text-xs text-muted-foreground">{@render row()}</div>
		{/if}
	{/if}
</section>
