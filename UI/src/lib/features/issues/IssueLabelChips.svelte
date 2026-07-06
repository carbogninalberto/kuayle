<script lang="ts">
	import type { Label } from '$lib/types/label';
	import * as HoverCard from '$lib/components/ui/hover-card';

	let {
		labels = [],
		maxVisible = 2
	}: {
		labels?: Pick<Label, 'id' | 'name' | 'color'>[];
		maxVisible?: number;
	} = $props();

	let visibleLabels = $derived(labels.slice(0, maxVisible));
	let hiddenLabels = $derived(labels.slice(maxVisible));
</script>

{#if labels.length > 0}
	<div class="hidden shrink-0 gap-1 sm:flex">
		{#each visibleLabels as label (label.id)}
			<span class="inline-flex items-center gap-1 rounded-full border border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-1.5 py-0 text-[11px] leading-5 text-[var(--color-text-tertiary)] transition-colors hover:border-[var(--app-border-hover)] hover:bg-[var(--color-bg-tertiary)] hover:text-[var(--color-text-primary)]">
				<span class="h-1.5 w-1.5 shrink-0 rounded-full" style="background-color: {label.color}"></span>
				{label.name}
			</span>
		{/each}

		{#if hiddenLabels.length > 0}
			<span onclick={(e) => e.stopPropagation()} onkeydown={(e) => e.stopPropagation()} role="presentation">
				<HoverCard.Root openDelay={150} closeDelay={100}>
					<HoverCard.Trigger class="inline-flex cursor-default items-center gap-1 rounded-full border border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-1.5 py-0 text-[11px] leading-5 text-[var(--color-text-tertiary)] transition-colors hover:border-[var(--app-border-hover)] hover:bg-[var(--color-bg-tertiary)] hover:text-[var(--color-text-primary)]">
						<span class="flex -space-x-0.5">
							{#each hiddenLabels.slice(0, 3) as label (label.id)}
								<span class="h-1.5 w-1.5 shrink-0 rounded-full ring-1 ring-[var(--color-bg-secondary)]" style="background-color: {label.color}"></span>
							{/each}
						</span>
						+{hiddenLabels.length} label{hiddenLabels.length === 1 ? '' : 's'}
					</HoverCard.Trigger>
					<HoverCard.Content class="w-52 p-1" align="end">
						{#each hiddenLabels as label (label.id)}
							<div class="flex items-center gap-2 rounded-md px-2 py-1.5 text-xs text-[var(--color-text-primary)]">
								<span class="h-2 w-2 shrink-0 rounded-full" style="background-color: {label.color}"></span>
								<span class="truncate">{label.name}</span>
							</div>
						{/each}
					</HoverCard.Content>
				</HoverCard.Root>
			</span>
		{/if}
	</div>
{/if}
