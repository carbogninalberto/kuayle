<script lang="ts">
	import { page } from '$app/state';
	import SelectField from '$lib/components/shared/SelectField.svelte';
	import { Label } from '$lib/components/ui/label/index.js';
	import TaskFiles from '../TaskFiles.svelte';
	import { m } from '$lib/features/i18n/messages';
	import type { PanelContext } from './panel-host.svelte';

	// The files of the issue chat's coding task at its head (KUA-285): the task
	// open in the agent view, otherwise the newest, until the person picks another.
	let { context }: { context: PanelContext } = $props();

	const tasks = $derived((context.board?.tasks ?? []).filter((task) => task.kind !== 'review'));
	const viewing = $derived(page.url.searchParams.get('agent'));
	let chosen = $state('');
	const taskId = $derived(
		tasks.some((task) => task.id === chosen)
			? chosen
			: (tasks.find((task) => task.id === viewing)?.id ?? tasks[0]?.id ?? '')
	);
	const task = $derived(tasks.find((t) => t.id === taskId));
	const taskName = (prompt: string) => (prompt.length > 60 ? `${prompt.slice(0, 60)}…` : prompt);
</script>

<div class="flex h-full min-h-0 min-w-0 flex-col" data-testid="files-panel">
	{#if !task && (!context.board || context.board.loading)}<p class="px-4 py-3 text-xs text-muted-foreground" role="status">
			{m['assistant.loading']()}
		</p>
	{:else if !task}<p class="px-4 py-3 text-xs text-muted-foreground">{m['workflows.task.no_task']()}</p>
	{:else}
		{#if tasks.length > 1}<div class="shrink-0 space-y-1.5 border-b px-3 py-2">
				<Label for="files-panel-task" class="text-xs">{m['workflows.task.pick_task']()}</Label>
				<SelectField
					id="files-panel-task"
					bind:value={() => taskId, (value: string) => (chosen = value)}
					options={tasks.map((t) => ({ value: t.id, label: taskName(t.prompt) }))}
				/>
			</div>{/if}
		{#key task.id}<TaskFiles slug={context.slug} {task} />{/key}
	{/if}
</div>
