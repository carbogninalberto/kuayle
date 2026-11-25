<script lang="ts">
	import TaskNotebook from '../TaskNotebook.svelte';
	import type { PanelContext } from './panel-host.svelte';

	// The open task's notebook (KUA-286): moved out of the thread into a tab
	// beside it. Offered to the task's creator alone; the server decides.
	let { context }: { context: PanelContext } = $props();
	const task = $derived(context.agentTask);
</script>

{#if task}{#key task.id}<TaskNotebook chat={context.chat} {task} />{/key}{/if}
