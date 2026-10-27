<script lang="ts">
	import Markdown from './Markdown.svelte';
	import { Badge } from '$lib/components/ui/badge/index.js';
	import { onDestroy, untrack } from 'svelte';
	import TestResults from './TestResults.svelte';
	import AgentPlan from './AgentPlan.svelte';
	import AgentReasoning from './AgentReasoning.svelte';
	import FileTreeNode from './FileTreeNode.svelte';
	import { fileTree, latestPlan, reasoningSummaries } from './agent-trace';
	import {
		Circle,
		CircleCheck,
		CircleAlert,
		LoaderCircle,
		Terminal,
		ExternalLink,
		GitPullRequest,
		SquareTerminal,
		Bot
	} from '@lucide/svelte';
	import { useTerminalDock } from '$lib/features/dev-machines/terminal-dock-context.svelte';
	import { agentSessionCommand, resumableStates } from './agent-session';
	import * as Trace from '$lib/components/ui/trace/index.js';
	import * as CodeBlock from '$lib/components/ui/code-block/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import SelectField from '$lib/components/shared/SelectField.svelte';
	import {
		agentTasks,
		type AgentTask,
		type PublicationView,
		type TaskSignal,
		type TaskTransition,
		type ValidationStep
	} from '$lib/api/agent-tasks';
	import type { AgentRun, DevMachineLogChunk } from '$lib/types/dev-machine';
	import { getMachinePreview, launchMachinePreview, type DevMachinePreview } from '$lib/api/dev-machines';
	import type { ConversationState } from './conversation-state.svelte';
	import { taskLabel } from './task-labels';
	import { m } from '$lib/paraglide/messages.js';
	let {
		task,
		chat,
		onretry,
		ondenied
	}: {
		task: AgentTask;
		chat: ConversationState;
		onretry: (task: AgentTask) => void;
		ondenied: (error: unknown) => void;
	} = $props();
	let open = $state(false),
		busy = $state(false),
		loading = $state(false),
		error = $state('');
	let history = $state<TaskTransition[]>([]),
		runs = $state<AgentRun[]>([]),
		logs = $state<DevMachineLogChunk[]>([]);
	let historyAfter = $state<number | undefined>(),
		runsBefore = $state<string | undefined>(),
		logsAfter = $state<number | undefined>();
	let validation = $state<ValidationStep | null>(null);
	let publication = $state<PublicationView | null>(null);
	let signals = $state<TaskSignal[]>([]);
	let machinePreview = $state<DevMachinePreview | null>(null);
	const signalAction: Record<TaskSignal['action'], (() => string) | undefined> = {
		recorded: undefined,
		follow_up: m['tasks.signal_follow_up'],
		budget_exhausted: m['tasks.signal_budget_exhausted'],
		not_reviewable: m['tasks.signal_not_reviewable'],
		untrusted: m['tasks.signal_untrusted'],
		follow_up_failed: m['tasks.signal_follow_up_failed']
	};
	let selectedRun = $state(untrack(() => task.run_id));
	let seenAttempt = untrack(() => task.run_id),
		disposed = false,
		dirty = false,
		initialRunsLoaded = false;
	let refreshTimer: ReturnType<typeof setTimeout> | undefined;
	const active = $derived(['preparing', 'running', 'validating', 'publishing', 'cancelling'].includes(task.state));
	const failed = $derived(['failed', 'blocked'].includes(task.state));
	const canCancel = $derived(!['completed', 'cancelled'].includes(task.state));
	const canRetry = $derived(['failed', 'blocked', 'awaiting_review'].includes(task.state));
	// The independent check owns its own runtime service; never attribute its
	// output to the native harness or the harness output to the check.
	const isCheck = (line: DevMachineLogChunk) => !!validation && line.service_id === validation.id;
	const output = $derived(
		logs
			.filter((line) => !isCheck(line))
			.map((line) => `${line.stream === 'stderr' ? '[stderr] ' : ''}${line.content}`)
			.join('')
	);
	const checkOutput = $derived(
		logs
			.filter(isCheck)
			.map((line) => line.content)
			.join('')
	);
	const selected = $derived(runs.find((run) => run.id === selectedRun));
	// Derived only from the attempt's recorded native output and git state.
	const nativeLines = $derived(
		logs.filter((line) => !isCheck(line) && line.stream === 'stdout').map((line) => line.content)
	);
	const plan = $derived(latestPlan(nativeLines));
	const reasoning = $derived(reasoningSummaries(nativeLines));
	const changedFiles = $derived(fileTree(selected?.changed_files ?? []));
	// Repository links come from the recorded publication, never agent output.
	const fileLink = $derived.by(() => {
		const url = publication?.step?.status === 'succeeded' ? publication.pull_request_url : undefined;
		const match = url?.match(/^https:\/\/github\.com\/([A-Za-z0-9-]+\/[A-Za-z0-9._-]+)\/pull\/\d+$/);
		const branch = publication?.branch;
		if (!match || !branch || publication?.step?.agent_run_id !== selectedRun) return undefined;
		return (path: string) =>
			`https://github.com/${match[1]}/blob/${branch.split('/').map(encodeURIComponent).join('/')}/${path.split('/').map(encodeURIComponent).join('/')}`;
	});
	// Review links come only from the recorded publication, never from agent output.
	const pullRequest = $derived(
		publication?.step?.status === 'succeeded' ? (publication.pull_request_url ?? undefined) : undefined
	);
	const publicationLabel = $derived(
		publication?.step
			? {
					queued: m['tasks.publication_queued'](),
					running: m['tasks.publication_running'](),
					succeeded: m['tasks.publication_succeeded'](),
					failed: m['tasks.publication_failed'](),
					cancelled: m['tasks.publication_cancelled'](),
					skipped: m['tasks.publication_skipped']()
				}[publication.step.status]
			: ''
	);
	const activity = $derived(chat.taskActivity[task.id]);
	onDestroy(() => {
		disposed = true;
		clearTimeout(refreshTimer);
	});
	function failure(e: unknown) {
		if (disposed) return;
		if ([401, 403, 404].includes((e as { status?: number }).status ?? 0)) ondenied(e);
		else error = m['tasks.load_error']();
	}
	async function refresh() {
		if (loading) {
			dirty = true;
			return;
		}
		loading = true;
		error = '';
		const runID = selectedRun;
		try {
			const [attempts, events, recorded] = await Promise.all([
				agentTasks.runs(chat.slug, task.id),
				agentTasks.history(chat.slug, task.id, history.at(-1)?.sequence ?? 0),
				agentTasks.signals(chat.slug, task.id)
			]);
			if (disposed || chat.unavailable) return;
			signals = recorded;
			const existing = new Map(runs.map((run) => [run.id, run]));
			for (const run of attempts.runs) existing.set(run.id, run);
			runs = [...existing.values()];
			if (!initialRunsLoaded) {
				runsBefore = attempts.next_before;
				initialRunsLoaded = true;
			}
			const known = new Set(history.map((e) => e.sequence));
			history = [...history, ...events.transitions.filter((e) => !known.has(e.sequence))];
			historyAfter = events.next_after;
			if (attempts.runs.some((run) => run.id === runID) || runs.some((run) => run.id === runID)) {
				const [page, check] = await Promise.all([
					agentTasks.logs(chat.slug, task.id, runID, logs.at(-1)?.id ?? 0),
					agentTasks.validation(chat.slug, task.id, runID)
				]);
				if (disposed || chat.unavailable || selectedRun !== runID) return;
				validation = check;
				const ids = new Set(logs.map((line) => line.id));
				logs = [...logs, ...page.logs.filter((line) => !ids.has(line.id))];
				logsAfter = page.next_after;
			}
		} catch (e) {
			failure(e);
		} finally {
			if (!disposed) {
				loading = false;
				if (dirty) {
					dirty = false;
					void refresh();
				}
			}
		}
	}
	$effect(() => {
		if (task.run_id !== seenAttempt) {
			seenAttempt = task.run_id;
			selectedRun = task.run_id;
			logs = [];
			validation = null;
			logsAfter = undefined;
		}
	});
	$effect(() => {
		const run = selectedRun;
		untrack(() => {
			logs = [];
			validation = null;
			logsAfter = undefined;
		});
		if (!run) return;
	});
	$effect(() => {
		const observed = [
			open,
			task.state,
			task.run_id,
			selectedRun,
			activity?.log_id,
			chat.taskValidationCursor[task.id],
			chat.taskSignalCursor[task.id]
		];
		// Throttle from the first event: a continuous stream must not keep
		// postponing output until the native process becomes quiet.
		if (!observed[0]) {
			clearTimeout(refreshTimer);
			refreshTimer = undefined;
			return;
		}
		if (refreshTimer === undefined) {
			refreshTimer = setTimeout(() => {
				refreshTimer = undefined;
				void refresh();
			}, 150);
		}
	});
	// Publication is loaded even while details are collapsed so the review link
	// stays visible; publication events and state changes refresh it.
	$effect(() => {
		const run = task.run_id;
		const observed = [task.state, chat.taskPublicationCursor[task.id]];
		if (!['publishing', 'awaiting_review', 'completed', 'blocked'].includes(observed[0] as string)) {
			untrack(() => (publication = null));
			return;
		}
		untrack(() => void loadPublication(run));
	});
	async function loadPublication(run: string) {
		try {
			const view = await agentTasks.publication(chat.slug, task.id, run);
			if (!disposed && !chat.unavailable && task.run_id === run) publication = view;
		} catch (e) {
			failure(e);
		}
	}
	// The task's machine may expose an application preview for manual QA.
	$effect(() => {
		const machine = task.machine_id;
		if (!machine || !['awaiting_review', 'completed', 'blocked', 'failed'].includes(task.state)) {
			untrack(() => (machinePreview = null));
			return;
		}
		untrack(() => {
			void getMachinePreview(chat.slug, machine)
				.then((value) => {
					if (!disposed && task.machine_id === machine) machinePreview = value;
				})
				.catch(() => (machinePreview = null));
		});
	});
	async function openPreview() {
		if (!task.machine_id || busy) return;
		busy = true;
		const popup = window.open('about:blank', '_blank');
		if (popup) popup.opener = null;
		try {
			const launch = await launchMachinePreview(chat.slug, task.machine_id);
			if (launch.status === 'ready' && launch.launch_url && popup) popup.location.replace(launch.launch_url);
			else {
				popup?.close();
				error = m['machines.preview_not_ready']();
			}
		} catch {
			popup?.close();
			error = m['machines.preview_not_ready']();
		} finally {
			if (!disposed) busy = false;
		}
	}
	async function cancel() {
		if (busy) return;
		busy = true;
		error = '';
		const previous = chat.liveTasks[task.id] ?? null;
		try {
			await agentTasks.cancel(chat.slug, task.id);
			const updated = await agentTasks.get(chat.slug, task.id);
			if (!disposed) chat.acceptTask(updated, false, previous);
		} catch (e) {
			failure(e);
		} finally {
			if (!disposed) busy = false;
		}
	}
	// A terminal in the task's own worktree, docked at the bottom, for working
	// directly beside (or instead of) the agent.
	const dock = useTerminalDock();
	const sessionCommand = $derived(
		resumableStates.includes(task.state) ? agentSessionCommand(task.provider, task.id) : undefined
	);
	function openTerminal(initialInput?: string) {
		if (!task.machine_id || !dock) return;
		dock.open({
			initialInput,
			slug: chat.slug,
			machineId: task.machine_id,
			machineName: chat.issue?.identifier ?? task.machine_id,
			checkoutId: task.checkout_id,
			checkoutLabel: chat.issue ? `${chat.issue.identifier} · ${chat.issue.title}` : undefined,
			sessionName: chat.issue?.identifier
		});
	}

	async function olderRuns() {
		if (!runsBefore || loading) return;
		loading = true;
		try {
			const page = await agentTasks.runs(chat.slug, task.id, runsBefore);
			if (disposed) return;
			const known = new Set(runs.map((run) => run.id));
			runs = [...runs, ...page.runs.filter((run) => !known.has(run.id))];
			runsBefore = page.next_before;
		} catch (e) {
			failure(e);
		} finally {
			if (!disposed) loading = false;
		}
	}
</script>

<!-- A task is a row in the panel's list, not a nested card. -->
<article class="min-w-0 space-y-2 py-3" aria-label={m['tasks.task']()}>
	<div class="flex items-start justify-between gap-3">
		<p class="min-w-0 whitespace-pre-wrap break-words text-sm font-medium line-clamp-2">{task.prompt}</p>
		<Badge
			variant={failed ? 'destructive' : task.state === 'completed' ? 'default' : 'secondary'}
			class="shrink-0"
			role="status">{taskLabel(task.state)}</Badge
		>
	</div>
	<p class="text-xs text-muted-foreground">
		{task.provider}{task.model ? ` · ${task.model}` : ''} · {m['tasks.attempt']({ number: task.generation })}
	</p>
	<Trace.Root>
		<Trace.Line class="items-start" status={failed ? 'error' : active ? 'running' : undefined}
			><Trace.Icon
				>{#if active}<LoaderCircle class="animate-spin" />{:else if failed}<CircleAlert
					/>{:else if task.state === 'completed'}<CircleCheck />{:else}<Circle />{/if}</Trace.Icon
			><Trace.Title class="overflow-visible text-clip whitespace-normal break-words">{task.reason}</Trace.Title
			></Trace.Line
		>
		<Trace.Collapsible bind:open>
			<Trace.CollapsibleTrigger aria-label={m['tasks.details']()}
				><Trace.Line><Trace.Icon><Terminal /></Trace.Icon><Trace.Title>{m['tasks.details']()}</Trace.Title></Trace.Line
				></Trace.CollapsibleTrigger
			>
			<Trace.CollapsibleContent class="space-y-3">
				{#each history as entry (entry.sequence)}<Trace.Line class="items-start"
						><Trace.Icon><CircleCheck /></Trace.Icon><span class="min-w-0 flex-1"
							><Trace.Title class="block">{taskLabel(entry.to_state)}</Trace.Title><Trace.Description
								class="block whitespace-normal break-words">{entry.reason}</Trace.Description
							></span
						></Trace.Line
					>{/each}
				{#if historyAfter}<Button variant="ghost" size="sm" disabled={loading} onclick={() => void refresh()}
						>{m['tasks.more_history']()}</Button
					>{/if}
				{#if runs.length > 1}<SelectField
						bind:value={selectedRun}
						aria-label={m['tasks.execution_attempt']()}
						options={runs.map((run) => ({
							value: run.id,
							label: `${run.provider_id} · ${new Date(run.created_at).toLocaleString()}`
						}))}
					/>{/if}
				{#if runsBefore}<Button variant="ghost" size="sm" disabled={loading} onclick={() => void olderRuns()}
						>{m['tasks.more_runs']()}</Button
					>{/if}
				{#if plan}<AgentPlan steps={plan} />{/if}
				{#if reasoning.length}<AgentReasoning summaries={reasoning} />{/if}
				<TestResults step={validation} output={checkOutput} slug={chat.slug} taskId={task.id} />
				{#if changedFiles.length}<section aria-label={m['tasks.changed_files']()} class="min-w-0 text-sm">
						<p class="mb-1 text-xs font-medium text-muted-foreground">{m['tasks.changed_files']()}</p>
						{#each changedFiles as node (node.path)}<FileTreeNode {node} href={fileLink} />{/each}
					</section>{/if}
				{#if signals.length}<section aria-label={m['tasks.signals']()} class="min-w-0 space-y-1">
						{#each signals as signal (signal.id)}
							{@const note = signalAction[signal.action]}
							<Trace.Line
								class="items-start"
								status={signal.outcome === 'failure' ||
								signal.outcome === 'timed_out' ||
								signal.outcome === 'changes_requested'
									? 'error'
									: undefined}
								><Trace.Icon
									>{#if signal.kind === 'ci'}<CircleCheck />{:else}<GitPullRequest />{/if}</Trace.Icon
								><span class="min-w-0 flex-1"
									><Trace.Title class="block whitespace-normal break-words"
										>{signal.kind === 'ci'
											? m['tasks.signal_ci']({ name: signal.name, outcome: signal.outcome })
											: m['tasks.signal_review']({ name: signal.name, outcome: signal.outcome })}</Trace.Title
									>{#if note}<Trace.Description class="block whitespace-normal break-words">{note()}</Trace.Description
										>{/if}</span
								></Trace.Line
							>
						{/each}
					</section>{/if}
				{#if publication?.step && publication.step.agent_run_id === selectedRun}<Trace.Line
						status={publication.step.status === 'failed'
							? 'error'
							: ['queued', 'running'].includes(publication.step.status)
								? 'running'
								: undefined}
						><Trace.Icon><GitPullRequest /></Trace.Icon><Trace.Title class="whitespace-normal break-words"
							>{publicationLabel}</Trace.Title
						></Trace.Line
					>{/if}
				{#if selected?.summary}<Markdown source={selected.summary} class="text-sm" />{/if}
				{#if activity?.run_id === selectedRun}{#each activity.events.slice(-8) as event, i (i)}<Trace.Line
							><Trace.Title>{event.type}</Trace.Title></Trace.Line
						>{/each}{/if}
				<CodeBlock.Root
					><CodeBlock.Header
						><CodeBlock.Title>{m['tasks.output']()}</CodeBlock.Title><CodeBlock.Actions
							><CodeBlock.CopyButton value={output} aria-label={m['assistant.copy']()} /></CodeBlock.Actions
						></CodeBlock.Header
					><CodeBlock.Content code={output || m['tasks.no_output']()} wrap class="max-h-72 text-xs" /></CodeBlock.Root
				>
				{#if logsAfter}<Button variant="ghost" size="sm" disabled={loading} onclick={() => void refresh()}
						>{m['tasks.more_logs']()}</Button
					>{/if}
				{#if loading}<p role="status" class="text-xs">{m['assistant.loading']()}</p>{/if}
			</Trace.CollapsibleContent>
		</Trace.Collapsible>
	</Trace.Root>
	{#if error}<p role="alert" class="text-sm text-destructive">{error}</p>
		<Button variant="ghost" size="sm" onclick={() => void refresh()}>{m['tasks.refresh']()}</Button>{/if}
	<div class="flex flex-wrap items-center gap-2">
		{#if canCancel}<Button variant="outline" size="sm" disabled={busy} onclick={() => void cancel()}
				>{task.state === 'cancelling' ? m['tasks.retry_cancel']() : m['tasks.cancel']()}</Button
			>{/if}
		{#if canRetry}<Button variant="outline" size="sm" disabled={busy} onclick={() => onretry(task)}
				>{m['tasks.retry']()}</Button
			>{/if}
		{#if pullRequest}<Button variant="outline" size="sm" href={pullRequest} target="_blank" rel="noopener noreferrer"
				><GitPullRequest />{m['tasks.open_pull_request']()}</Button
			>{/if}
		{#if machinePreview}<Button variant="outline" size="sm" disabled={busy} onclick={() => void openPreview()}
				><ExternalLink />{m['machines.preview_open']()}</Button
			>{/if}
		{#if task.machine_id && dock}<Button variant="outline" size="sm" onclick={() => openTerminal()}
				><SquareTerminal />{m['tasks.agents_terminal']()}</Button
			>{#if sessionCommand}<Button
					variant="outline"
					size="sm"
					title={m['tasks.agent_session_help']()}
					onclick={() => openTerminal(sessionCommand)}><Bot />{m['tasks.agent_session']()}</Button
				>{/if}{/if}
		{#if task.machine_id}<a
				class="inline-flex items-center gap-1 text-xs underline underline-offset-2"
				href="/{chat.slug}/machines/{task.machine_id}"><ExternalLink size={12} />{m['tasks.environment']()}</a
			>{/if}
	</div>
</article>
