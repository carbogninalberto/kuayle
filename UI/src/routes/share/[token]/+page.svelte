<script lang="ts">
	import { page } from '$app/stores';
	import { onMount } from 'svelte';
	import { getShareMeta, listShareIssues } from '$lib/api/public';
	import type { PublicShareMeta, PublicIssue, PublicStatus } from '$lib/types/shared-link';
	import type { PaginatedResponse } from '$lib/types/common';
	import type { ViewFilter, ViewLayout } from '$lib/types/view';
	import type { IssuePriority } from '$lib/types/issue';
	import { PRIORITY_LABELS } from '$lib/types/issue';
	import PublicIssueRow from '$lib/features/issues/PublicIssueRow.svelte';
	import PublicIssueDetail from '$lib/features/issues/PublicIssueDetail.svelte';
	import PublicKanbanBoard from '$lib/features/issues/PublicKanbanBoard.svelte';
	import ViewSwitcher from '$lib/components/shared/ViewSwitcher.svelte';
	import IssueStatusIcon from '$lib/features/issues/IssueStatusIcon.svelte';
	import IssuePriorityIcon from '$lib/features/issues/IssuePriorityIcon.svelte';
	import { Checkbox } from '$lib/components/ui/checkbox';
	import * as Popover from '$lib/components/ui/popover';
	import { Globe, Search, CircleDashed, Signal, Plus, X, ChevronDown, ChevronRight } from 'lucide-svelte';
	import type { StatusCategory } from '$lib/types/team-status';

	let meta = $state<PublicShareMeta | null>(null);
	let issues = $state<PublicIssue[]>([]);
	let totalCount = $state(0);
	let currentPage = $state(1);
	let hasMore = $state(false);
	let loading = $state(true);
	let error = $state<string | null>(null);
	let selectedIssue = $state<PublicIssue | null>(null);
	let layout = $state<ViewLayout>('list');
	let filters = $state<ViewFilter>({});
	let searchValue = $state('');
	let searchTimeout: ReturnType<typeof setTimeout>;
	let statusOpen = $state(false);
	let priorityOpen = $state(false);
	let collapsedGroups = $state<Set<string>>(new Set());

	interface IssueGroup {
		statusId: string;
		name: string;
		category: string;
		color: string | null;
		position: number;
		issues: PublicIssue[];
	}

	const groupedByStatus = $derived.by((): IssueGroup[] => {
		if (!meta?.statuses || meta.statuses.length === 0) return [];

		const map = new Map<string, IssueGroup>();
		for (const st of meta.statuses) {
			map.set(st.id, {
				statusId: st.id,
				name: st.name,
				category: st.category,
				color: st.color,
				position: st.position,
				issues: []
			});
		}

		for (const issue of issues) {
			const sid = issue.status_info?.id;
			if (sid && map.has(sid)) {
				map.get(sid)!.issues.push(issue);
			} else {
				// Fallback: put in first group
				const first = map.values().next().value;
				if (first) first.issues.push(issue);
			}
		}

		return [...map.values()]
			.filter(g => g.issues.length > 0)
			.sort((a, b) => a.position - b.position);
	});

	const token = $derived($page.params.token ?? '');

	onMount(async () => {
		try {
			meta = await getShareMeta(token);
			await loadIssues();
		} catch {
			error = 'This shared link is not available or has expired.';
		} finally {
			loading = false;
		}
	});

	async function loadIssues() {
		const params: Record<string, string> = {
			page: String(currentPage),
			per_page: '50'
		};
		if (filters.status) params.status = filters.status;
		if (filters.priority) params.priority = filters.priority;
		if (filters.search) params.search = filters.search;
		if (filters.sort) params.sort = filters.sort;
		if (filters.order) params.order = filters.order;
		if (filters.label) params.label = filters.label;

		try {
			const result: PaginatedResponse<PublicIssue> = await listShareIssues(token, params);
			issues = result.data;
			totalCount = result.total_count;
			hasMore = result.has_more;
		} catch {
			error = 'Failed to load issues.';
		}
	}

	function handleFilterChange() {
		currentPage = 1;
		loadIssues();
	}

	function handleSearchInput(e: Event) {
		const value = (e.target as HTMLInputElement).value;
		searchValue = value;
		clearTimeout(searchTimeout);
		searchTimeout = setTimeout(() => {
			if (value.trim()) {
				filters = { ...filters, search: value.trim() };
			} else {
				const { search: _, ...rest } = filters;
				filters = rest;
			}
			handleFilterChange();
		}, 300);
	}

	// Status filter helpers
	function getStatusValues(): string[] {
		return filters.status ? filters.status.split(',') : [];
	}
	function toggleStatus(id: string) {
		const current = getStatusValues();
		const next = current.includes(id) ? current.filter((v) => v !== id) : [...current, id];
		if (next.length > 0) {
			filters = { ...filters, status: next.join(',') };
		} else {
			const { status: _, ...rest } = filters;
			filters = rest;
		}
		handleFilterChange();
	}

	// Priority filter helpers
	function getPriorityValues(): string[] {
		return filters.priority ? filters.priority.split(',') : [];
	}
	function togglePriority(value: string) {
		const current = getPriorityValues();
		const next = current.includes(value) ? current.filter((v) => v !== value) : [...current, value];
		if (next.length > 0) {
			filters = { ...filters, priority: next.join(',') };
		} else {
			const { priority: _, ...rest } = filters;
			filters = rest;
		}
		handleFilterChange();
	}

	function clearFilters() {
		filters = {};
		searchValue = '';
		handleFilterChange();
	}

	function loadMore() {
		currentPage++;
		loadIssues();
	}

	const hasFilters = $derived(
		Object.keys(filters).length > 0
	);
</script>

<svelte:head>
	<title>{meta ? `${meta.scope_name} - ${meta.workspace_name}` : 'Shared View'}</title>
</svelte:head>

{#if loading}
	<div class="flex h-[60vh] items-center justify-center">
		<p class="text-sm text-[var(--color-text-tertiary)]">Loading...</p>
	</div>
{:else if error}
	<div class="flex h-[60vh] flex-col items-center justify-center gap-2">
		<Globe size={32} class="text-[var(--color-text-tertiary)]" />
		<p class="text-sm text-[var(--color-text-tertiary)]">{error}</p>
	</div>
{:else if meta}
	<!-- Header -->
	<div class="border-b border-[var(--app-border)] px-4 py-3">
		<div class="flex items-center gap-3">
			<div class="flex items-center gap-2">
				<Globe size={16} class="text-[var(--color-text-tertiary)]" />
				<span class="text-xs text-[var(--color-text-tertiary)]">{meta.workspace_name}</span>
				<span class="text-xs text-[var(--color-text-tertiary)]">/</span>
				<span class="text-sm font-medium text-[var(--color-text-primary)]">{meta.scope_name}</span>
			</div>
			<span class="rounded-full bg-[var(--color-bg-tertiary)] px-2 py-0.5 text-[10px] font-medium text-[var(--color-text-tertiary)] uppercase tracking-wider">
				Read only
			</span>
			<div class="flex-1"></div>
			<span class="text-xs text-[var(--color-text-tertiary)]">{totalCount} issues</span>
			{#if meta.statuses && meta.statuses.length > 0}
				<ViewSwitcher bind:layout />
			{/if}
		</div>
	</div>

	<!-- Filters -->
	<div class="flex items-center gap-1.5 border-b border-[var(--app-border)] px-4 py-2">
		<!-- Search -->
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

		<!-- Status filter (if statuses available) -->
		{#if meta.statuses && meta.statuses.length > 0}
			<Popover.Root bind:open={statusOpen}>
				<Popover.Trigger>
					<button class="flex items-center gap-1 rounded-md border {filters.status ? 'border-[var(--app-accent)]/30 bg-[var(--app-accent)]/10 text-[var(--app-accent-light)]' : 'border-[var(--app-border)] text-[var(--color-text-tertiary)]'} px-2 py-0.5 text-xs hover:bg-[var(--color-bg-hover)]">
						<CircleDashed size={12} />
						{#if getStatusValues().length === 0}
							Status
						{:else if getStatusValues().length === 1}
							{@const st = meta.statuses?.find(s => s.id === getStatusValues()[0])}
							{st?.name ?? 'Status'}
						{:else}
							{getStatusValues().length} statuses
						{/if}
					</button>
				</Popover.Trigger>
				<Popover.Content class="w-44 p-1" align="start">
					{#each meta.statuses as st}
						<button
							onclick={() => toggleStatus(st.id)}
							class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)]"
						>
							<Checkbox checked={getStatusValues().includes(st.id)} />
							<IssueStatusIcon category={st.category as StatusCategory} color={st.color} />
							{st.name}
						</button>
					{/each}
				</Popover.Content>
			</Popover.Root>
		{/if}

		<!-- Priority filter -->
		<Popover.Root bind:open={priorityOpen}>
			<Popover.Trigger>
				<button class="flex items-center gap-1 rounded-md border {filters.priority ? 'border-[var(--app-accent)]/30 bg-[var(--app-accent)]/10 text-[var(--app-accent-light)]' : 'border-[var(--app-border)] text-[var(--color-text-tertiary)]'} px-2 py-0.5 text-xs hover:bg-[var(--color-bg-hover)]">
					<Signal size={12} />
					{#if getPriorityValues().length === 0}
						Priority
					{:else if getPriorityValues().length === 1}
						{PRIORITY_LABELS[Number(getPriorityValues()[0]) as IssuePriority]}
					{:else}
						{getPriorityValues().length} priorities
					{/if}
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
			</Popover.Content>
		</Popover.Root>

		<div class="flex-1"></div>

		{#if hasFilters}
			<button onclick={clearFilters} class="text-xs text-[var(--color-text-tertiary)] hover:text-[var(--color-text-secondary)]">
				Clear filters
			</button>
		{/if}
	</div>

	<!-- Issues -->
	{#if layout === 'board' && meta.statuses && meta.statuses.length > 0}
		<PublicKanbanBoard {issues} statuses={meta.statuses} />
	{:else}
		<div class="flex-1 overflow-y-auto py-1">
			{#if issues.length === 0}
				<div class="flex h-40 items-center justify-center">
					<p class="text-sm text-[var(--color-text-tertiary)]">No issues found</p>
				</div>
			{:else if groupedByStatus.length > 0}
				{#each groupedByStatus as group (group.statusId)}
					{@const collapsed = collapsedGroups.has(group.statusId)}
					<!-- Group header -->
					<div class="mt-1 first:mt-0 mx-2">
						<button
							class="flex w-full items-center gap-2 rounded-md bg-[var(--color-bg-secondary)] px-4 py-1.5 text-xs font-medium text-[var(--color-text-secondary)]"
							onclick={() => {
								const next = new Set(collapsedGroups);
								if (collapsed) next.delete(group.statusId);
								else next.add(group.statusId);
								collapsedGroups = next;
							}}
						>
							{#if collapsed}
								<ChevronRight size={12} class="text-[var(--color-text-tertiary)]" />
							{:else}
								<ChevronDown size={12} class="text-[var(--color-text-tertiary)]" />
							{/if}
							<IssueStatusIcon category={group.category as StatusCategory} color={group.color} size={14} />
							<span>{group.name}</span>
							<span class="text-[10px] font-normal text-[var(--color-text-tertiary)]">{group.issues.length}</span>
						</button>
					</div>
					{#if !collapsed}
						{#each group.issues as issue (issue.identifier)}
							<PublicIssueRow {issue} onclick={(i) => (selectedIssue = i)} />
						{/each}
					{/if}
				{/each}
			{:else}
				{#each issues as issue (issue.identifier)}
					<PublicIssueRow {issue} onclick={(i) => (selectedIssue = i)} />
				{/each}
			{/if}
			{#if hasMore}
				<div class="flex justify-center py-4">
					<button onclick={loadMore} class="rounded-md border border-[var(--app-border)] px-4 py-1.5 text-xs text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)]">
						Load more
					</button>
				</div>
			{/if}
		</div>
	{/if}
{/if}

{#if selectedIssue}
	<PublicIssueDetail bind:issue={selectedIssue} {issues} onclose={() => (selectedIssue = null)} />
{/if}
