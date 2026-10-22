<script lang="ts">
	import { Button } from '$lib/components/ui/button/index.js';
	import type { IssuePriority } from '$lib/types/issue';
	import { getPriorityLabel, getPriorityLabels } from '$lib/types/issue';
	import { teamStatusesState } from '$lib/features/issues/team-statuses.state.svelte';
	import IssueStatusIcon from '$lib/features/issues/IssueStatusIcon.svelte';
	import IssuePriorityIcon from '$lib/features/issues/IssuePriorityIcon.svelte';
	import * as Popover from '$lib/components/ui/popover';

	let {
		filters = $bindable({}),
		onchange
	}: {
		filters: Record<string, string>;
		onchange: (filters: Record<string, string>) => void;
	} = $props();

	let statusOpen = $state(false);
	let priorityOpen = $state(false);

	function setFilter(key: string, value: string) {
		if (value) {
			filters[key] = value;
		} else {
			delete filters[key];
		}
		filters = { ...filters };
		onchange(filters);
	}

	let statusLabel = $derived.by(() => {
		if (!filters.status) return 'All statuses';
		const ts = teamStatusesState.statusById.get(filters.status);
		return ts ? ts.name : filters.status;
	});
	let priorityLabel = $derived(filters.priority ? getPriorityLabel(Number(filters.priority) as IssuePriority) : 'All priorities');
</script>

<div class="flex items-center gap-2 border-b border-[var(--app-border)] px-4 py-2">
	<Popover.Root bind:open={statusOpen}>
		<Popover.Trigger>
			{#snippet child({ props })}
				<Button {...props} variant="plain" class="flex items-center gap-1.5 rounded-md border border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-2 py-1 text-xs text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)]">
					{#if filters.status}
						{@const ts = teamStatusesState.statusById.get(filters.status)}
						<IssueStatusIcon category={ts?.category} color={ts?.color} size={12} />
					{/if}
					{statusLabel}
				</Button>
			{/snippet}
		</Popover.Trigger>
		<Popover.Content class="w-40 p-1" align="start">
			<Button variant="plain" onclick={() => { setFilter('status', ''); statusOpen = false; }}
				class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)] {!filters.status ? 'bg-[var(--color-bg-hover)]' : ''}"
			>
				All statuses
			</Button>
			{#each teamStatusesState.statusOrder as ts}
				<Button variant="plain" onclick={() => { setFilter('status', ts.id); statusOpen = false; }}
					class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)] {filters.status === ts.id ? 'bg-[var(--color-bg-hover)]' : ''}"
				>
					<IssueStatusIcon category={ts.category} color={ts.color} size={14} />
					{ts.name}
				</Button>
			{/each}
		</Popover.Content>
	</Popover.Root>

	<Popover.Root bind:open={priorityOpen}>
		<Popover.Trigger>
			{#snippet child({ props })}
				<Button {...props} variant="plain" class="flex items-center gap-1.5 rounded-md border border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-2 py-1 text-xs text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)]">
					{#if filters.priority}
						<IssuePriorityIcon priority={Number(filters.priority) as IssuePriority} size={12} />
					{/if}
					{priorityLabel}
				</Button>
			{/snippet}
		</Popover.Trigger>
		<Popover.Content class="w-40 p-1" align="start">
			<Button variant="plain" onclick={() => { setFilter('priority', ''); priorityOpen = false; }}
				class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)] {!filters.priority ? 'bg-[var(--color-bg-hover)]' : ''}"
			>
				All priorities
			</Button>
			{#each Object.entries(getPriorityLabels()) as [value, label]}
				<Button variant="plain" onclick={() => { setFilter('priority', value); priorityOpen = false; }}
					class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)] {filters.priority === value ? 'bg-[var(--color-bg-hover)]' : ''}"
				>
					<IssuePriorityIcon priority={Number(value) as IssuePriority} size={14} />
					{label}
				</Button>
			{/each}
		</Popover.Content>
	</Popover.Root>

	{#if Object.keys(filters).length > 0}
		<Button variant="plain" onclick={() => {
				filters = {};
				onchange({});
			}}
			class="text-xs text-[var(--color-text-tertiary)] hover:text-[var(--color-text-secondary)]"
		>
			Clear filters
		</Button>
	{/if}
</div>
