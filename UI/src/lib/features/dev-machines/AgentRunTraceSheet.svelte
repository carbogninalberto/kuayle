<script lang="ts">
	import { onDestroy } from 'svelte';
	import * as Sheet from '$lib/components/ui/sheet';
	import { Button } from '$lib/components/ui/button';
	import { getAgentRunTrace, cancelAgentRun } from '$lib/api/dev-machines';
	import type { AgentRun, AgentRunTrace, DevMachine, DevMachineCheckout, DevMachineEvent, DevMachineLogChunk, AgentRunStep } from '$lib/types/dev-machine';
	import { appToast } from '$lib/features/toast/toast';
	import { safeGitHubPullRequestUrl } from '$lib/security/github-url';
	import { appendRecentTelemetry, DEV_MACHINE_EVENT_RETENTION, DEV_MACHINE_LOG_RETENTION } from './telemetry-retention';
	import { Bot, ExternalLink, X, RotateCw, Loader } from 'lucide-svelte';

	let { open = $bindable(false), slug, runId, machine, checkouts, onclose }: {
		open: boolean;
		slug: string;
		runId: string | null;
		machine: DevMachine | null;
		checkouts: DevMachineCheckout[];
		onclose?: () => void;
	} = $props();

	let run = $state<AgentRun | null>(null);
	let steps = $state<AgentRunStep[]>([]);
	let events = $state<DevMachineEvent[]>([]);
	let eventsLimited = $state(false);
	let logs = $state<DevMachineLogChunk[]>([]);
	let logsLimited = $state(false);
	let loading = $state(false);
	let failed = $state(false);
	let eventsAfterId = 0;
	let logsAfterId = 0;
	let cancelBusy = $state(false);
	let timer: ReturnType<typeof setTimeout> | undefined;
	let pollingVersion = 0;
	let requestVersion = 0;

	const terminalStatuses = new Set(['succeeded', 'failed', 'cancelled', 'timeout']);
	const isTerminal = $derived(run ? terminalStatuses.has(run.status) : true);
	const pullRequestUrl = $derived(run ? safeGitHubPullRequestUrl(run.pull_request_url, repositoryFullNameForRun(run)) : null);

	function repositoryFullNameForRun(agentRun: AgentRun): string {
		if (agentRun.checkout_id) {
			return checkouts.find((checkout) => checkout.id === agentRun.checkout_id)?.repository_full_name ?? '';
		}
		if (machine?.repo_owner && machine.repo_name) return `${machine.repo_owner}/${machine.repo_name}`;
		return '';
	}

	$effect(() => {
		const currentOpen = open;
		const currentSlug = slug;
		const currentRunId = runId;
		const version = ++requestVersion;
		clearPoll();
		if (!currentOpen || !currentRunId) return;
		resetTrace();
		void loadTrace(currentSlug, currentRunId, version);
		return () => {
			if (requestVersion === version) requestVersion++;
			clearPoll();
		};
	});

	onDestroy(() => {
		requestVersion++;
		clearPoll();
	});

	function clearPoll() {
		if (timer) clearTimeout(timer);
		timer = undefined;
	}

	function resetTrace() {
		run = null;
		steps = [];
		events = [];
		eventsLimited = false;
		logs = [];
		logsLimited = false;
		eventsAfterId = 0;
		logsAfterId = 0;
		loading = false;
		failed = false;
		cancelBusy = false;
		pollingVersion = 0;
	}

	function handleClose() {
		open = false;
		requestVersion++;
		pollingVersion = 0;
		clearPoll();
		onclose?.();
	}

	async function loadTrace(currentSlug = slug, currentRunId = runId, version = requestVersion) {
		if (!currentRunId) return;
		loading = true;
		failed = false;
		try {
			await fetchAvailableTrace(currentSlug, currentRunId, version);
		} catch (error) {
			if (version !== requestVersion) return;
			failed = true;
			appToast.apiError(error, 'Failed to load agent run trace');
		} finally {
			if (version === requestVersion) {
				loading = false;
				schedulePoll(currentSlug, currentRunId, version);
			}
		}
	}

	async function fetchAvailableTrace(currentSlug: string, currentRunId: string, version: number) {
		let hasMore = false;
		do {
			const trace = await getAgentRunTrace(currentSlug, currentRunId, eventsAfterId, 200, logsAfterId, 500);
			if (version !== requestVersion || !open || slug !== currentSlug || runId !== currentRunId) return;
			applyTrace(trace);
			hasMore = trace.has_more_events || trace.has_more_logs;
		} while (hasMore);
	}

	function applyTrace(trace: AgentRunTrace) {
		run = trace.run;
		steps = trace.steps ?? [];
		const retainedEvents = appendRecentTelemetry(events, trace.events ?? [], DEV_MACHINE_EVENT_RETENTION);
		events = retainedEvents.items;
		eventsLimited ||= retainedEvents.dropped > 0;
		eventsAfterId = Math.max(eventsAfterId, trace.next_event_id ?? 0, ...(trace.events ?? []).map((event) => event.id));
		const retainedLogs = appendRecentTelemetry(logs, trace.logs ?? [], DEV_MACHINE_LOG_RETENTION);
		logs = retainedLogs.items;
		logsLimited ||= retainedLogs.dropped > 0;
		logsAfterId = Math.max(logsAfterId, trace.next_log_id ?? 0, ...(trace.logs ?? []).map((log) => log.id));
	}

	function schedulePoll(currentSlug: string, currentRunId: string, version: number, delay = 1000) {
		clearPoll();
		if (version !== requestVersion || !open || slug !== currentSlug || runId !== currentRunId || !run || terminalStatuses.has(run.status)) return;
		timer = setTimeout(() => void poll(currentSlug, currentRunId, version), delay);
	}

	async function poll(currentSlug = slug, currentRunId = runId, version = requestVersion) {
		if (pollingVersion === version || !currentRunId || version !== requestVersion || slug !== currentSlug) return;
		pollingVersion = version;
		try {
			await fetchAvailableTrace(currentSlug, currentRunId, version);
		} catch {
			// Silent background poll
		} finally {
			if (version !== requestVersion || pollingVersion !== version) return;
			pollingVersion = 0;
			schedulePoll(currentSlug, currentRunId, version);
		}
	}

	async function doCancel() {
		if (!runId || cancelBusy) return;
		const currentSlug = slug;
		const currentRunId = runId;
		let version = requestVersion;
		cancelBusy = true;
		try {
			await cancelAgentRun(currentSlug, currentRunId);
			if (version !== requestVersion || slug !== currentSlug || runId !== currentRunId) return;
			version = ++requestVersion;
			pollingVersion = 0;
			clearPoll();
			appToast.success('Run cancelled');
			await loadTrace(currentSlug, currentRunId, version);
		} catch (error) {
			if (version !== requestVersion || slug !== currentSlug || runId !== currentRunId) return;
			appToast.apiError(error, 'Failed to cancel run');
		} finally {
			if (version === requestVersion && slug === currentSlug && runId === currentRunId) cancelBusy = false;
		}
	}

	function formatTimestamp(ts?: string) {
		return ts ? new Date(ts).toLocaleString() : '--';
	}

	function statusClass(status: string) {
		const base = 'inline-flex rounded-full px-2 py-0.5 text-[10px] font-semibold uppercase';
		if (status === 'succeeded') return `${base} bg-green-500/15 text-green-400`;
		if (status === 'failed' || status === 'timeout') return `${base} bg-red-500/15 text-red-400`;
		if (status === 'cancelled') return `${base} bg-zinc-500/15 text-zinc-400`;
		if (status === 'running') return `${base} bg-blue-500/15 text-blue-400`;
		if (status === 'waiting_input') return `${base} bg-yellow-500/15 text-yellow-400`;
		return `${base} bg-zinc-500/15 text-zinc-400`;
	}

	function safeJSON(value: unknown): string {
		if (typeof value === 'object' && value !== null) {
			try { return JSON.stringify(value, null, 2); } catch { return String(value); }
		}
		return String(value ?? '');
	}
</script>

<Sheet.Root bind:open onOpenChange={(v) => { if (!v) handleClose(); }}>
	<Sheet.Content side="right" class="w-[28rem] max-w-[90vw] sm:w-[32rem] lg:w-[36rem] border-[var(--app-border)] bg-[var(--color-bg-secondary)]">
		<div class="flex h-full flex-col">
			<Sheet.Header class="flex-shrink-0 border-b border-[var(--app-border)] px-5 py-3">
				<div class="flex items-center justify-between">
					<div class="flex items-center gap-2">
						<Bot size={16} />
						<Sheet.Title class="text-sm font-semibold">Agent Run Trace</Sheet.Title>
					</div>
					<div class="flex items-center gap-1">
						{#if run && !isTerminal}
							<Button variant="ghost" size="icon-sm" onclick={() => void poll()} title="Refresh"><RotateCw size={14} /></Button>
						{/if}
						<Button variant="ghost" size="icon-sm" onclick={handleClose} title="Close"><X size={16} /></Button>
					</div>
				</div>
			</Sheet.Header>

			<div class="min-h-0 flex-1 overflow-y-auto p-5 space-y-5">
				{#if loading && !run}
					<div class="flex items-center justify-center py-12">
						<Loader size={24} class="animate-spin text-[var(--color-text-tertiary)]" />
					</div>
				{:else if failed && !run}
					<div class="text-center text-sm text-red-400 py-12">
						Failed to load trace data.
						<Button variant="outline" size="xs" class="mt-2" onclick={() => void loadTrace()}>Retry</Button>
					</div>
				{:else if run}
					<!-- Status overview -->
					<section class="space-y-2">
						<div class="flex items-center justify-between">
							<span class="text-xs font-medium">{run.provider_id} · {run.mode}</span>
							<span class={statusClass(run.status)}>{run.status.replace('_', ' ')}</span>
						</div>
						{#if !isTerminal}
							<span class="inline-flex items-center gap-1 rounded-full border border-blue-500/30 bg-blue-500/10 px-2 py-0.5 text-[10px] font-semibold uppercase text-blue-400">
								<Loader size={10} class="animate-spin" /> Running
							</span>
						{/if}
						<p class="text-[10px] text-[var(--color-text-tertiary)]">
							Created: {formatTimestamp(run.created_at)}<br />
							{#if run.started_at}Started: {formatTimestamp(run.started_at)}<br />{/if}
							{#if run.completed_at}Completed: {formatTimestamp(run.completed_at)}{/if}
						</p>
					</section>

					<!-- Prompt -->
					{#if run.prompt}
						<section>
							<h3 class="text-[10px] font-semibold uppercase tracking-wider text-[var(--color-text-tertiary)]">Prompt</h3>
							<pre class="mt-1 whitespace-pre-wrap rounded-md bg-black/20 p-3 text-[11px] leading-relaxed text-zinc-300 max-h-40 overflow-y-auto">{run.prompt}</pre>
						</section>
					{/if}

					<!-- Result / Summary -->
					{#if run.summary}
						<section>
							<h3 class="text-[10px] font-semibold uppercase tracking-wider text-[var(--color-text-tertiary)]">Summary</h3>
							<p class="mt-1 text-xs text-[var(--color-text-secondary)]">{run.summary}</p>
						</section>
					{/if}

					<!-- Error -->
					{#if run.error_message}
						<section>
							<h3 class="text-[10px] font-semibold uppercase tracking-wider text-red-400">Error</h3>
							<pre class="mt-1 whitespace-pre-wrap rounded-md bg-red-500/10 p-3 text-[11px] leading-relaxed text-red-300 max-h-32 overflow-y-auto">{run.error_message}</pre>
						</section>
					{/if}

					<!-- Changed files -->
					{#if run.changed_files?.length}
						<section>
							<h3 class="text-[10px] font-semibold uppercase tracking-wider text-[var(--color-text-tertiary)]">Changed Files ({run.changed_files.length})</h3>
							<ul class="mt-1 space-y-0.5">
								{#each run.changed_files as file}
									<li class="text-[11px] text-[var(--color-text-secondary)] font-mono">{file}</li>
								{/each}
							</ul>
						</section>
					{/if}

					<!-- Commits -->
					{#if run.commits?.length}
						<section>
							<h3 class="text-[10px] font-semibold uppercase tracking-wider text-[var(--color-text-tertiary)]">Commits ({run.commits.length})</h3>
							<ul class="mt-1 space-y-0.5">
								{#each run.commits as commit}
									<li class="text-[11px] text-[var(--color-text-secondary)] font-mono">{commit}</li>
								{/each}
							</ul>
						</section>
					{/if}

					<!-- Tests -->
					{#if run.tests_run?.length || run.test_status !== 'not_run'}
						<section>
							<h3 class="text-[10px] font-semibold uppercase tracking-wider text-[var(--color-text-tertiary)]">Tests · <span class={run.test_status === 'passed' ? 'text-green-400' : run.test_status === 'failed' ? 'text-red-400' : ''}>{run.test_status}</span></h3>
							{#if run.tests_run?.length}
								<ul class="mt-1 space-y-0.5">
									{#each run.tests_run as test}
										<li class="text-[11px] text-[var(--color-text-secondary)]">{test}</li>
									{/each}
								</ul>
							{/if}
						</section>
					{/if}

					<!-- PR -->
					{#if pullRequestUrl}
						<section>
							<a href={pullRequestUrl} target="_blank" rel="noopener noreferrer" class="inline-flex items-center gap-1 text-xs text-[var(--app-accent)] hover:underline">
								Pull Request <ExternalLink size={11} />
							</a>
						</section>
					{/if}

					<!-- Risk notes -->
					{#if run.risk_notes?.length}
						<section>
							<h3 class="text-[10px] font-semibold uppercase tracking-wider text-amber-400">Risk Notes</h3>
							<ul class="mt-1 space-y-0.5">
								{#each run.risk_notes as note}
									<li class="text-[11px] text-amber-400/80">{note}</li>
								{/each}
							</ul>
						</section>
					{/if}

					<!-- Steps -->
					{#if steps.length}
						<section>
							<h3 class="text-[10px] font-semibold uppercase tracking-wider text-[var(--color-text-tertiary)]">Steps ({steps.length})</h3>
							<div class="mt-1 space-y-1">
								{#each steps as step}
									<div class="flex items-start gap-2 rounded border border-[var(--app-border)] p-2">
										<span class={statusClass(step.status)}>{step.status}</span>
										<div class="min-w-0 text-[11px]">
											<p class="font-medium">{step.name}</p>
											<p class="text-[var(--color-text-tertiary)]">{step.step_type} · seq {step.sequence}</p>
											{#if step.summary}<p class="mt-0.5 text-zinc-400">{step.summary}</p>{/if}
											{#if step.exit_code != null}<p class="text-zinc-500">exit {step.exit_code}</p>{/if}
										</div>
									</div>
								{/each}
							</div>
						</section>
					{/if}

					<!-- Events -->
					{#if events.length}
						<section>
							<h3 class="text-[10px] font-semibold uppercase tracking-wider text-[var(--color-text-tertiary)]">Events ({events.length})</h3>
							{#if eventsLimited}<p class="mt-1 text-[10px] text-[var(--color-text-tertiary)]" data-testid="trace-events-retention">Showing the latest {DEV_MACHINE_EVENT_RETENTION} entries; older events are omitted from this live view.</p>{/if}
							<div class="mt-1 space-y-1 max-h-64 overflow-y-auto">
								{#each events.toReversed() as event}
									<div class="rounded border border-[var(--app-border)] p-2">
										<div class="flex items-start justify-between gap-2">
											<p class="text-[11px] font-medium">{event.event_type.replaceAll('_', ' ')}</p>
											<span class="shrink-0 text-[10px] text-[var(--color-text-tertiary)]">{new Date(event.occurred_at).toLocaleTimeString()}</span>
										</div>
										<p class="text-[10px] text-[var(--color-text-tertiary)]">{event.source}</p>
										{#if Object.keys(event.payload ?? {}).length > 0}
											<pre class="mt-1 overflow-x-auto whitespace-pre-wrap rounded bg-black/20 p-2 text-[10px] text-zinc-400 max-h-24">{safeJSON(event.payload)}</pre>
										{/if}
									</div>
								{/each}
							</div>
						</section>
					{/if}

					<!-- Logs -->
					{#if logs.length}
						<section>
							<h3 class="text-[10px] font-semibold uppercase tracking-wider text-[var(--color-text-tertiary)]">Logs ({logs.length})</h3>
							{#if logsLimited}<p class="mt-1 text-[10px] text-[var(--color-text-tertiary)]" data-testid="trace-logs-retention">Showing the latest {DEV_MACHINE_LOG_RETENTION} chunks; older logs are omitted from this live view.</p>{/if}
							<pre class="mt-1 max-h-64 overflow-auto whitespace-pre-wrap rounded-md bg-black/30 p-3 text-[11px] leading-relaxed text-zinc-300 font-mono">{logs.map((chunk) => `[${chunk.stream}] ${chunk.content}`).join('')}</pre>
						</section>
					{/if}

					<!-- Cancel button for active runs -->
					{#if !isTerminal}
						<div class="pt-2">
							<Button variant="destructive" size="sm" disabled={cancelBusy} onclick={doCancel} class="w-full">
								{cancelBusy ? 'Cancelling...' : 'Cancel run'}
							</Button>
						</div>
					{/if}
				{/if}
			</div>
		</div>
	</Sheet.Content>
</Sheet.Root>
