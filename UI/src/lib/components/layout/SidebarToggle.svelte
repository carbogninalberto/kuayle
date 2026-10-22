<script lang="ts">
	import { Button } from '$lib/components/ui/button/index.js';
	import { PanelLeft } from '@lucide/svelte';
	import { sidebarState } from '$lib/features/layout/sidebar.state.svelte';
	import { m } from '$lib/paraglide/messages.js';
	import { getLocale, setLocale } from '$lib/paraglide/runtime.js';
</script>

{#if sidebarState.collapsed}
	<Button variant="ghost" size="sm"
		onclick={() => sidebarState.expand()}
		class="shrink-0 rounded-md p-1 text-[var(--color-text-tertiary)] hover:text-[var(--color-text-primary)]"
		title={m['sidebar.expand_sidebar']()}
	>
		<PanelLeft size={16} />
	</Button>
{/if}
