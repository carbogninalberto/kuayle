<script lang="ts">
	import TaskPanel from '../TaskPanel.svelte';
	import TaskWorkflowView from '$lib/features/workflows/TaskWorkflowView.svelte';
	import type { PanelContext } from './panel-host.svelte';
	import { m } from '$lib/paraglide/messages.js';

	// The issue chat's coding agents: each task as a conversation with its agent.
	// When the newest task follows a configurable workflow, its graph, current
	// step and path come first so each step and loop shows as it happens.
	let { context }: { context: PanelContext } = $props();
	const workflowTask = $derived(context.board?.tasks.find((task) => task.kind !== 'review'));
	const refreshKey = $derived(
		workflowTask
			? [workflowTask.state, workflowTask.generation, workflowTask.workflow_node ?? '', workflowTask.workflow_status ?? ''].join(':')
			: ''
	);
</script>

<div class="min-w-0 space-y-4 px-4 py-3">
	{#if context.board}
		{#if workflowTask?.workflow_version_id}{#key workflowTask.id}<TaskWorkflowView
					slug={context.slug}
					taskId={workflowTask.id}
					{refreshKey}
					compact
				/>{/key}{/if}
		<TaskPanel chat={context.chat} board={context.board} />
	{:else}<p class="text-xs text-muted-foreground">
			{m['assistant.agent_panel_general']()}
		</p>{/if}
</div>
