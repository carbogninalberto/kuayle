<script lang="ts">
	import type { Cycle } from '$lib/types/cycle';
	import { Badge } from '$lib/components/ui/badge';
	import { Play, Clock, CheckCircle2, Circle } from 'lucide-svelte';

	let {
		cycle,
		slug,
		teamId,
		clickable = true
	}: {
		cycle: Cycle;
		slug: string;
		teamId: string;
		clickable?: boolean;
	} = $props();

	const statusIcon = $derived(
		cycle.status === 'active' ? Play :
		cycle.status === 'completed' ? CheckCircle2 :
		cycle.status === 'upcoming' ? Clock :
		Circle
	);

	const badgeVariant = $derived<'default' | 'secondary' | 'outline'>(
		cycle.status === 'active' ? 'default' :
		cycle.status === 'completed' ? 'secondary' :
		'outline'
	);

	const badgeLabel = $derived(
		cycle.status === 'active' ? 'Current' :
		cycle.status === 'completed' ? 'Completed' :
		'Upcoming'
	);

	const successPct = $derived(
		cycle.progress && cycle.progress.total > 0
			? Math.round((cycle.progress.completed / cycle.progress.total) * 100)
			: 0
	);

	// Small SVG capacity ring for active cycles
	const ringPct = $derived(
		cycle.progress && cycle.progress.total > 0
			? Math.min(100, Math.round((cycle.progress.completed / cycle.progress.total) * 100))
			: 0
	);
	const ringCircumference = 2 * Math.PI * 8;
	const ringOffset = $derived(ringCircumference - (ringPct / 100) * ringCircumference);
</script>

{#snippet content()}
	<svelte:component this={statusIcon} size={14} class="shrink-0 text-[var(--color-text-tertiary)]" />

	<span class="min-w-0 flex-1 truncate text-sm font-medium text-[var(--color-text-primary)]">
		{cycle.name}
	</span>

	<div class="flex shrink-0 items-center gap-3 text-xs text-[var(--color-text-tertiary)]">
		<Badge variant={badgeVariant} class="text-[10px]">{badgeLabel}</Badge>

		{#if cycle.status === 'active' && cycle.progress}
			<div class="flex items-center gap-1.5">
				<svg width="20" height="20" viewBox="0 0 20 20" class="shrink-0 -rotate-90">
					<circle cx="10" cy="10" r="8" fill="none" stroke="var(--app-border)" stroke-width="2" />
					<circle
						cx="10" cy="10" r="8" fill="none"
						stroke="var(--color-error)"
						stroke-width="2"
						stroke-dasharray={ringCircumference}
						stroke-dashoffset={ringOffset}
						stroke-linecap="round"
					/>
				</svg>
				<span>{ringPct}% complete</span>
			</div>
			<span>{cycle.progress.total} scope</span>
		{:else if cycle.status === 'completed' && cycle.progress}
			<span>{successPct}% success</span>
			<span>{cycle.progress.completed} completed</span>
			<span>{cycle.progress.total} scope</span>
		{:else if cycle.progress}
			<span>{successPct}% of capacity</span>
			<span>{cycle.progress.total} scope</span>
		{:else}
			<span>0 scope</span>
		{/if}
	</div>
{/snippet}

{#if clickable}
	<a
		href="/{slug}/teams/{teamId}/cycles/{cycle.id}"
		class="group flex items-center gap-3 rounded-md px-3 py-2.5 hover:bg-[var(--color-bg-hover)]"
	>
		{@render content()}
	</a>
{:else}
	<div class="group flex items-center gap-3 rounded-md px-3 py-2.5 hover:bg-[var(--color-bg-hover)]">
		{@render content()}
	</div>
{/if}
