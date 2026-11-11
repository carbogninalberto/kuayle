<script lang="ts">
	import { untrack } from 'svelte';
	import { ExternalLink, RefreshCw } from '@lucide/svelte';
	import { Badge } from '$lib/components/ui/badge/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import type { AgentTask } from '$lib/api/agent-tasks';
	import { workflows, type WorkflowDecision } from '$lib/api/workflows';
	import type { ConversationState } from '$lib/features/assistant/conversation-state.svelte';
	import { useTaskRun } from '$lib/features/assistant/task-run.svelte';
	import { appToast } from '$lib/features/toast/toast';
	import { m } from '$lib/features/i18n/messages';
	import WorkflowCanvas from './WorkflowCanvas.svelte';
	import { statusLabel } from './labels';
	import { pendingDecision, runOverlay } from './workflow-graph';

	// A task's workflow run: the graph with the current step highlighted and
	// the decisions a person can make (an approval, or a reached limit). The
	// server decides who may decide; the view only offers what it allows. The
	// path the task took is the task's own (TaskPath). The run is the task's
	// shared one (useTaskRun): it follows the task's events and is polled
	// once for every view that shows it. Mounted per task.
	let {
		chat,
		task,
		compact = false
	}: {
		chat: ConversationState;
		task: AgentTask;
		compact?: boolean;
	} = $props();
	const slug = $derived(chat.slug);
	const taskId = $derived(task.id);

	const shared = useTaskRun(untrack(() => chat), () => task, { chart: true });
	const run = $derived(shared.view);
	const failed = $derived(shared.followsWorkflow ? shared.failed : shared.chartFailed);
	const load = () => shared.load();
	let deciding = $state(false);

	const overlay = $derived(run ? runOverlay(run) : null);
	const decision = $derived(run ? pendingDecision(run) : null);
	const nodeLabel = (id: string | null | undefined) =>
		(id && run?.graph.nodes.find((n) => n.id === id)?.label) || id || '';

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
	{:else}
		<p class="text-xs text-muted-foreground">{m['assistant.loading']()}</p>
	{/if}
</section>
