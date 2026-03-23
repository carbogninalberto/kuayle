<script lang="ts">
	import { PanelLeftOpen } from 'lucide-svelte';
	import { sidebarState } from '$lib/features/layout/sidebar.state.svelte';
</script>

{#if sidebarState.collapsed}
	<button
		onclick={() => sidebarState.expand()}
		class="shrink-0 rounded-md p-1 text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]"
		title="Expand sidebar"
	>
		<PanelLeftOpen size={16} />
	</button>
{/if}
