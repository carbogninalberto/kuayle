<script lang="ts">
	import type { Cycle } from '$lib/types/cycle';
	import { Badge } from '$lib/components/ui/badge';
	import * as Popover from '$lib/components/ui/popover';
	import { CirclePlay, CircleDotDashed, CircleCheckBig, Circle, MoreHorizontal, Pencil, Calendar, Play, CheckCircle2, Trash2 } from 'lucide-svelte';

	let {
		cycle,
		slug,
		teamId,
		clickable = true,
		onactivate,
		oncomplete,
		ondelete,
		onedit
	}: {
		cycle: Cycle;
		slug: string;
		teamId: string;
		clickable?: boolean;
		onactivate?: (cycleId: string) => void;
		oncomplete?: (cycleId: string) => void;
		ondelete?: (cycleId: string) => void;
		onedit?: (cycle: Cycle) => void;
	} = $props();

	let menuOpen = $state(false);

	const statusIcon = $derived(
		cycle.status === 'active' ? CirclePlay :
		cycle.status === 'completed' ? CircleCheckBig :
		cycle.status === 'upcoming' ? CircleDotDashed :
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

	const ringPct = $derived(
		cycle.progress && cycle.progress.total > 0
			? Math.min(100, Math.round((cycle.progress.completed / cycle.progress.total) * 100))
			: 0
	);
	const ringCircumference = 2 * Math.PI * 8;
	const ringOffset = $derived(ringCircumference - (ringPct / 100) * ringCircumference);
</script>

{#snippet content()}
	{@const StatusIcon = statusIcon}
	<StatusIcon size={14} class="shrink-0 text-[var(--color-text-tertiary)]"></StatusIcon>

	<span class="min-w-0 flex-1 truncate text-sm font-medium text-[var(--color-text-primary)]">
		{cycle.name}
	</span>

	<div class="flex shrink-0 flex-wrap items-center gap-x-3 gap-y-1 text-[13px] text-[var(--color-text-tertiary)]">
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
				<span><span class="font-semibold text-[var(--color-text-secondary)]">{ringPct}%</span> complete</span>
			</div>
			<span><span class="font-semibold text-[var(--color-text-secondary)]">{cycle.progress.total}</span> scope</span>
		{:else if cycle.status === 'completed' && cycle.progress}
			<span><span class="font-semibold text-[var(--color-text-secondary)]">{successPct}%</span> success</span>
			<span><span class="font-semibold text-[var(--color-text-secondary)]">{cycle.progress.completed}</span> completed</span>
			<span><span class="font-semibold text-[var(--color-text-secondary)]">{cycle.progress.total}</span> scope</span>
		{:else if cycle.progress}
			<span><span class="font-semibold text-[var(--color-text-secondary)]">{successPct}%</span> of capacity</span>
			<span><span class="font-semibold text-[var(--color-text-secondary)]">{cycle.progress.total}</span> scope</span>
		{:else}
			<span><span class="font-semibold text-[var(--color-text-secondary)]">0</span> scope</span>
		{/if}
	</div>

	<!-- 3-dot menu: visible on hover or when open -->
	<Popover.Root bind:open={menuOpen}>
		<Popover.Trigger>
			<!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
			<button
				onclick={(e) => { e.stopPropagation(); e.preventDefault(); }}
				class="rounded p-0.5 text-[var(--color-text-tertiary)] opacity-100 transition-opacity hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-secondary)] sm:ml-auto sm:opacity-0 sm:group-hover:opacity-100 {menuOpen ? '!opacity-100' : ''}"
			>
				<MoreHorizontal size={14} />
			</button>
		</Popover.Trigger>
		<Popover.Content class="w-48 p-1" align="end" side="bottom">
			{#if onedit}
				<button
					onclick={(e) => { e.stopPropagation(); menuOpen = false; onedit?.(cycle); }}
					class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]"
				>
					<Pencil size={14} />
					Edit cycle
				</button>
			{/if}
			{#if cycle.status === 'upcoming' && onactivate}
				<button
					onclick={(e) => { e.stopPropagation(); menuOpen = false; onactivate?.(cycle.id); }}
					class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]"
				>
					<Play size={14} />
					Start cycle
				</button>
			{/if}
			{#if cycle.status === 'active' && oncomplete}
				<button
					onclick={(e) => { e.stopPropagation(); menuOpen = false; oncomplete?.(cycle.id); }}
					class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]"
				>
					<CheckCircle2 size={14} />
					Complete cycle
				</button>
			{/if}
			{#if ondelete}
				<div class="my-1 border-t border-[var(--app-border)]"></div>
				<button
					onclick={(e) => { e.stopPropagation(); menuOpen = false; ondelete?.(cycle.id); }}
					class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-error)] hover:bg-[var(--color-bg-hover)]"
				>
					<Trash2 size={14} />
					Delete cycle
				</button>
			{/if}
		</Popover.Content>
	</Popover.Root>
{/snippet}

{#if clickable}
	<a
		href="/{slug}/teams/{teamId}/cycles/{cycle.id}"
		class="group flex min-w-0 flex-wrap items-center gap-x-3 gap-y-2 rounded-md px-3 py-5 hover:bg-[var(--color-bg-hover)] sm:py-4"
	>
		{@render content()}
	</a>
{:else}
	<div class="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-2 px-3 py-5 sm:py-4">
		{@render content()}
	</div>
{/if}
