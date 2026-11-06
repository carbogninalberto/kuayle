<script lang="ts">
	import TaskPanel from '../TaskPanel.svelte';
	import type { PanelContext } from './panel-host.svelte';
	import { m } from '$lib/paraglide/messages.js';

	// The issue chat's coding agents: each task as a conversation with its agent.
	let { context }: { context: PanelContext } = $props();
</script>

<div class="min-w-0 px-4 py-3">
	{#if context.board}<TaskPanel chat={context.chat} board={context.board} />{:else}<p
			class="text-xs text-muted-foreground"
		>
			{m['assistant.agent_panel_general']()}
		</p>{/if}
</div>
