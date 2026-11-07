<script lang="ts">
	import { untrack } from 'svelte';
	import { ExternalLink, RefreshCw } from '@lucide/svelte';
	import { Badge } from '$lib/components/ui/badge/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { workflows, type TaskWorkflow, type WorkflowDecision } from '$lib/api/workflows';
	import { appToast } from '$lib/features/toast/toast';
	import { m } from '$lib/paraglide/messages.js';
	import WorkflowCanvas from './WorkflowCanvas.svelte';
	import { conditionLabel, statusLabel } from './labels';
	import { pendingDecision, runOverlay } from './workflow-graph';

	// A task's workflow run: the graph with the current step highlighted, the
	// path taken with visit counts and why each transition fired, and the
	// decisions a person can make (an approval, or a reached limit). The
	// server decides who may decide; the view only offers what it allows.
	let {
		slug,
		taskId,
		refreshKey = '',
		compact = false
	}: {
		slug: string;
		taskId: string;
		/** Changes when the task moves (state, iteration, workflow step) so the run reloads. */
		refreshKey?: string;
		compact?: boolean;
	} = $props();

	// Statuses after which the run no longer changes by itself.
	const SETTLED = new Set(['done', 'failed', 'cancelled', 'accepted', 'merged', 'completed']);

	let run = $state<TaskWorkflow | null>(null);
	let failed = $state(false);
	let deciding = $state(false);
	let request = 0;

	async function load() {
		const current = ++request;
		try {
			const loaded = await workflows.task(slug, taskId);
			if (current !== request) return;
			run = loaded;
			failed = false;
		} catch {
			if (current === request) failed = true;
		}
	}

	$effect(() => {
		void [slug, taskId, refreshKey];
		untrack(() => void load());
	});

	// Waiting steps (CI, a review, a timeout) move without a task event the
	// view sees, so poll modestly until the run settles.
	$effect(() => {
		if (!run || SETTLED.has(run.status)) return;
		const timer = setInterval(() => void load(), 15000);
		return () => clearInterval(timer);
	});

	const overlay = $derived(run ? runOverlay(run) : null);
	const decision = $derived(run ? pendingDecision(run) : null);
	const nodeLabel = (id: string | null | undefined) =>
		(id && run?.graph.nodes.find((n) => n.id === id)?.label) || id || '';
	const steps = $derived(run ? [...run.steps].sort((a, b) => a.sequence - b.sequence) : []);

	async function decide(choice: WorkflowDecision) {
		if (!decision || deciding) return;
		deciding = true;
		try {
			await workflows.decide(slug, taskId, decision.sequence, choice);
			appToast.success(m['workflows.task.decided']());
		} catch (error) {
			appToast.apiError(error, m['workflows.task.decision_failed']());
		} finally {
			deciding = false;
			await load();
		}
	}
</script>

<section class="min-w-0 space-y-3" aria-labelledby="task-workflow-{taskId}" data-testid="task-workflow">
	{#if failed && !run}
		<div class="flex items-center justify-between gap-2" role="alert">
			<p class="text-sm text-destructive">{m['workflows.task.load_failed']()}</p>
			<Button variant="ghost" size="sm" onclick={() => void load()}><RefreshCw />{m['tasks.refresh']()}</Button>
		</div>
	{:else if run}
		<div class="flex flex-wrap items-center justify-between gap-2">
			<div class="min-w-0">
				<h3 id="task-workflow-{taskId}" class="truncate text-sm font-medium">
					{m['workflows.task.heading']({
						name: run.builtin ? m['workflows.standard']() : run.name,
						version: run.version
					})}
				</h3>
				<p class="flex flex-wrap items-center gap-1.5 text-xs text-muted-foreground" aria-live="polite">
					{#if statusLabel(run.status)}<Badge variant="outline" data-testid="task-workflow-status"
							>{statusLabel(run.status)}</Badge
						>{/if}
					<span>{m['workflows.task.current']()}: {nodeLabel(run.node)}</span>
					<span>· {m['workflows.task.iterations']({ count: run.iterations })}</span>
				</p>
			</div>
			<Button
				variant="ghost"
				size="sm"
				href="/{slug}/settings/workflows/{encodeURIComponent(run.workflow_id)}"
				aria-label={m['workflows.open_named']({ name: run.builtin ? m['workflows.standard']() : run.name })}
				><ExternalLink />{m['workflows.task.view']()}</Button
			>
		</div>

		{#if run.builtin}<p class="text-xs text-muted-foreground">{m['workflows.task.standard_note']()}</p>{/if}

		{#if decision}
			<div class="space-y-2 rounded-lg border border-primary/40 bg-primary/5 p-3" role="group" aria-label={nodeLabel(run.node)} data-testid="task-workflow-decision">
				<p class="text-sm">
					{decision.kind === 'approval'
						? m['workflows.task.waiting_approval']({ question: decision.question || nodeLabel(run.node) })
						: m['workflows.task.paused_note']()}
				</p>
				<div class="flex flex-wrap gap-2">
					{#if decision.kind === 'approval'}
						<Button size="sm" disabled={deciding} onclick={() => decide('approve')}>{m['workflows.task.approve']()}</Button>
						<Button size="sm" variant="outline" disabled={deciding} onclick={() => decide('deny')}
							>{m['workflows.task.deny']()}</Button
						>
					{:else}
						<Button size="sm" disabled={deciding} onclick={() => decide('continue')}
							>{m['workflows.task.continue']()}</Button
						>
						<Button size="sm" variant="outline" disabled={deciding} onclick={() => decide('stop')}
							>{m['workflows.task.stop']()}</Button
						>
					{/if}
				</div>
			</div>
		{/if}

		{#if overlay}<WorkflowCanvas
				graph={run.graph}
				profiles={run.profiles}
				run={overlay}
				label={m['workflows.task.title']()}
				class={compact ? 'h-72' : 'h-[420px]'}
			/>{/if}

		{#if steps.length > 0}
			<section aria-labelledby="task-workflow-path-{taskId}" class="space-y-1">
				<h4 id="task-workflow-path-{taskId}" class="text-xs font-medium text-muted-foreground uppercase">
					{m['workflows.task.path']()}
				</h4>
				<ol class="max-h-80 space-y-1.5 overflow-y-auto pr-1" data-testid="task-workflow-path">
					{#each steps as step (step.sequence)}
						<li
							class="rounded-md border px-2.5 py-1.5 text-xs {step.sequence === run.sequence &&
							step.node_id === run.node
								? 'border-primary/60 bg-primary/5'
								: ''}"
							aria-current={step.sequence === run.sequence && step.node_id === run.node ? 'step' : undefined}
						>
							<div class="flex flex-wrap items-center gap-1.5">
								<span class="font-medium"
									>{m['workflows.task.step_title']({ sequence: step.sequence, node: nodeLabel(step.node_id) })}</span
								>
								{#if step.generation}<Badge variant="secondary" class="h-4 px-1.5 text-[10px] font-normal"
										>{m['tasks.attempt']({ number: step.generation })} · {m['workflows.task.iteration_reason']({
											node: nodeLabel(step.node_id),
											reason: step.condition ? conditionLabel(step.condition) : nodeLabel(step.from_node)
										})}</Badge
									>{/if}
								{#if (run.visits[step.node_id] ?? 0) > 1}<span class="text-muted-foreground"
										>{m['workflows.task.visits']({ count: step.visit })}</span
									>{/if}
							</div>
							{#if step.from_node && step.condition}<p class="text-muted-foreground">
									{nodeLabel(step.from_node)} → {nodeLabel(step.node_id)} · {conditionLabel(step.condition)}
								</p>{/if}
							{#if step.reason}<p class="break-words whitespace-pre-wrap">{step.reason}</p>{/if}
							{#if step.outcome}<p class="text-muted-foreground">
									{m['workflows.task.outcome']({ outcome: conditionLabel(step.outcome) })}
								</p>{/if}
							{#if step.decided_by}<p class="text-muted-foreground">{m['workflows.task.decided_by']()}</p>{/if}
						</li>
					{/each}
				</ol>
			</section>
		{/if}
	{:else}
		<p class="text-xs text-muted-foreground">{m['assistant.loading']()}</p>
	{/if}
</section>
