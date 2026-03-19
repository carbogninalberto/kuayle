<script lang="ts">
	import type { CycleProgress } from '$lib/types/cycle';

	let { progress }: { progress: CycleProgress } = $props();

	let percentage = $derived(
		progress.total > 0
			? Math.round(((progress.completed + progress.cancelled) / progress.total) * 100)
			: 0
	);

	let completedWidth = $derived(
		progress.total > 0
			? (progress.completed / progress.total) * 100
			: 0
	);

	let cancelledWidth = $derived(
		progress.total > 0
			? (progress.cancelled / progress.total) * 100
			: 0
	);
</script>

<div class="flex items-center gap-3">
	<div class="relative h-1.5 flex-1 overflow-hidden rounded-full bg-[var(--color-bg-tertiary)]">
		{#if completedWidth > 0}
			<div
				class="absolute left-0 top-0 h-full rounded-full bg-[var(--color-success)]"
				style="width: {completedWidth}%"
			></div>
		{/if}
		{#if cancelledWidth > 0}
			<div
				class="absolute top-0 h-full rounded-full bg-[var(--color-text-tertiary)]"
				style="left: {completedWidth}%; width: {cancelledWidth}%"
			></div>
		{/if}
	</div>
	<span class="shrink-0 text-xs tabular-nums text-[var(--color-text-tertiary)]">
		{percentage}%
	</span>
</div>
