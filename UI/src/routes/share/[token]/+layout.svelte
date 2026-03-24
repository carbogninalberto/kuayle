<script lang="ts">
	import { onMount } from 'svelte';
	import { preferencesState } from '$lib/features/preferences/preferences.state.svelte';

	let { children } = $props();

	// Ensure theme class is set immediately for portaled elements (tooltips, popovers)
	onMount(() => {
		if (!document.documentElement.className.includes('dark') && !document.documentElement.className.includes('light')) {
			document.documentElement.className = preferencesState.activeTheme || 'dark';
		}
	});
</script>

<div class="flex min-h-screen flex-col bg-[var(--color-bg)]">
	<div class="flex-1">
		{@render children()}
	</div>
	<footer class="px-4 py-3 text-center text-xs text-[var(--color-text-tertiary)]">
		Shared via <a href="https://kuayle.com" target="_blank" rel="noopener noreferrer" class="hover:text-[var(--color-text-secondary)] underline decoration-[var(--color-text-tertiary)]/30 hover:decoration-[var(--color-text-secondary)]/50 transition-colors">Kuayle</a>
	</footer>
</div>
