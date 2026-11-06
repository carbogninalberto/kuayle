<script lang="ts">
	import DiffPanel from '../DiffPanel.svelte';
	import type { PanelContext } from './panel-host.svelte';

	// An attempt's diff, opened from a task or the overview's Changes section.
	let { context }: { context: PanelContext } = $props();
	const view = $derived(context.chat.diffView);
</script>

{#if view}{#key `${view.taskId}:${view.runId}`}<DiffPanel
			slug={context.chat.slug}
			taskId={view.taskId}
			runId={view.runId}
			title={view.title}
			onclose={() => {
				context.chat.diffView = null;
				context.close();
			}}
		/>{/key}{/if}
