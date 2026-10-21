<script lang="ts">
	import type { Cycle } from '$lib/types/cycle';
	import { Badge } from '$lib/components/ui/badge';
	import * as Popover from '$lib/components/ui/popover';
	import { m } from '$lib/paraglide/messages.js';
	import { getLocale, setLocale } from '$lib/paraglide/runtime.js';
	import {
		CirclePlay,
		CircleDotDashed,
		CircleCheckBig,
		Circle,
		MoreHorizontal,
		Pencil,
		Calendar,
		Play,
		CheckCircle2,
		Trash2
	} from 'lucide-svelte';

	let {
		cycle,
		slug,
		teamId,
		onactivate,
		oncomplete,
		ondelete,
		onedit
	}: {
		cycle: Cycle;
		slug: string;
		teamId: string;
		onactivate?: (cycleId: string) => void;
		oncomplete?: (cycleId: string) => void;
		ondelete?: (cycleId: string) => void;
		onedit?: (cycle: Cycle) => void;
	} = $props();

	let menuOpen = $state(false);

	const statusIcon = $derived(
		cycle.status === 'active'
			? CirclePlay
			: cycle.status === 'completed'
				? CircleCheckBig
				: cycle.status === 'upcoming'
					? CircleDotDashed
					: Circle
	);

	const badgeVariant = $derived<'default' | 'secondary' | 'outline'>(
		cycle.status === 'active' ? 'default' : cycle.status === 'completed' ? 'secondary' : 'outline'
	);

	const badgeLabel = $derived(
		cycle.status === 'active'
			? m['cycles.status.current']()
			: cycle.status === 'completed'
				? m['cycles.status.completed']()
				: m['cycles.status.upcoming']()
	);

	const successPct = $derived(
		cycle.progress && cycle.progress.total > 0 ? Math.round((cycle.progress.completed / cycle.progress.total) * 100) : 0
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
	<StatusIcon size={14} class="mt-0.5 shrink-0 text-[var(--color-text-tertiary)] sm:mt-0"></StatusIcon>

	<a
		href="/{slug}/teams/{teamId}/cycles/{cycle.id}"
		class="min-w-0 flex-1 truncate text-sm font-medium text-[var(--color-text-primary)] after:absolute after:inset-0 after:rounded-md focus-visible:outline-2 focus-visible:outline-[var(--app-accent)]"
	>
		{cycle.name}
	</a>

	<div
		class="col-start-2 col-end-4 flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1 text-[13px] text-[var(--color-text-tertiary)] sm:col-auto sm:shrink-0 sm:flex-nowrap"
	>
		<Badge variant={badgeVariant} class="text-[10px]">{badgeLabel}</Badge>

		{#if cycle.status === 'active' && cycle.progress}
			<div class="flex items-center gap-1.5">
				<svg width="20" height="20" viewBox="0 0 20 20" class="shrink-0 -rotate-90">
					<circle cx="10" cy="10" r="8" fill="none" stroke="var(--app-border)" stroke-width="2" />
					<circle
						cx="10"
						cy="10"
						r="8"
						fill="none"
						stroke="var(--color-error)"
						stroke-width="2"
						stroke-dasharray={ringCircumference}
						stroke-dashoffset={ringOffset}
						stroke-linecap="round"
					/>
				</svg>
				<span
					><span class="font-semibold text-[var(--color-text-secondary)]">{ringPct}%</span>{m[
						'cycles.timeline.complete'
					]()}</span
				>
			</div>
			<span
				><span class="font-semibold text-[var(--color-text-secondary)]">{cycle.progress.total}</span>{m[
					'cycles.timeline.scope'
				]()}</span
			>
		{:else if cycle.status === 'completed' && cycle.progress}
			<span
				><span class="font-semibold text-[var(--color-text-secondary)]">{successPct}%</span>{m[
					'cycles.timeline.success'
				]()}</span
			>
			<span
				><span class="font-semibold text-[var(--color-text-secondary)]">{cycle.progress.completed}</span>{m[
					'cycles.timeline.completed'
				]()}</span
			>
			<span
				><span class="font-semibold text-[var(--color-text-secondary)]">{cycle.progress.total}</span>{m[
					'cycles.timeline.scope'
				]()}</span
			>
		{:else if cycle.progress}
			<span
				><span class="font-semibold text-[var(--color-text-secondary)]">{successPct}%</span>{m[
					'cycles.timeline.of_capacity'
				]()}</span
			>
			<span
				><span class="font-semibold text-[var(--color-text-secondary)]">{cycle.progress.total}</span>{m[
					'cycles.timeline.scope'
				]()}</span
			>
		{:else}
			<span><span class="font-semibold text-[var(--color-text-secondary)]">0</span>{m['cycles.timeline.scope']()}</span>
		{/if}
	</div>

	{#if onedit || ondelete || (cycle.status === 'upcoming' && onactivate) || (cycle.status === 'active' && oncomplete)}
		<div class="relative z-10 col-start-3 row-start-1 self-start sm:ml-auto sm:self-center">
			<Popover.Root bind:open={menuOpen}>
				<Popover.Trigger
					aria-label={m['cycles.actions']({ name: cycle.name })}
					class="rounded p-1 text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)] focus-visible:outline-2 focus-visible:outline-[var(--app-accent)]"
				>
					<MoreHorizontal size={14} />
				</Popover.Trigger>
				<Popover.Content class="w-48 p-1" align="end" side="bottom">
					{#if onedit}
						<button
							onclick={(e) => {
								e.stopPropagation();
								menuOpen = false;
								onedit?.(cycle);
							}}
							class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]"
						>
							<Pencil size={14} />
							{m['cycles.edit_cycle']()}
						</button>
					{/if}
					{#if cycle.status === 'upcoming' && onactivate}
						<button
							onclick={(e) => {
								e.stopPropagation();
								menuOpen = false;
								onactivate?.(cycle.id);
							}}
							class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]"
						>
							<Play size={14} />
							{m['cycles.start_cycle']()}
						</button>
					{/if}
					{#if cycle.status === 'active' && oncomplete}
						<button
							onclick={(e) => {
								e.stopPropagation();
								menuOpen = false;
								oncomplete?.(cycle.id);
							}}
							class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]"
						>
							<CheckCircle2 size={14} />
							{m['cycles.complete_cycle']()}
						</button>
					{/if}
					{#if ondelete}
						<div class="my-1 border-t border-[var(--app-border)]"></div>
						<button
							onclick={(e) => {
								e.stopPropagation();
								menuOpen = false;
								ondelete?.(cycle.id);
							}}
							class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-error)] hover:bg-[var(--color-bg-hover)]"
						>
							<Trash2 size={14} />
							{m['cycles.delete_cycle']()}
						</button>
					{/if}
				</Popover.Content>
			</Popover.Root>
		</div>
	{/if}
{/snippet}

<div
	class="group relative grid min-h-16 min-w-0 grid-cols-[auto_minmax(0,1fr)_auto] items-start gap-x-3 gap-y-2 rounded-md px-3 py-3 hover:bg-[var(--color-bg-hover)] sm:flex sm:min-h-0 sm:items-center sm:gap-3 sm:py-4"
>
	{@render content()}
</div>
