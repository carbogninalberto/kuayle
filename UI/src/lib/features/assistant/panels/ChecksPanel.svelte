<script lang="ts">
	import TaskChecks from '../TaskChecks.svelte';
	import type { PanelContext } from './panel-host.svelte';

	// The checks of the open task's pipeline (KUA-286): how each stands for
	// the head, and the owner's approval and waivers.
	let { context }: { context: PanelContext } = $props();
	const task = $derived(context.agentTask);
</script>

{#if task}{#key task.id}<TaskChecks chat={context.chat} {task} />{/key}{/if}
