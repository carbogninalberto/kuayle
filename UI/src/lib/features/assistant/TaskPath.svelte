<script lang="ts">
	import { untrack } from 'svelte';
	import { ArrowRight, ChevronRight, CircleArrowUp, Pause, Play, RotateCw } from '@lucide/svelte';
	import { page } from '$app/state';
	import { Button } from '$lib/components/ui/button/index.js';
	import * as Tooltip from '$lib/components/ui/tooltip/index.js';
	import type { AgentTask } from '$lib/api/agent-tasks';
	import { conditionLabel } from '$lib/features/workflows/labels';
	import type { ConversationState } from './conversation-state.svelte';
	import StepStatus from './StepStatus.svelte';
	import { stepDuration } from './agent-timeline';
	import { stepAgent, stepTitle } from './step-labels';
	import { taskLabel } from './task-labels';
	import {
		compactPath,
		pathDuration,
		pathTime,
		taskPath,
		type PathEvent,
		type PathStep,
		type PathWait
	} from './task-path';
	import { useTaskRun } from './task-run.svelte';
	import { getLocale, m } from '$lib/features/i18n/messages';

	// The path a task took, as one short vertical line: each step with its
	// agent, how it went (an icon), when it started and how long it took.
	// Compact shows each step once, as it last went; one control opens the
	// whole path with its loops, pauses and version moves. A step links to
	// its section in the agent view and opens to where the task went next and
	// why. Only the step the task is on says more by itself: what it waits
	// for, or why the task stopped there. A task without a workflow shows its
	// attempts the same way. It reads the task's shared run (useTaskRun), so
	// it follows the task live without a request of its own.
	let {
		chat,
		task,
		saysReason = $bindable(false)
	}: {
		chat: ConversationState;
		task: AgentTask;
		// The path shows the task's reason itself (why it waits or stopped).
		saysReason?: boolean;
	} = $props();

	const run = useTaskRun(
		untrack(() => chat),
		() => task,
		{ history: untrack(() => !task.workflow_version_id) }
	);
	const entries = $derived(
		taskPath(run.steps, {
			task,
			status: run.followsWorkflow ? run.view?.status : undefined,
			pauses: run.view?.pauses,
			transitions: run.transitions
		})
	);
	const compact = $derived(compactPath(entries));
	let full = $state(false);
	const shown = $derived(full ? entries : compact);
	let opened = $state<Record<string, boolean>>({});

	const waitText = (wait: PathWait, entry: PathStep) => {
		switch (wait.kind) {
			case 'ci':
				return m['tasks.path.wait_ci']();
			case 'merge':
				return m['tasks.path.wait_merge']();
			case 'approval':
				return m['workflows.task.waiting_approval']({ question: wait.question || stepTitle(entry.step) });
			case 'paused':
				return m['tasks.state.paused']();
			default:
				return wait.text;
		}
	};
	// What the step the task is on says without being opened.
	const note = (entry: PathStep) => entry.stopped ?? (entry.wait ? waitText(entry.wait, entry) : '');
	$effect(() => {
		saysReason = shown.some((entry) => entry.kind === 'step' && !!note(entry));
	});
	// A step opens when it has more to say: where the task went, what the
	// runner recorded, or an attempt's state changes.
	const detail = (entry: PathStep) => (entry.step.detail && entry.step.detail !== note(entry) ? entry.step.detail : '');
	const opens = (entry: PathStep) => !!entry.next || !!detail(entry) || entry.changes.length > 0;

	const eventLabel = (event: PathEvent) =>
		event.action === 'paused'
			? m['tasks.state.paused']()
			: event.action === 'resumed'
				? m['tasks.path.resumed']()
				: m['tasks.path.moved']({ version: event.to ?? '' });

	// Durations of running steps tick.
	let now = $state(Date.now());
	// What counts as today is asked again whenever the path changes.
	$effect(() => {
		void entries;
		now = Date.now();
	});
	const ticking = $derived(run.steps.some((step) => step.status === 'running'));
	$effect(() => {
		if (!ticking) return;
		now = Date.now();
		const timer = setInterval(() => (now = Date.now()), 1000);
		return () => clearInterval(timer);
	});
	const short = (at: string) => pathTime(at, now, getLocale());
	const absolute = (at: string) => new Date(at).toLocaleString(getLocale(), { dateStyle: 'medium', timeStyle: 'short' });
	const duration = (entry: PathStep) => {
		const ms = stepDuration(entry.step, now);
		// A step passed through at once has no time worth showing.
		return ms === undefined || ms < 1000 ? '' : pathDuration(ms);
	};
	// The step's section in the task's agent view.
	const href = (anchor: string) => {
		const url = new URL(page.url);
		url.searchParams.set('agent', task.id);
		url.searchParams.delete('task');
		return `${url.pathname}${url.search}#${anchor}`;
	};
	const focus = 'outline-none focus-visible:ring-2 focus-visible:ring-ring/50';
</script>

{#if shown.length}
	<!-- Wide enough, a step is one line with its time at the end; narrower, the time goes under its name. -->
	<div class="@container min-w-0" data-testid="task-path">
		<Tooltip.Provider delayDuration={300}>
			<ol aria-label={m['workflows.task.path']()} class="min-w-0">
				{#each shown as entry, index (entry.key)}
					{@const last = index === shown.length - 1}
					<li
						class="relative min-w-0 pl-6 {last ? '' : 'pb-2'}"
						data-entry={entry.kind === 'step' ? 'step' : entry.action}
						aria-current={entry.kind === 'step' && entry.step.current ? 'step' : undefined}
					>
						<!-- The line the steps sit on, from the first to the last. -->
						{#if shown.length > 1}<span
								aria-hidden="true"
								class="absolute left-[7px] w-px bg-border {index === 0 ? 'top-2.5' : 'top-0'} {last
									? 'h-2.5'
									: 'bottom-0'}"
							></span>{/if}
						{#if entry.kind === 'step'}
							{@const step = entry.step}
							{@const agent = stepAgent(step)}
							{@const took = duration(entry)}
							{@const said = note(entry)}
							{@const more = detail(entry)}
							<span class="absolute top-[2.5px] left-0 inline-flex rounded-full bg-background"
								><StepStatus status={step.status} /></span
							>
							<div class="flex min-w-0 items-start gap-1">
								<Tooltip.Root>
									<Tooltip.Trigger>
										{#snippet child({ props })}
											<!-- The trigger's props are a button's; a link takes neither its type nor disabled. -->
											{@const { type: _type, disabled: _disabled, ...link } = props}
											<a
												{...link}
												href={href(entry.key)}
												class="group flex min-w-0 flex-1 flex-col rounded-sm text-[13px] leading-5 @sm:flex-row @sm:items-baseline @sm:gap-2 {focus}"
											>
												<span class="flex min-w-0 items-baseline gap-1.5"
													><span class="min-w-0 truncate"
														><span class="font-medium underline-offset-2 group-hover:underline">{stepTitle(step)}</span
														>{#if agent}<span class="text-muted-foreground">{` · ${agent}`}</span>{/if}</span
													>{#if step.visit > 1}<span class="shrink-0 text-xs text-muted-foreground tabular-nums"
															>×{step.visit}</span
														>{/if}</span
												>
												<span class="shrink-0 text-xs leading-4 text-muted-foreground tabular-nums @sm:ml-auto"
													><time datetime={step.startedAt}>{short(step.startedAt)}</time>{took ? ` · ${took}` : ''}</span
												>
												<span class="sr-only">{absolute(step.startedAt)}</span>
											</a>
										{/snippet}
									</Tooltip.Trigger>
									<!-- The whole name too: the row may have had to cut it. -->
									<Tooltip.Content side="top" align="start" class="max-w-64"
										><span class="block font-medium">{stepTitle(step)}{agent ? ` · ${agent}` : ''}</span
										>{absolute(step.startedAt)}</Tooltip.Content
									>
								</Tooltip.Root>
								{#if opens(entry)}<Button
										variant="plain"
										class="flex size-5 shrink-0 cursor-pointer items-center justify-center rounded text-muted-foreground/70 transition-colors hover:bg-muted hover:text-foreground {focus}"
										aria-expanded={!!opened[entry.key]}
										aria-label={m['tasks.path.details']({ step: stepTitle(step) })}
										onclick={() => (opened[entry.key] = !opened[entry.key])}
										><ChevronRight
											size={13}
											class="transition-transform motion-reduce:transition-none {opened[entry.key] ? 'rotate-90' : ''}"
										/></Button
									>{:else}<span class="size-5 shrink-0"></span>{/if}
							</div>
							{#if said}<p
									class="mr-6 text-xs leading-4 break-words {entry.stopped
										? 'text-[var(--status-error)]'
										: 'text-muted-foreground'} {opened[entry.key] ? '' : 'line-clamp-3'}"
									data-testid="task-path-note"
								>
									{said}
								</p>{/if}
							{#if opened[entry.key]}
								<div class="mt-0.5 mr-6 space-y-0.5 text-xs leading-4 text-muted-foreground" data-testid="task-path-detail">
									{#if more}<p class="break-words whitespace-pre-wrap">{more}</p>{/if}
									{#each entry.changes as change, at (at)}
										<p class="flex min-w-0 items-baseline gap-1.5">
											<span class="min-w-0 break-words"
												><span class="text-foreground">{taskLabel(change.state as AgentTask['state'])}</span
												>{#if change.reason}{` · ${change.reason}`}{/if}</span
											>
											<Tooltip.Root>
												<Tooltip.Trigger class="ml-auto shrink-0 cursor-default rounded-sm tabular-nums {focus}"
													><time datetime={change.at}>{short(change.at)}</time><span class="sr-only"
														>{absolute(change.at)}</span
													></Tooltip.Trigger
												>
												<Tooltip.Content side="top" align="end">{absolute(change.at)}</Tooltip.Content>
											</Tooltip.Root>
										</p>
									{/each}
									{#if entry.next}<p class="flex min-w-0 items-center gap-1">
											<ArrowRight size={11} class="shrink-0" aria-hidden="true" /><span class="min-w-0 break-words"
												><span class="text-foreground">{stepTitle(entry.next)}</span>{#if entry.next.condition}{` · ${conditionLabel(
														entry.next.condition
													)}`}{/if}</span
											>
										</p>{/if}
								</div>
							{/if}
						{:else}
							<span
								class="absolute top-[4px] left-[1.5px] inline-flex bg-background text-muted-foreground"
								aria-hidden="true"
							>
								{#if entry.action === 'paused'}<Pause size={12} />{:else if entry.action === 'resumed'}<Play
										size={12}
									/>{:else}<CircleArrowUp size={12} />{/if}
							</span>
							<div class="flex min-w-0 items-baseline gap-1.5 pr-6 text-xs leading-5 text-muted-foreground">
								<span class="min-w-0 break-words">{eventLabel(entry)}</span>
								<Tooltip.Root>
									<Tooltip.Trigger class="shrink-0 cursor-default rounded-sm tabular-nums @sm:ml-auto {focus}"
										><time datetime={entry.at}>{short(entry.at)}</time><span class="sr-only"
											>{absolute(entry.at)}</span
										></Tooltip.Trigger
									>
									<Tooltip.Content side="top" align="start">{absolute(entry.at)}</Tooltip.Content>
								</Tooltip.Root>
							</div>
						{/if}
					</li>
				{/each}
			</ol>
		</Tooltip.Provider>
		{#if entries.length > compact.length}<Button
				variant="plain"
				class="mt-2 ml-6 inline-flex cursor-pointer items-center gap-1 rounded-sm text-xs text-muted-foreground transition-colors hover:text-foreground {focus}"
				aria-expanded={full}
				onclick={() => (full = !full)}
				><ChevronRight
					size={12}
					aria-hidden="true"
					class="transition-transform motion-reduce:transition-none {full ? 'rotate-90' : ''}"
				/>{m['tasks.path.full']()}</Button
			>{/if}
	</div>
{/if}
<!-- What the task did could not be read, also when tried again: one quiet control reads it again. -->
{#if run.failed}<Button
		variant="ghost"
		size="icon-xs"
		class="text-muted-foreground hover:text-foreground"
		aria-label={m['assistant.retry']()}
		title={m['assistant.retry']()}
		onclick={() => void run.load()}><RotateCw /></Button
	>{/if}
