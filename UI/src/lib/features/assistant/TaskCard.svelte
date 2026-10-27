<script lang="ts">
	import { onDestroy, untrack } from 'svelte';
	import TestResults from './TestResults.svelte';
	import { Circle, CircleCheck, CircleAlert, LoaderCircle, Terminal, ExternalLink } from '@lucide/svelte';
	import * as Trace from '$lib/components/ui/trace/index.js';
	import * as CodeBlock from '$lib/components/ui/code-block/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import SelectField from '$lib/components/shared/SelectField.svelte';
	import { agentTasks, type AgentTask, type TaskTransition, type ValidationStep } from '$lib/api/agent-tasks';
	import type { AgentRun, DevMachineLogChunk } from '$lib/types/dev-machine';
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
			const [attempts, events] = await Promise.all([
				agentTasks.runs(chat.slug, task.id),
				agentTasks.history(chat.slug, task.id, history.at(-1)?.sequence ?? 0)
			]);
			if (disposed || chat.unavailable) return;
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
		const observed = [open, task.state, task.run_id, selectedRun, activity?.log_id, chat.taskValidationCursor[task.id]];
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

<article class="min-w-0 space-y-3 rounded-lg border bg-background p-3" aria-label={m['tasks.task']()}>
	<div class="flex items-start justify-between gap-3">
		<p class="min-w-0 whitespace-pre-wrap break-words text-sm font-medium line-clamp-3">{task.prompt}</p>
		<span class="shrink-0 rounded-md bg-muted px-2 py-1 text-xs" role="status">{taskLabel(task.state)}</span>
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
				<TestResults step={validation} output={checkOutput} />
				{#if selected?.summary}<p class="whitespace-pre-wrap break-words text-sm">{selected.summary}</p>{/if}
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
		{#if task.machine_id}<a
				class="inline-flex items-center gap-1 text-xs underline underline-offset-2"
				href="/{chat.slug}/machines/{task.machine_id}"><ExternalLink size={12} />{m['tasks.environment']()}</a
			>{/if}
	</div>
</article>
