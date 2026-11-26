<script lang="ts">
	import TaskPanel from '../TaskPanel.svelte';
	import TaskWorkflowView from '$lib/features/workflows/TaskWorkflowView.svelte';
	import type { PanelContext } from './panel-host.svelte';
	import { m } from '$lib/features/i18n/messages';

	let { context }: { context: PanelContext } = $props();
	const workflowTask = $derived(context.board?.tasks.find((task) => task.kind !== 'review'));
</script>

<div class="min-w-0 space-y-4 px-4 py-3">
	{#if context.board}
		{#if workflowTask?.workflow_version_id}{#key workflowTask.id}<TaskWorkflowView onopenpost={context.close}
					chat={context.chat}
					task={workflowTask}
					onopenworkflow={() => context.open('workflow')}
					compact
				/>{/key}{/if}
		<TaskPanel chat={context.chat} board={context.board} />
	{:else}<p class="text-xs text-muted-foreground">
			{m['assistant.agent_panel_general']()}
		</p>{/if}
</div>
