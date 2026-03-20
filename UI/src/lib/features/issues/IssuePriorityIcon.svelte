<script lang="ts">
	import type { IssuePriority } from '$lib/types/issue';
	import { Minus } from 'lucide-svelte';

	let { priority, size = 16 }: { priority: IssuePriority; size?: number } = $props();
</script>

{#if priority === 0}
	<span class="text-[var(--color-text-tertiary)]">
		<Minus {size} />
	</span>
{:else if priority === 4}
	<!-- Low: 3 bars, only 1st filled -->
	<svg width={size} height={size} viewBox="0 0 16 16" fill="none" class="shrink-0">
		<rect x="2" y="11" width="3" height="4" rx="0.5" fill="currentColor" />
		<rect x="6.5" y="7" width="3" height="8" rx="0.5" fill="currentColor" opacity="0.2" />
		<rect x="11" y="3" width="3" height="12" rx="0.5" fill="currentColor" opacity="0.2" />
	</svg>
{:else if priority === 3}
	<!-- Medium: 3 bars, 1st and 2nd filled -->
	<svg width={size} height={size} viewBox="0 0 16 16" fill="none" class="shrink-0">
		<rect x="2" y="11" width="3" height="4" rx="0.5" fill="currentColor" />
		<rect x="6.5" y="7" width="3" height="8" rx="0.5" fill="currentColor" />
		<rect x="11" y="3" width="3" height="12" rx="0.5" fill="currentColor" opacity="0.2" />
	</svg>
{:else if priority === 2}
	<!-- High: all 3 bars filled -->
	<svg width={size} height={size} viewBox="0 0 16 16" fill="none" class="shrink-0">
		<rect x="2" y="11" width="3" height="4" rx="0.5" fill="currentColor" />
		<rect x="6.5" y="7" width="3" height="8" rx="0.5" fill="currentColor" />
		<rect x="11" y="3" width="3" height="12" rx="0.5" fill="currentColor" />
	</svg>
{:else if priority === 1}
	<!-- Urgent: circle with exclamation -->
	<svg width={size} height={size} viewBox="0 0 16 16" fill="none" class="shrink-0">
		<circle cx="8" cy="8" r="6.5" stroke="currentColor" stroke-width="1.5" />
		<path d="M8 4.5V9" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" />
		<circle cx="8" cy="11.5" r="0.75" fill="currentColor" />
	</svg>
{/if}
