<script lang="ts">
	import { agentTasks, type AgentTask, type WorkflowSwitch } from '$lib/api/agent-tasks';
	import { isPipeline, workflows, type WorkflowSummary } from '$lib/api/workflows';
	import { Button } from '$lib/components/ui/button/index.js';
	import * as Dialog from '$lib/components/ui/dialog/index.js';
	import SelectField from '$lib/components/shared/SelectField.svelte';
	import { m } from '$lib/features/i18n/messages';
	import { pathDuration } from './task-path';

	let {
		open = $bindable(false),
		slug,
		taskId,
		oncontinued,
		returnFocus
	}: {
		open: boolean;
		slug: string;
		taskId: string;
		oncontinued: (task: AgentTask) => void;
		returnFocus: () => void;
	} = $props();
	const fieldId = $props.id();

	let choices = $state<WorkflowSummary[]>([]);
	let workflowId = $state<string>();
	let node = $state<string>();
	let plan = $state<WorkflowSwitch | null>(null);
	let loading = $state(false);
	let planning = $state(false);
	let busy = $state(false);
	let error = $state('');
	const destination = $derived(choices.find((workflow) => workflow.id === workflowId));
	const steps = $derived(
		destination && !isPipeline(destination.graph)
			? destination.graph.nodes
					.filter((step) => step.type !== 'start' && step.type !== 'end')
					.map((step) => ({ value: step.id, label: step.label || step.id }))
			: []
	);
	const continuation = $derived(plan?.continuation);

	function refusal(reason?: string): string {
		switch (reason) {
			case 'not_at_rest':
				return m['tasks.workflow_update_pause_first']();
			case 'workflow_archived':
			case 'workflow_unusable':
			case 'workflow_unavailable':
				return m['tasks.workflow_continue_unavailable']();
			case 'step_missing':
			case 'step_not_runnable':
				return m['tasks.workflow_continue_step_missing']();
			case 'target_version_changed':
				return m['tasks.workflow_continue_stale']();
			case 'workflow_kind':
				return m['tasks.workflow_continue_kind']();
			case 'pipeline_pinned':
				return m['tasks.workflow_continue_pipeline']();
			default:
				return m['tasks.workflow_update_error']();
		}
	}

	function errorText(error: unknown): string {
		return refusal((error as { error?: { message?: string } })?.error?.message);
	}

	$effect(() => {
		if (!open) {
			choices = [];
			workflowId = node = undefined;
			error = '';
			return;
		}
		let stale = false;
		loading = true;
		Promise.all([workflows.list(slug), workflows.task(slug, taskId)])
			.then(([list, current]) => {
				if (stale) return;
				// A task never changes engine, so pipelines are not offered.
				choices = list.workflows.filter(
					(workflow) =>
						!workflow.archived &&
						workflow.latest_version > 0 &&
						workflow.id !== current.workflow_id &&
						!isPipeline(workflow.graph)
				);
			})
			.catch((e) => {
				if (!stale) error = errorText(e);
			})
			.finally(() => {
				if (!stale) loading = false;
			});
		return () => {
			stale = true;
		};
	});

	$effect(() => {
		plan = null;
		planning = false;
		if (!open || !workflowId || !node) return;
		const controller = new AbortController();
		planning = true;
		error = '';
		agentTasks
			.workflowSwitch(slug, taskId, { workflow_id: workflowId, node }, controller.signal)
			.then((result) => {
				if (!controller.signal.aborted) plan = result;
			})
			.catch((e) => {
				if (!controller.signal.aborted) error = errorText(e);
			})
			.finally(() => {
				if (!controller.signal.aborted) planning = false;
			});
		return () => controller.abort();
	});

	async function submit() {
		if (!workflowId || !node || !continuation || busy) return;
		busy = true;
		error = '';
		try {
			const moved = await agentTasks.switchWorkflow(slug, taskId, {
				workflow_id: workflowId,
				node,
				expected_version_id: continuation.to_version_id
			});
			oncontinued(moved.task);
			open = false;
		} catch (e) {
			plan = null;
			error = errorText(e);
		} finally {
			busy = false;
		}
	}
</script>

<Dialog.Root
	bind:open={
		() => open,
		(next) => {
			if (!busy) open = next;
		}
	}
>
	<Dialog.Content
		class="max-h-[80dvh] overflow-y-auto"
		onCloseAutoFocus={(event) => {
			event.preventDefault();
			returnFocus();
		}}
	>
		<Dialog.Header>
			<Dialog.Title>{m['tasks.workflow_continue_title']()}</Dialog.Title>
			<Dialog.Description>{m['tasks.workflow_continue_choose']()}</Dialog.Description>
		</Dialog.Header>
		{#if loading}
			<p role="status" class="text-muted-foreground">{m['common.loading']()}</p>
		{:else if choices.length}
			<div class="grid gap-3 sm:grid-cols-2">
				<div class="grid gap-1.5">
					<label for={`${fieldId}-workflow`}>{m['tasks.workflow_continue_workflow']()}</label>
					<SelectField
						id={`${fieldId}-workflow`}
						bind:value={
							() => workflowId,
							(next) => {
								workflowId = next;
								node = undefined;
								error = '';
							}
						}
						options={choices.map((workflow) => ({ value: workflow.id, label: workflow.name }))}
						placeholder={m['tasks.workflow_continue_workflow']()}
						disabled={busy}
					/>
				</div>
				<div class="grid gap-1.5">
					<label for={`${fieldId}-step`}>{m['tasks.workflow_continue_step']()}</label>
					<SelectField
						id={`${fieldId}-step`}
						bind:value={node}
						options={steps}
						placeholder={m['tasks.workflow_continue_step']()}
						disabled={!steps.length || busy}
					/>
				</div>
			</div>
		{:else if !error}
			<p class="text-muted-foreground">{m['tasks.workflow_continue_empty']()}</p>
		{/if}
		{#if error || plan?.refused}
			<p role="alert" class="text-destructive">{error || refusal(plan?.refused)}</p>
		{:else if planning}
			<p role="status" class="text-muted-foreground">{m['common.loading']()}</p>
		{:else if continuation && plan}
			<section aria-live="polite" class="grid gap-2 rounded-lg bg-muted/50 p-3">
				<p class="font-medium">
					{m['tasks.workflow_continue_plan']({
						workflow: continuation.to_workflow_name,
						version: plan.to_version,
						step: plan.node_label || plan.node || ''
					})}
				</p>
				<p class="text-muted-foreground">{m['tasks.workflow_continue_carries']()}</p>
				<p class="text-muted-foreground">
					{m['tasks.workflow_continue_resets']({ time: pathDuration(continuation.time_spent_seconds * 1000) })}
				</p>
				{#if continuation.budget.max_iterations || continuation.budget.max_minutes || continuation.max_visits || continuation.timeout_minutes}
					<dl class="grid grid-cols-[1fr_auto] gap-x-4 gap-y-1 border-t pt-2 text-muted-foreground">
						{#if continuation.budget.max_iterations}
							<dt>{m['workflows.editor.budget_iterations']()}</dt>
							<dd>{continuation.budget.max_iterations}</dd>
						{/if}
						{#if continuation.budget.max_minutes}
							<dt>{m['workflows.editor.budget_minutes']()}</dt>
							<dd>{continuation.budget.max_minutes}</dd>
						{/if}
						{#if continuation.max_visits}
							<dt>{m['workflows.editor.node_max_visits']()}</dt>
							<dd>{continuation.max_visits}</dd>
						{/if}
						{#if continuation.timeout_minutes}
							<dt>{m['workflows.editor.node_timeout']()}</dt>
							<dd>{continuation.timeout_minutes}</dd>
						{/if}
					</dl>
				{/if}
			</section>
		{/if}
		<Dialog.Footer>
			<Dialog.Close disabled={busy}>{m['common.cancel']()}</Dialog.Close>
			<Button disabled={!continuation || busy || planning} onclick={() => void submit()}
				>{m['tasks.workflow_continue_confirm']()}</Button
			>
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>
