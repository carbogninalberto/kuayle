<script lang="ts">
	import { untrack } from 'svelte';
	import { ExternalLink, RefreshCw } from '@lucide/svelte';
	import { Badge } from '$lib/components/ui/badge/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import * as Dialog from '$lib/components/ui/dialog/index.js';
	import type { AgentTask } from '$lib/api/agent-tasks';
	import { workflows, type TaskWorkflow, type WorkflowDecision } from '$lib/api/workflows';
	import type { ConversationState } from '$lib/features/assistant/conversation-state.svelte';
	import { roomBlocker } from '$lib/features/assistant/task-room';
	import RoomBlockerNotice from '$lib/features/assistant/RoomBlockerNotice.svelte';
	import { useTaskRun } from '$lib/features/assistant/task-run.svelte';
	import { appToast } from '$lib/features/toast/toast';
	import { m } from '$lib/features/i18n/messages';
	import PipelineAgentOverview from './PipelineAgentOverview.svelte';
	import PipelineRunDetails from './PipelineRunDetails.svelte';
	import RunChart from './RunChart.svelte';
	import { taskRunChart } from './task-run-chart';
	import { statusLabel } from './labels';
	import { pendingDecision } from './workflow-graph';

	// A task's workflow run: the graph with the current step highlighted and
	// the decisions a person can make (an approval, or a reached limit). The
	// server decides who may decide; the view only offers what it allows. The
	// path the task took is the task's own (TaskPath). The run is the task's
	// shared one (useTaskRun): it follows the task's events and is polled
	// once for every view that shows it. Mounted per task.
	let {
		chat,
		task,
		compact = false,
		onopenworkflow,
		onopenpost
	}: {
		chat: ConversationState;
		task: AgentTask;
		compact?: boolean;
		onopenworkflow?: () => void;
		onopenpost?: () => void;
	} = $props();
	const slug = $derived(chat.slug);
	const taskId = $derived(task.id);

	const shared = useTaskRun(untrack(() => chat), () => task, { chart: true, room: true });
	const run = $derived(shared.view);
	const failed = $derived(shared.followsWorkflow ? shared.failed : shared.chartFailed);
	const load = () => shared.load();
	let deciding = $state(false);

	const pipeline = $derived(shared.pipeline);
	const chart = $derived(run ? taskRunChart(run) : null);
	const overlay = $derived(chart?.overlay);
	const decision = $derived(run ? pendingDecision(run) : null);
	const blocker = $derived(pipeline && decision?.kind === 'limit' ? roomBlocker(chat.taskRooms[taskId] ?? []) : undefined);
	const nodeLabel = (id: string | null | undefined) =>
		(id && (pipeline?.phases ?? run?.graph.nodes)?.find((node) => node.id === id)?.label) || id || '';

	const name = $derived(run ? (run.builtin ? m['workflows.standard']() : run.name) : '');
	let expanded = $state(false);

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

{#snippet decisionCard(run: TaskWorkflow)}
	{#if decision}
		<div class="space-y-2 rounded-lg border border-primary/40 bg-primary/5 p-3" role="group" aria-label={nodeLabel(run.node)} data-testid="task-workflow-decision">
			{#if blocker}
				<RoomBlockerNotice post={blocker} {chat} onfollow={onopenpost} />
			{:else}<p class="text-sm">
				{decision.kind === 'approval'
					? m['workflows.task.waiting_approval']({ question: decision.question || nodeLabel(run.node) })
					: m['workflows.task.paused_note']()}
			</p>{/if}
			{#if decision.kind === 'approval' && run.ship_merge}
				<!-- The ship approval: approving merges (KUA-209, KUA-216). -->
				<p class="text-xs text-muted-foreground" data-testid="task-workflow-ship-merge">
					{m['workflows.task.ship_merge_note']()}
				</p>
			{/if}
			<div class="mobile-touch-targets flex flex-wrap gap-2">
				{#if decision.kind === 'approval'}
					<Button size="sm" disabled={deciding} onclick={() => decide('approve')}>{m['workflows.task.approve']()}</Button>
					{#if !shared.pipeline}<Button size="sm" variant="outline" disabled={deciding} onclick={() => decide('deny')}
							>{m['workflows.task.deny']()}</Button
						>{/if}
				{:else}
					<Button size="sm" variant={blocker ? 'outline' : 'default'} disabled={deciding} onclick={() => decide('continue')}
						>{m['workflows.task.continue']()}</Button
					>
					<Button size="sm" variant="outline" disabled={deciding} onclick={() => decide('stop')}
						>{m['workflows.task.stop']()}</Button
					>
				{/if}
			</div>
		</div>
	{/if}
{/snippet}

<section class="min-w-0 space-y-3" aria-labelledby="task-workflow-{taskId}" data-testid="task-workflow">
	{#if failed && !run}
		<div class="flex items-center justify-between gap-2" role="alert">
			<p class="text-sm text-destructive">{m['workflows.task.load_failed']()}</p>
			<Button variant="ghost" size="sm" onclick={() => void load()}><RefreshCw />{m['tasks.refresh']()}</Button>
		</div>
	{:else if run && pipeline && onopenworkflow}
		<PipelineAgentOverview
			{run}
			{pipeline}
			{task}
			room={chat.taskRooms[taskId] ?? []}
			{onopenworkflow}
		>
			{@render decisionCard(run)}
		</PipelineAgentOverview>
		{#if shared.roomFailed}
			<div class="flex items-center justify-between gap-2" role="alert">
				<p class="text-sm text-destructive">{m['tasks.load_error']()}</p>
				<Button variant="ghost" size="sm" onclick={() => void shared.loadRoom()}><RefreshCw />{m['tasks.refresh']()}</Button>
			</div>
		{/if}
	{:else if run}
		<div class="flex flex-wrap items-center justify-between gap-2">
			<div class="min-w-0">
				<h3 id="task-workflow-{taskId}" class="truncate text-sm font-medium">
					{m['workflows.task.heading']({
						name,
						version: run.version
					})}
				</h3>
				<p class="flex flex-wrap items-center gap-1.5 text-xs text-muted-foreground" aria-live="polite">
					{#if statusLabel(run.status)}<Badge variant="outline" data-testid="task-workflow-status"
							>{statusLabel(run.status)}</Badge
						>{/if}
				{#if overlay?.current}
					<span>{m['workflows.task.current']()}: {nodeLabel(overlay.current)}</span>
				{/if}
				<span>{overlay?.current ? '· ' : ''}{m['workflows.task.iterations']({ count: run.iterations })}</span>
				</p>
			</div>
			<Button
				variant="ghost"
				size="sm"
				href="/{slug}/settings/workflows/{encodeURIComponent(run.workflow_id)}"
				aria-label={m['workflows.open_named']({ name })}
				><ExternalLink />{m['workflows.task.view']()}</Button
			>
		</div>

		{#if run.builtin}<p class="text-xs text-muted-foreground">{m['workflows.task.standard_note']()}</p>{/if}

		{@render decisionCard(run)}

		{#if overlay && chart}
			<!-- No minimap in the side panel's short chart: it would cover most of it. -->
			<RunChart
				graph={chart.graph}
				{pipeline}
				profiles={run.profiles}
				run={overlay}
				label={m['workflows.task.title']()}
				zoom={compact ? 0.8 : 1}
				minimap={!compact}
				class={compact ? 'h-64' : 'h-80'}
				testid="task-workflow-chart"
				onexpand={() => (expanded = true)}
			/>
			<!-- Closing (Esc or the close button) returns focus to Expand. -->
			<Dialog.Root bind:open={expanded}>
				<Dialog.Content
					class="mobile-touch-targets motion-reduce:animate-none! top-0 left-0 flex h-dvh w-screen max-w-none translate-x-0 flex-col rounded-none sm:max-w-none"
				>
					<Dialog.Title class="pr-10">{m['workflows.task.expanded_title']({ name })}</Dialog.Title>
					<RunChart
						graph={chart.graph}
						{pipeline}
						profiles={run.profiles}
						run={overlay}
						label={m['workflows.task.title']()}
						zoom={1}
						class="min-h-0 flex-1"
						testid="task-workflow-chart-expanded"
					/>
				</Dialog.Content>
			</Dialog.Root>
		{/if}

		{#if pipeline}
			{#if shared.roomFailed}
				<div class="flex items-center justify-between gap-2" role="alert">
					<p class="text-sm text-destructive">{m['tasks.load_error']()}</p>
					<Button variant="ghost" size="sm" onclick={() => void shared.loadRoom()}><RefreshCw />{m['tasks.refresh']()}</Button>
				</div>
			{:else if shared.roomLoaded}
				<PipelineRunDetails
					{pipeline}
					phase={run.node}
					done={task.state === 'completed' || run.status === 'done'}
					room={chat.taskRooms[taskId] ?? []}
					runs={run.runs ?? []}
				/>
			{:else}
				<p role="status" class="text-xs text-muted-foreground">{m['assistant.loading']()}</p>
			{/if}
		{/if}
	{:else}
		<p class="text-xs text-muted-foreground">{m['assistant.loading']()}</p>
	{/if}
</section>
