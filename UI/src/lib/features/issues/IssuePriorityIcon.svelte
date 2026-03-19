<script lang="ts">
	import type { IssuePriority } from '$lib/types/issue';
	import { SignalHigh, SignalMedium, SignalLow, Signal, Minus } from 'lucide-svelte';

	let { priority, size = 16 }: { priority: IssuePriority; size?: number } = $props();

	const icons: Record<IssuePriority, any> = {
		0: Minus,
		1: SignalHigh,
		2: SignalHigh,
		3: SignalMedium,
		4: SignalLow
	};

	const colors: Record<IssuePriority, string> = {
		0: 'text-[var(--color-text-tertiary)]',
		1: 'text-[var(--color-urgent)]',
		2: 'text-[var(--color-high)]',
		3: 'text-[var(--color-medium)]',
		4: 'text-[var(--color-low)]'
	};
</script>

<span class={colors[priority]}>
	<svelte:component this={icons[priority]} {size} />
</span>
