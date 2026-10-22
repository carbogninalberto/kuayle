<script lang="ts">
	import { Input } from '$lib/components/ui/input/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import * as Popover from '$lib/components/ui/popover';
	import * as HoverCard from '$lib/components/ui/hover-card';
	import { Checkbox } from '$lib/components/ui/checkbox';
	import { Separator } from '$lib/components/ui/separator';
	import type { ViewFilter } from '$lib/types/view';
	import type { IssuePriority, IssueStatus } from '$lib/types/issue';
	import { getPriorityLabel, getPriorityLabels, getStatusLabel } from '$lib/types/issue';
	import { teamStatusesState } from '$lib/features/issues/team-statuses.state.svelte';
	import type { Team } from '$lib/types/team';
	import type { Project } from '$lib/types/project';
	import type { Label } from '$lib/types/label';
	import type { WorkspaceMember } from '$lib/types/workspace';
	import IssueStatusIcon from '$lib/features/issues/IssueStatusIcon.svelte';
	import IssuePriorityIcon from '$lib/features/issues/IssuePriorityIcon.svelte';
	import { Plus, X, Search, CircleDashed, Signal, User, FolderKanban, Tag, CornerDownRight } from '@lucide/svelte';
	import { m } from '$lib/paraglide/messages.js';
	import { getLocale, setLocale } from '$lib/paraglide/runtime.js';

	let {
		filters = $bindable<ViewFilter>({}),
		teams = [],
		projects = [],
		labels = [],
		members = [],
		readonly = false,
		excludedFilters = [],
		onchange
	}: {
		filters: ViewFilter;
		teams?: Team[];
		projects?: Project[];
		labels?: Label[];
		members?: WorkspaceMember[];
		readonly?: boolean;
		excludedFilters?: string[];
		onchange: (filters: ViewFilter) => void;
	} = $props();

	let addFilterOpen = $state(false);
	let statusOpen = $state(false);
	let priorityOpen = $state(false);
	let assigneeOpen = $state(false);
	let projectOpen = $state(false);
	let labelOpen = $state(false);
	let subIssuesOpen = $state(false);
	let searchValue = $state(filters.search ?? '');
	let searchTimeout: ReturnType<typeof setTimeout>;

	// Track which filter chips are visible (active value OR just added via "Add filter")
	let visibleFilters = $state<Set<string>>(
		new Set(
			Object.entries(filters)
				.filter(([_, v]) => v !== undefined && v !== '')
				.map(([k]) => k)
		)
	);

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
	const FILTER_OPTIONS = $derived([
		{ key: 'status', label: m['sharedComponents.filter_builder.status'](), icon: CircleDashed },
		{ key: 'priority', label: m['sharedComponents.filter_builder.priority'](), icon: Signal },
		{ key: 'assignee', label: m['sharedComponents.filter_builder.assignee'](), icon: User },
		{ key: 'project', label: m['sharedComponents.filter_builder.project'](), icon: FolderKanban },
		{ key: 'label', label: m['sharedComponents.filter_builder.label'](), icon: Tag },
		{ key: 'sub_issues', label: m['sharedComponents.filter_builder.sub_issues'](), icon: CornerDownRight }
	]);

	const SUB_ISSUE_FILTERS = $derived([
		{ value: 'include', label: m['sharedComponents.filter_builder.show_all_issues']() },
		{ value: 'exclude', label: m['sharedComponents.filter_builder.hide_sub_issues']() },
		{ value: 'only', label: m['sharedComponents.filter_builder.only_sub_issues']() },
		{ value: 'has_sub_issues', label: m['sharedComponents.filter_builder.has_sub_issues']() }
	]);

	let availableFilters = $derived(
		FILTER_OPTIONS.filter((f) => !excludedFilters.includes(f.key) && !visibleFilters.has(f.key))
	);

	// Helpers for multi-value filters
	function getStatusValues(): string[] {
		return filters.status ? filters.status.split(',') : [];
	}
	function getPriorityValues(): string[] {
		return filters.priority ? filters.priority.split(',') : [];
	}

	function getStatusByValue(value: string) {
		return teamStatusesState.statusById.get(value) ?? teamStatusesState.statusOrder.find((ts) => ts.slug === value);
	}

	function statusValueMatches(status: { id: string; slug?: string }, value: string): boolean {
		return status.id === value || status.slug === value;
	}

	function toggleStatus(value: string) {
		const current = getStatusValues();
		const next = current.includes(value) ? current.filter((v) => v !== value) : [...current, value];
		updateFilter('status', next.length > 0 ? next.join(',') : undefined);
	}

	function togglePriority(value: string) {
		const current = getPriorityValues();
		const next = current.includes(value) ? current.filter((v) => v !== value) : [...current, value];
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
				case 'status':
					statusOpen = true;
					break;
				case 'priority':
					priorityOpen = true;
					break;
				case 'assignee':
					assigneeOpen = true;
					break;
				case 'project':
					projectOpen = true;
					break;
				case 'label':
					labelOpen = true;
					break;
				case 'sub_issues':
					subIssuesOpen = true;
					break;
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
		if (vals.length === 0) return m['sharedComponents.filter_builder.status']();
		if (vals.length === 1) {
			const ts = getStatusByValue(vals[0]);
			return ts ? ts.name : (getStatusLabel(vals[0] as IssueStatus) ?? vals[0]);
		}
		return m['sharedComponents.filter_builder.statuses_count']({ count: vals.length });
	}

	function getPriorityChipLabel(): string {
		const vals = getPriorityValues();
		if (vals.length === 0) return m['sharedComponents.filter_builder.priority']();
		if (vals.length === 1) return getPriorityLabel(Number(vals[0]) as IssuePriority) ?? vals[0];
		return m['sharedComponents.filter_builder.priorities_count']({ count: vals.length });
	}

	function getAssigneeChipLabel(): string {
		if (!filters.assignee) return m['sharedComponents.filter_builder.assignee']();
		if (filters.assignee === 'none') return m['sharedComponents.filter_builder.unassigned']();
		const member = members.find((member) => member.user_id === filters.assignee);
		return member?.name || member?.email || m['sharedComponents.filter_builder.assignee']();
	}

	function getProjectChipLabel(): string {
		if (!filters.project) return m['sharedComponents.filter_builder.project']();
		if (filters.project === 'none') return m['sharedComponents.filter_builder.no_project']();
		const p = projects.find((p) => p.id === filters.project);
		return p?.name || m['sharedComponents.filter_builder.project']();
	}

	function getLabelChipLabel(): string {
		if (!filters.label) return m['sharedComponents.filter_builder.label']();
		if (filters.label === 'none') return m['sharedComponents.filter_builder.no_label']();
		const l = labels.find((l) => l.id === filters.label);
		return l?.name || m['sharedComponents.filter_builder.label']();
	}

	function getSubIssuesChipLabel(): string {
		return (
			SUB_ISSUE_FILTERS.find((option) => option.value === filters.sub_issues)?.label ??
			m['sharedComponents.filter_builder.sub_issues']()
		);
	}

	function getChipLabel(key: string): string {
		switch (key) {
			case 'status':
				return getStatusChipLabel();
			case 'priority':
				return getPriorityChipLabel();
			case 'assignee':
				return getAssigneeChipLabel();
			case 'project':
				return getProjectChipLabel();
			case 'label':
				return getLabelChipLabel();
			case 'status_type':
				return m['sharedComponents.filter_builder.status_type']({ type: filters.status_type ?? '' });
			case 'cycle':
				return filters.cycle === 'none'
					? m['sharedComponents.filter_builder.no_cycle']()
					: m['sharedComponents.filter_builder.cycle']();
			case 'team':
				return m['sharedComponents.filter_builder.team']();
			case 'creator':
				return m['sharedComponents.filter_builder.creator']();
			case 'sub_issues':
				return getSubIssuesChipLabel();
			default:
				return key;
		}
	}

	function chipClass(hasValue: boolean): string {
		return hasValue
			? 'flex h-8 shrink-0 items-center gap-1 rounded-md border border-[var(--app-accent)]/30 bg-[var(--app-accent)]/10 px-2.5 text-xs text-[var(--app-accent-light)] hover:bg-[var(--app-accent)]/20 sm:h-auto sm:px-2 sm:py-0.5'
			: 'flex h-8 shrink-0 items-center gap-1 rounded-md border border-[var(--app-border)] px-2.5 text-xs text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)] sm:h-auto sm:px-2 sm:py-0.5';
	}
</script>

<div
	class="no-scrollbar flex shrink-0 max-w-full items-center gap-1.5 overflow-x-auto overflow-y-hidden px-2 py-1.5 sm:overflow-visible sm:py-2"
	style="-webkit-overflow-scrolling: touch;"
>
	{#if !readonly}
		<!-- Search input -->
		<div class="relative shrink-0">
			<Search size={14} class="absolute left-2 top-1/2 -translate-y-1/2 text-[var(--color-text-tertiary)]" />
			<Input
				type="text"
				value={searchValue}
				oninput={handleSearchInput}
				placeholder={m['sharedComponents.filter_builder.search_placeholder']()}
				class="h-8 w-[min(58vw,14rem)] pl-7 pr-2 text-xs sm:h-7 sm:w-40"
			/>
		</div>
	{/if}

	<!-- Filter chips -->
	{#if readonly}
		{#snippet readonlyContent(key: string)}
			{#if key === 'status'}
				{@const statusValues = getStatusValues()}
				{#if teamStatusesState.statusOrder.length > 0}
					{#each statusValues.filter((value) => !teamStatusesState.statusOrder.some( (ts) => statusValueMatches(ts, value) )) as value}
						<div class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-primary)]">
							<Checkbox checked class="pointer-events-none" />
							<IssueStatusIcon status={value} size={14} />
							{getStatusLabel(value as IssueStatus) ?? value}
						</div>
					{/each}
					{#each teamStatusesState.statusOrder as ts}
						{@const selected = statusValues.some((value) => statusValueMatches(ts, value))}
						<div class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-primary)]">
							<Checkbox checked={selected} class="pointer-events-none" />
							<IssueStatusIcon category={ts.category} color={ts.color} size={14} />
							{ts.name}
						</div>
					{/each}
				{:else}
					{#each statusValues as id}
						<div class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-primary)]">
							<Checkbox checked class="pointer-events-none" />
							<IssueStatusIcon status={id} size={14} />
							{id}
						</div>
					{/each}
				{/if}
			{:else if key === 'priority'}
				{#each Object.entries(getPriorityLabels()) as [value, label]}
					{@const selected = getPriorityValues().includes(value)}
					<div class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-primary)]">
						<Checkbox checked={selected} class="pointer-events-none" />
						<IssuePriorityIcon priority={Number(value) as IssuePriority} size={14} />
						{label}
					</div>
				{/each}
			{:else if key === 'assignee'}
				<div class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-tertiary)]">
					{m['sharedComponents.filter_builder.unassigned']()}
				</div>
				{#each members as member}
					{@const selected = filters.assignee === member.user_id}
					<div class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-primary)]">
						<User size={14} class="text-[var(--color-text-tertiary)]" />
						{member.name || member.email}
					</div>
				{/each}
			{:else if key === 'project'}
				<div class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-tertiary)]">
					{m['sharedComponents.filter_builder.no_project']()}
				</div>
				{#each projects as project}
					{@const selected = filters.project === project.id}
					<div class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-primary)]">
						<FolderKanban size={14} class="text-[var(--color-text-tertiary)]" />
						{project.name}
					</div>
				{/each}
			{:else if key === 'label'}
				{#each labels as label}
					{@const selected = filters.label === label.id}
					<div class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-primary)]">
						<div class="h-2.5 w-2.5 shrink-0 rounded-full" style="background-color: {label.color}"></div>
						{label.name}
					</div>
				{/each}
				{#if labels.length === 0}
					<p class="px-2 py-3 text-center text-xs text-[var(--color-text-tertiary)]">
						{m['sharedComponents.filter_builder.no_labels']()}
					</p>
				{/if}
			{:else if key === 'sub_issues'}
				{#each SUB_ISSUE_FILTERS as option}
					<div class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-primary)]">
						<Checkbox checked={filters.sub_issues === option.value} class="pointer-events-none" />
						{option.label}
					</div>
				{/each}
			{/if}
		{/snippet}

		{#each FILTER_OPTIONS as option}
			{#if filters[option.key]}
				<HoverCard.Root openDelay={150} closeDelay={100}>
					<HoverCard.Trigger class={chipClass(true)}>
						<option.icon size={12} />
						{getChipLabel(option.key)}
					</HoverCard.Trigger>
					<HoverCard.Content class="w-48 p-1" align="start">
						{@render readonlyContent(option.key)}
					</HoverCard.Content>
				</HoverCard.Root>
			{/if}
		{/each}
	{:else}
		{#if visibleFilters.has('status')}
			<Popover.Root bind:open={statusOpen} onOpenChange={(open) => handlePopoverClose('status', open)}>
				<Popover.Trigger>
					{#snippet child({ props })}
						<Button {...props} variant="ghost" size="sm" class={chipClass(!!filters.status)}>
							<CircleDashed size={12} />
							{getStatusChipLabel()}
						</Button>
					{/snippet}
				</Popover.Trigger>
				<Popover.Content class="w-44 p-1" align="start">
					{#each teamStatusesState.statusOrder as ts}
						<Button variant="ghost" size="sm"
							onclick={() => toggleStatus(ts.id)}
							class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-primary)]"
						>
							<Checkbox checked={getStatusValues().includes(ts.id)} />
							<IssueStatusIcon category={ts.category} color={ts.color} />
							{ts.name}
						</Button>
					{/each}
					<Separator class="my-1" />
					<Button variant="ghost" size="sm"
						onclick={() => removeFilter('status')}
						class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-xs text-[var(--color-text-tertiary)]"
					>
						<X size={12} />
						{m['sharedComponents.filter_builder.remove_filter']()}
					</Button>
				</Popover.Content>
			</Popover.Root>
		{/if}

		{#if visibleFilters.has('priority')}
			<Popover.Root bind:open={priorityOpen} onOpenChange={(open) => handlePopoverClose('priority', open)}>
				<Popover.Trigger>
					{#snippet child({ props })}
						<Button {...props} variant="ghost" size="sm" class={chipClass(!!filters.priority)}>
							<Signal size={12} />
							{getPriorityChipLabel()}
						</Button>
					{/snippet}
				</Popover.Trigger>
				<Popover.Content class="w-44 p-1" align="start">
					{#each Object.entries(getPriorityLabels()) as [value, label]}
						<Button variant="ghost" size="sm"
							onclick={() => togglePriority(value)}
							class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-primary)]"
						>
							<Checkbox checked={getPriorityValues().includes(value)} />
							<IssuePriorityIcon priority={Number(value) as IssuePriority} />
							{label}
						</Button>
					{/each}
					<Separator class="my-1" />
					<Button variant="ghost" size="sm"
						onclick={() => removeFilter('priority')}
						class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-xs text-[var(--color-text-tertiary)]"
					>
						<X size={12} />
						{m['sharedComponents.filter_builder.remove_filter']()}
					</Button>
				</Popover.Content>
			</Popover.Root>
		{/if}

		{#if visibleFilters.has('assignee')}
			<Popover.Root bind:open={assigneeOpen} onOpenChange={(open) => handlePopoverClose('assignee', open)}>
				<Popover.Trigger>
					{#snippet child({ props })}
						<Button {...props} variant="ghost" size="sm" class={chipClass(!!filters.assignee)}>
							<User size={12} />
							{getAssigneeChipLabel()}
						</Button>
					{/snippet}
				</Popover.Trigger>
				<Popover.Content class="w-48 p-1" align="start">
					<Button variant="ghost" size="sm"
						onclick={() => updateFilter('assignee', 'none')}
						class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)] {filters.assignee ===
						'none'
							? 'bg-[var(--color-bg-hover)]'
							: ''}"
					>
						{m['sharedComponents.filter_builder.unassigned']()}
					</Button>
					{#each members as member}
						<Button variant="ghost" size="sm"
							onclick={() => updateFilter('assignee', member.user_id)}
							class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)] {filters.assignee ===
							member.user_id
								? 'bg-[var(--color-bg-hover)]'
								: ''}"
						>
							<User size={14} class="text-[var(--color-text-tertiary)]" />
							{member.name || member.email}
						</Button>
					{/each}
					<Separator class="my-1" />
					<Button variant="ghost" size="sm"
						onclick={() => removeFilter('assignee')}
						class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-xs text-[var(--color-text-tertiary)]"
					>
						<X size={12} />
						{m['sharedComponents.filter_builder.remove_filter']()}
					</Button>
				</Popover.Content>
			</Popover.Root>
		{/if}

		{#if visibleFilters.has('project')}
			<Popover.Root bind:open={projectOpen} onOpenChange={(open) => handlePopoverClose('project', open)}>
				<Popover.Trigger>
					{#snippet child({ props })}
						<Button {...props} variant="ghost" size="sm" class={chipClass(!!filters.project)}>
							<FolderKanban size={12} />
							{getProjectChipLabel()}
						</Button>
					{/snippet}
				</Popover.Trigger>
				<Popover.Content class="w-48 p-1" align="start">
					<Button variant="ghost" size="sm"
						onclick={() => updateFilter('project', 'none')}
						class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)] {filters.project ===
						'none'
							? 'bg-[var(--color-bg-hover)]'
							: ''}"
					>
						{m['sharedComponents.filter_builder.no_project']()}
					</Button>
					{#each projects as project}
						<Button variant="ghost" size="sm"
							onclick={() => updateFilter('project', project.id)}
							class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)] {filters.project ===
							project.id
								? 'bg-[var(--color-bg-hover)]'
								: ''}"
						>
							<FolderKanban size={14} class="text-[var(--color-text-tertiary)]" />
							{project.name}
						</Button>
					{/each}
					<Separator class="my-1" />
					<Button variant="ghost" size="sm"
						onclick={() => removeFilter('project')}
						class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-xs text-[var(--color-text-tertiary)]"
					>
						<X size={12} />
						{m['sharedComponents.filter_builder.remove_filter']()}
					</Button>
				</Popover.Content>
			</Popover.Root>
		{/if}

		{#if visibleFilters.has('label')}
			<Popover.Root bind:open={labelOpen} onOpenChange={(open) => handlePopoverClose('label', open)}>
				<Popover.Trigger>
					{#snippet child({ props })}
						<Button {...props} variant="ghost" size="sm" class={chipClass(!!filters.label)}>
							<Tag size={12} />
							{getLabelChipLabel()}
						</Button>
					{/snippet}
				</Popover.Trigger>
				<Popover.Content class="w-48 p-1" align="start">
					{#each labels as label}
						<Button variant="ghost" size="sm"
							onclick={() => updateFilter('label', label.id)}
							class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)] {filters.label ===
							label.id
								? 'bg-[var(--color-bg-hover)]'
								: ''}"
						>
							<div class="h-2.5 w-2.5 rounded-full shrink-0" style="background-color: {label.color}"></div>
							{label.name}
						</Button>
					{/each}
					{#if labels.length === 0}
						<p class="px-2 py-3 text-center text-xs text-[var(--color-text-tertiary)]">
							{m['sharedComponents.filter_builder.no_labels']()}
						</p>
					{/if}
					<Separator class="my-1" />
					<Button variant="ghost" size="sm"
						onclick={() => removeFilter('label')}
						class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-xs text-[var(--color-text-tertiary)]"
					>
						<X size={12} />
						{m['sharedComponents.filter_builder.remove_filter']()}
					</Button>
				</Popover.Content>
			</Popover.Root>
		{/if}

		{#if visibleFilters.has('sub_issues')}
			<Popover.Root bind:open={subIssuesOpen} onOpenChange={(open) => handlePopoverClose('sub_issues', open)}>
				<Popover.Trigger>
					{#snippet child({ props })}
						<Button {...props} variant="ghost" size="sm" class={chipClass(!!filters.sub_issues)}>
							<CornerDownRight size={12} />
							{getSubIssuesChipLabel()}
						</Button>
					{/snippet}
				</Popover.Trigger>
				<Popover.Content class="w-52 p-1" align="start">
					{#each SUB_ISSUE_FILTERS as option}
						<Button variant="ghost" size="sm"
							onclick={() => updateFilter('sub_issues', option.value)}
							class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)] {filters.sub_issues ===
							option.value
								? 'bg-[var(--color-bg-hover)]'
								: ''}"
						>
							<Checkbox checked={filters.sub_issues === option.value} />
							{option.label}
						</Button>
					{/each}
					<Separator class="my-1" />
					<Button variant="ghost" size="sm"
						onclick={() => removeFilter('sub_issues')}
						class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-xs text-[var(--color-text-tertiary)]"
					>
						<X size={12} />
						{m['sharedComponents.filter_builder.remove_filter']()}
					</Button>
				</Popover.Content>
			</Popover.Root>
		{/if}

		{#each Array.from(visibleFilters).filter((key) => !FILTER_OPTIONS.some((option) => option.key === key)) as key}
			{#if filters[key]}
				<Button variant="ghost" size="sm"
					class={chipClass(true)}
					onclick={() => removeFilter(key)}
					title={m['sharedComponents.filter_builder.remove_filter']()}
				>
					{getChipLabel(key)}
					<X size={12} />
				</Button>
			{/if}
		{/each}

		<!-- Add filter button -->
		{#if availableFilters.length > 0}
			<Popover.Root bind:open={addFilterOpen}>
				<Popover.Trigger>
					{#snippet child({ props })}
						<Button {...props} variant="ghost" size="sm"
							class="flex items-center gap-1 rounded-md px-1.5 py-0.5 text-xs text-[var(--color-text-tertiary)] hover:text-[var(--color-text-secondary)]"
						>
							<Plus size={14} />
							{m['sharedComponents.filter_builder.filter']()}
						</Button>
					{/snippet}
				</Popover.Trigger>
				<Popover.Content class="w-44 p-1" align="start">
					{#each availableFilters as option}
						<Button variant="ghost" size="sm"
							onclick={() => addFilter(option.key)}
							class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-primary)]"
						>
							<option.icon size={14} class="text-[var(--color-text-tertiary)]" />
							{option.label}
						</Button>
					{/each}
				</Popover.Content>
			</Popover.Root>
		{/if}

		<!-- Spacer -->
		<div class="flex-1"></div>

		<!-- Clear all -->
		{#if visibleFilters.size > 0}
			<Button variant="ghost" size="sm"
				onclick={clearAll}
				class="text-xs text-[var(--color-text-tertiary)] hover:text-[var(--color-text-secondary)]"
			>
				{m['sharedComponents.filter_builder.clear_filters']()}
			</Button>
		{/if}
	{/if}
</div>
