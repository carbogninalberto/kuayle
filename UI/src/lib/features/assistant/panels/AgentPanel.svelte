<script lang="ts">
	import TaskPanel from '../TaskPanel.svelte';
	import TaskWorkflowView from '$lib/features/workflows/TaskWorkflowView.svelte';
	import type { PanelContext } from './panel-host.svelte';
	import { m } from '$lib/features/i18n/messages';

	// The issue chat's coding tasks. When the newest one follows a
	// configurable workflow, its graph and current step come first; each task
	// then shows the path it took.
	let { context }: { context: PanelContext } = $props();
	const workflowTask = $derived(context.board?.tasks.find((task) => task.kind !== 'review'));
</script>

<div class="min-w-0 space-y-4 px-4 py-3">
	{#if context.board}
		{#if workflowTask?.workflow_version_id}{#key workflowTask.id}<TaskWorkflowView
					chat={context.chat}
					task={workflowTask}
					compact
				/>{/key}{/if}
		<TaskPanel chat={context.chat} board={context.board} />
	{:else}<p class="text-xs text-muted-foreground">
			{m['assistant.agent_panel_general']()}
		</p>{/if}
</div>
