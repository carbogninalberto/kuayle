<script lang="ts">
	import * as Popover from '$lib/components/ui/popover';
	import { Button } from '$lib/components/ui/button';
	import { Checkbox } from '$lib/components/ui/checkbox';
	import { Separator } from '$lib/components/ui/separator';
	import type { ViewFilter } from '$lib/types/view';
	import type { IssueStatus, IssuePriority } from '$lib/types/issue';
	import { STATUS_LABELS, PRIORITY_LABELS } from '$lib/types/issue';
	import type { Team } from '$lib/types/team';
	import type { Project } from '$lib/types/project';
	import type { Label } from '$lib/types/label';
	import type { WorkspaceMember } from '$lib/types/workspace';
	import IssueStatusIcon from '$lib/features/issues/IssueStatusIcon.svelte';
	import IssuePriorityIcon from '$lib/features/issues/IssuePriorityIcon.svelte';
	import {
		Plus,
		X,
		Search,
		CircleDashed,
		Signal,
		User,
		FolderKanban,
		Tag
	} from 'lucide-svelte';

	let {
		filters = $bindable<ViewFilter>({}),
		teams = [],
		projects = [],
		labels = [],
		members = [],
		onchange
	}: {
		filters: ViewFilter;
		teams?: Team[];
		projects?: Project[];
		labels?: Label[];
		members?: WorkspaceMember[];
		onchange: (filters: ViewFilter) => void;
	} = $props();

	let addFilterOpen = $state(false);
	let statusOpen = $state(false);
	let priorityOpen = $state(false);
	let assigneeOpen = $state(false);
	let projectOpen = $state(false);
	let labelOpen = $state(false);
	let searchValue = $state(filters.search ?? '');
	let searchTimeout: ReturnType<typeof setTimeout>;

	// Track which filter chips are visible (active value OR just added via "Add filter")
	let visibleFilters = $state<Set<string>>(new Set(
		Object.entries(filters)
			.filter(([_, v]) => v !== undefined && v !== '')
			.map(([k]) => k)
	));

	// Keep visibleFilters in sync when filters change externally
	$effect(() => {
		const activeKeys = Object.entries(filters)
			.filter(([_, v]) => v !== undefined && v !== '')
			.map(([k]) => k);
		for (const key of activeKeys) {
			if (!visibleFilters.has(key)) {
				visibleFilters.add(key);
				visibleFilters = new Set(visibleFilters);
			}
		}
	});

	// Which filter types are available to add
	const FILTER_OPTIONS = [
		{ key: 'status', label: 'Status', icon: CircleDashed },
		{ key: 'priority', label: 'Priority', icon: Signal },
		{ key: 'assignee', label: 'Assignee', icon: User },
		{ key: 'project', label: 'Project', icon: FolderKanban },
		{ key: 'label', label: 'Label', icon: Tag }
	] as const;

	let availableFilters = $derived(
		FILTER_OPTIONS.filter((f) => !visibleFilters.has(f.key))
	);

	// Helpers for multi-value filters
	function getStatusValues(): string[] {
		return filters.status ? filters.status.split(',') : [];
	}
	function getPriorityValues(): string[] {
		return filters.priority ? filters.priority.split(',') : [];
	}

	function toggleStatus(value: string) {
		const current = getStatusValues();
		const next = current.includes(value)
			? current.filter((v) => v !== value)
			: [...current, value];
		updateFilter('status', next.length > 0 ? next.join(',') : undefined);
	}

	function togglePriority(value: string) {
		const current = getPriorityValues();
		const next = current.includes(value)
			? current.filter((v) => v !== value)
			: [...current, value];
		updateFilter('priority', next.length > 0 ? next.join(',') : undefined);
	}

	function updateFilter(key: string, value: string | undefined) {
		if (value === undefined || value === '') {
			const { [key]: _, ...rest } = filters;
			filters = rest;
		} else {
			filters = { ...filters, [key]: value };
		}
		onchange(filters);
	}

	function removeFilter(key: string) {
		const { [key]: _, ...rest } = filters;
		filters = rest;
		visibleFilters.delete(key);
		visibleFilters = new Set(visibleFilters);
		onchange(filters);
	}

	function clearAll() {
		filters = {};
		searchValue = '';
		visibleFilters = new Set();
		onchange({});
	}

	function handleSearchInput(e: Event) {
		const value = (e.target as HTMLInputElement).value;
		searchValue = value;
		clearTimeout(searchTimeout);
		searchTimeout = setTimeout(() => {
			updateFilter('search', value.trim() || undefined);
		}, 300);
	}

	function addFilter(key: string) {
		addFilterOpen = false;
		// Make the chip visible first, then open its popover on next tick
		visibleFilters.add(key);
		visibleFilters = new Set(visibleFilters);
		// Use tick to ensure the popover DOM is rendered before opening
		requestAnimationFrame(() => {
			switch (key) {
				case 'status': statusOpen = true; break;
				case 'priority': priorityOpen = true; break;
				case 'assignee': assigneeOpen = true; break;
				case 'project': projectOpen = true; break;
				case 'label': labelOpen = true; break;
			}
		});
	}

	// When a popover closes without a value selected, remove from visible
	function handlePopoverClose(key: string, isOpen: boolean) {
		if (!isOpen && !filters[key]) {
			visibleFilters.delete(key);
			visibleFilters = new Set(visibleFilters);
		}
	}

	// Display labels for active chips
	function getStatusChipLabel(): string {
		const vals = getStatusValues();
		if (vals.length === 0) return 'Status';
		if (vals.length === 1) return STATUS_LABELS[vals[0] as IssueStatus] ?? vals[0];
		return `${vals.length} statuses`;
	}

	function getPriorityChipLabel(): string {
		const vals = getPriorityValues();
		if (vals.length === 0) return 'Priority';
		if (vals.length === 1) return PRIORITY_LABELS[Number(vals[0]) as IssuePriority] ?? vals[0];
		return `${vals.length} priorities`;
	}

	function getAssigneeChipLabel(): string {
		if (!filters.assignee) return 'Assignee';
		if (filters.assignee === 'none') return 'Unassigned';
		const m = members.find((m) => m.user_id === filters.assignee);
		return m?.name || m?.email || 'Assignee';
	}

	function getProjectChipLabel(): string {
		if (!filters.project) return 'Project';
		if (filters.project === 'none') return 'No project';
		const p = projects.find((p) => p.id === filters.project);
		return p?.name || 'Project';
	}

	function getLabelChipLabel(): string {
		if (!filters.label) return 'Label';
		const l = labels.find((l) => l.id === filters.label);
		return l?.name || 'Label';
	}

	function chipClass(hasValue: boolean): string {
		return hasValue
			? 'flex items-center gap-1 rounded-md border border-[var(--app-accent)]/30 bg-[var(--app-accent)]/10 px-2 py-0.5 text-xs text-[var(--app-accent-light)] hover:bg-[var(--app-accent)]/20'
			: 'flex items-center gap-1 rounded-md border border-[var(--app-border)] px-2 py-0.5 text-xs text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)]';
	}
</script>

<div class="flex items-center gap-1.5 px-2 py-2">
	<!-- Search input -->
	<div class="relative">
		<Search size={14} class="absolute left-2 top-1/2 -translate-y-1/2 text-[var(--color-text-tertiary)]" />
		<input
			type="text"
			value={searchValue}
			oninput={handleSearchInput}
			placeholder="Search..."
			class="h-7 w-40 rounded-md border border-[var(--app-border)] bg-[var(--color-bg-secondary)] pl-7 pr-2 text-xs text-[var(--color-text-primary)] outline-none placeholder:text-[var(--color-text-tertiary)] focus:border-[var(--app-accent)]"
		/>
	</div>

	<!-- Filter chips -->
	{#if visibleFilters.has('status')}
		<Popover.Root bind:open={statusOpen} onOpenChange={(open) => handlePopoverClose('status', open)}>
			<Popover.Trigger>
				<button class={chipClass(!!filters.status)}>
					<CircleDashed size={12} />
					{getStatusChipLabel()}
				</button>
			</Popover.Trigger>
			<Popover.Content class="w-44 p-1" align="start">
				{#each Object.entries(STATUS_LABELS) as [value, label]}
					<button
						onclick={() => toggleStatus(value)}
						class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)]"
					>
						<Checkbox checked={getStatusValues().includes(value)} />
						<IssueStatusIcon status={value as IssueStatus} />
						{label}
					</button>
				{/each}
				<Separator class="my-1" />
				<button
					onclick={() => removeFilter('status')}
					class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-xs text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)]"
				>
					<X size={12} />
					Remove filter
				</button>
			</Popover.Content>
		</Popover.Root>
	{/if}

	{#if visibleFilters.has('priority')}
		<Popover.Root bind:open={priorityOpen} onOpenChange={(open) => handlePopoverClose('priority', open)}>
			<Popover.Trigger>
				<button class={chipClass(!!filters.priority)}>
					<Signal size={12} />
					{getPriorityChipLabel()}
				</button>
			</Popover.Trigger>
			<Popover.Content class="w-44 p-1" align="start">
				{#each Object.entries(PRIORITY_LABELS) as [value, label]}
					<button
						onclick={() => togglePriority(value)}
						class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)]"
					>
						<Checkbox checked={getPriorityValues().includes(value)} />
						<IssuePriorityIcon priority={Number(value) as IssuePriority} />
						{label}
					</button>
				{/each}
				<Separator class="my-1" />
				<button
					onclick={() => removeFilter('priority')}
					class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-xs text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)]"
				>
					<X size={12} />
					Remove filter
				</button>
			</Popover.Content>
		</Popover.Root>
	{/if}

	{#if visibleFilters.has('assignee')}
		<Popover.Root bind:open={assigneeOpen} onOpenChange={(open) => handlePopoverClose('assignee', open)}>
			<Popover.Trigger>
				<button class={chipClass(!!filters.assignee)}>
					<User size={12} />
					{getAssigneeChipLabel()}
				</button>
			</Popover.Trigger>
			<Popover.Content class="w-48 p-1" align="start">
				<button
					onclick={() => updateFilter('assignee', 'none')}
					class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)] {filters.assignee === 'none' ? 'bg-[var(--color-bg-hover)]' : ''}"
				>
					Unassigned
				</button>
				{#each members as member}
					<button
						onclick={() => updateFilter('assignee', member.user_id)}
						class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)] {filters.assignee === member.user_id ? 'bg-[var(--color-bg-hover)]' : ''}"
					>
						<User size={14} class="text-[var(--color-text-tertiary)]" />
						{member.name || member.email}
					</button>
				{/each}
				<Separator class="my-1" />
				<button
					onclick={() => removeFilter('assignee')}
					class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-xs text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)]"
				>
					<X size={12} />
					Remove filter
				</button>
			</Popover.Content>
		</Popover.Root>
	{/if}

	{#if visibleFilters.has('project')}
		<Popover.Root bind:open={projectOpen} onOpenChange={(open) => handlePopoverClose('project', open)}>
			<Popover.Trigger>
				<button class={chipClass(!!filters.project)}>
					<FolderKanban size={12} />
					{getProjectChipLabel()}
				</button>
			</Popover.Trigger>
			<Popover.Content class="w-48 p-1" align="start">
				<button
					onclick={() => updateFilter('project', 'none')}
					class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)] {filters.project === 'none' ? 'bg-[var(--color-bg-hover)]' : ''}"
				>
					No project
				</button>
				{#each projects as project}
					<button
						onclick={() => updateFilter('project', project.id)}
						class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)] {filters.project === project.id ? 'bg-[var(--color-bg-hover)]' : ''}"
					>
						<FolderKanban size={14} class="text-[var(--color-text-tertiary)]" />
						{project.name}
					</button>
				{/each}
				<Separator class="my-1" />
				<button
					onclick={() => removeFilter('project')}
					class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-xs text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)]"
				>
					<X size={12} />
					Remove filter
				</button>
			</Popover.Content>
		</Popover.Root>
	{/if}

	{#if visibleFilters.has('label')}
		<Popover.Root bind:open={labelOpen} onOpenChange={(open) => handlePopoverClose('label', open)}>
			<Popover.Trigger>
				<button class={chipClass(!!filters.label)}>
					<Tag size={12} />
					{getLabelChipLabel()}
				</button>
			</Popover.Trigger>
			<Popover.Content class="w-48 p-1" align="start">
				{#each labels as label}
					<button
						onclick={() => updateFilter('label', label.id)}
						class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)] {filters.label === label.id ? 'bg-[var(--color-bg-hover)]' : ''}"
					>
						<div class="h-2.5 w-2.5 rounded-full shrink-0" style="background-color: {label.color}"></div>
						{label.name}
					</button>
				{/each}
				{#if labels.length === 0}
					<p class="px-2 py-3 text-center text-xs text-[var(--color-text-tertiary)]">No labels</p>
				{/if}
				<Separator class="my-1" />
				<button
					onclick={() => removeFilter('label')}
					class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-xs text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)]"
				>
					<X size={12} />
					Remove filter
				</button>
			</Popover.Content>
		</Popover.Root>
	{/if}

	<!-- Add filter button -->
	{#if availableFilters.length > 0}
		<Popover.Root bind:open={addFilterOpen}>
			<Popover.Trigger>
				<button class="flex items-center gap-1 rounded-md px-1.5 py-0.5 text-xs text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-secondary)]">
					<Plus size={14} />
					Filter
				</button>
			</Popover.Trigger>
			<Popover.Content class="w-44 p-1" align="start">
				{#each availableFilters as option}
					<button
						onclick={() => addFilter(option.key)}
						class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)]"
					>
						<svelte:component this={option.icon} size={14} class="text-[var(--color-text-tertiary)]" />
						{option.label}
					</button>
				{/each}
			</Popover.Content>
		</Popover.Root>
	{/if}

	<!-- Spacer -->
	<div class="flex-1"></div>

	<!-- Clear all -->
	{#if visibleFilters.size > 0}
		<button
			onclick={clearAll}
			class="text-xs text-[var(--color-text-tertiary)] hover:text-[var(--color-text-secondary)]"
		>
			Clear filters
		</button>
	{/if}
</div>
