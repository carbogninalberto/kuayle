<script lang="ts">
	import SelectField from '$lib/components/shared/SelectField.svelte';
	import { Label } from '$lib/components/ui/label/index.js';
	import TaskWorkflowView from '$lib/features/workflows/TaskWorkflowView.svelte';
	import TaskPath from '../TaskPath.svelte';
	import { m } from '$lib/features/i18n/messages';
	import type { PanelContext } from './panel-host.svelte';

	// The issue chat's workflow: the graph a coding task follows, its current
	// step and the path taken. The graph opens in Settings → Workflows, where
	// owners and admins edit it and everyone else reads it.
	let { context }: { context: PanelContext } = $props();

	const tasks = $derived((context.board?.tasks ?? []).filter((task) => task.kind !== 'review'));
	let chosen = $state('');
	// The newest task until the person picks another.
	const taskId = $derived(tasks.some((task) => task.id === chosen) ? chosen : (tasks[0]?.id ?? ''));
	const task = $derived(tasks.find((t) => t.id === taskId));
	const taskName = (prompt: string) => (prompt.length > 60 ? `${prompt.slice(0, 60)}…` : prompt);
</script>

<div class="min-w-0 space-y-3 px-4 py-3" data-testid="workflow-panel">
	{#if !task}<p class="text-xs text-muted-foreground">{m['workflows.task.no_task']()}</p>
	{:else}
		{#if tasks.length > 1}<div class="space-y-1.5">
				<Label for="workflow-panel-task" class="text-xs">{m['workflows.task.pick_task']()}</Label>
				<SelectField
					id="workflow-panel-task"
					bind:value={() => taskId, (value: string) => (chosen = value)}
					options={tasks.map((t) => ({ value: t.id, label: taskName(t.prompt) }))}
				/>
			</div>{/if}
		{#key task.id}
			<TaskWorkflowView chat={context.chat} {task} compact />
			<TaskPath chat={context.chat} {task} />
		{/key}
	{/if}
</div>
