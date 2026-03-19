<script lang="ts">
	import type { IssueStatus, IssuePriority } from '$lib/types/issue';
	import { STATUS_LABELS, PRIORITY_LABELS } from '$lib/types/issue';
	import * as Popover from '$lib/components/ui/popover';
	import {
		Circle,
		CircleDot,
		CircleDashed,
		Loader,
		CheckCircle2,
		XCircle,
		SignalHigh,
		SignalMedium,
		SignalLow,
		Minus
	} from 'lucide-svelte';

	let {
		filters = $bindable({}),
		onchange
	}: {
		filters: Record<string, string>;
		onchange: (filters: Record<string, string>) => void;
	} = $props();

	let statusOpen = $state(false);
	let priorityOpen = $state(false);

	const statusIcons: Record<IssueStatus, typeof Circle> = {
		backlog: CircleDashed,
		todo: Circle,
		in_progress: Loader,
		in_review: CircleDot,
		done: CheckCircle2,
		cancelled: XCircle
	};

	const priorityIcons: Record<IssuePriority, typeof Minus> = {
		0: Minus,
		1: SignalHigh,
		2: SignalHigh,
		3: SignalMedium,
		4: SignalLow
	};

	function setFilter(key: string, value: string) {
		if (value) {
			filters[key] = value;
		} else {
			delete filters[key];
		}
		filters = { ...filters };
		onchange(filters);
	}

	let statusLabel = $derived(filters.status ? STATUS_LABELS[filters.status as IssueStatus] : 'All statuses');
	let priorityLabel = $derived(filters.priority ? PRIORITY_LABELS[Number(filters.priority) as IssuePriority] : 'All priorities');
</script>

<div class="flex items-center gap-2 border-b border-[var(--app-border)] px-4 py-2">
	<Popover.Root bind:open={statusOpen}>
		<Popover.Trigger>
			<button class="flex items-center gap-1.5 rounded-md border border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-2 py-1 text-xs text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)]">
				{#if filters.status}
					<svelte:component this={statusIcons[filters.status as IssueStatus]} size={12} />
				{/if}
				{statusLabel}
			</button>
		</Popover.Trigger>
		<Popover.Content class="w-40 p-1" align="start">
			<button
				onclick={() => { setFilter('status', ''); statusOpen = false; }}
				class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)] {!filters.status ? 'bg-[var(--color-bg-hover)]' : ''}"
			>
				All statuses
			</button>
			{#each Object.entries(STATUS_LABELS) as [value, label]}
				<button
					onclick={() => { setFilter('status', value); statusOpen = false; }}
					class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)] {filters.status === value ? 'bg-[var(--color-bg-hover)]' : ''}"
				>
					<svelte:component this={statusIcons[value as IssueStatus]} size={14} />
					{label}
				</button>
			{/each}
		</Popover.Content>
	</Popover.Root>

	<Popover.Root bind:open={priorityOpen}>
		<Popover.Trigger>
			<button class="flex items-center gap-1.5 rounded-md border border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-2 py-1 text-xs text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)]">
				{#if filters.priority}
					<svelte:component this={priorityIcons[Number(filters.priority) as IssuePriority]} size={12} />
				{/if}
				{priorityLabel}
			</button>
		</Popover.Trigger>
		<Popover.Content class="w-40 p-1" align="start">
			<button
				onclick={() => { setFilter('priority', ''); priorityOpen = false; }}
				class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)] {!filters.priority ? 'bg-[var(--color-bg-hover)]' : ''}"
			>
				All priorities
			</button>
			{#each Object.entries(PRIORITY_LABELS) as [value, label]}
				<button
					onclick={() => { setFilter('priority', value); priorityOpen = false; }}
					class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)] {filters.priority === value ? 'bg-[var(--color-bg-hover)]' : ''}"
				>
					<svelte:component this={priorityIcons[Number(value) as IssuePriority]} size={14} />
					{label}
				</button>
			{/each}
		</Popover.Content>
	</Popover.Root>

	{#if Object.keys(filters).length > 0}
		<button
			onclick={() => {
				filters = {};
				onchange({});
			}}
			class="text-xs text-[var(--color-text-tertiary)] hover:text-[var(--color-text-secondary)]"
		>
			Clear filters
		</button>
	{/if}
</div>
