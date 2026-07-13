<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { issuesState } from '$lib/features/issues/issues.state.svelte';
	import { teamStatusesState } from '$lib/features/issues/team-statuses.state.svelte';
	import type { GroupByField } from '$lib/features/issues/issues.state.svelte';
	import IssueRow from '$lib/features/issues/IssueRow.svelte';
	import IssueDetail from '$lib/features/issues/IssueDetail.svelte';
	import IssueGroupHeader from '$lib/features/issues/IssueGroupHeader.svelte';
	import IssueListLoadMore from '$lib/features/issues/IssueListLoadMore.svelte';
	import KanbanBoard from '$lib/features/issues/KanbanBoard.svelte';
	import BulkActionBar from '$lib/features/issues/BulkActionBar.svelte';
	import FilterBuilder from '$lib/components/shared/FilterBuilder.svelte';
	import ViewSwitcher from '$lib/components/shared/ViewSwitcher.svelte';
	import EmptyState from '$lib/components/shared/EmptyState.svelte';
	import CreateIssueDialog from '$lib/features/issues/CreateIssueDialog.svelte';
	import { showIssueCreatedToast } from '$lib/features/issues/issue-created-toast';
	import SaveViewDialog from '$lib/components/shared/SaveViewDialog.svelte';
	import * as Popover from '$lib/components/ui/popover';
	import { listTeams } from '$lib/api/teams';
	import { listProjects } from '$lib/api/projects';
	import { listLabels } from '$lib/api/labels';
	import { listMembers } from '$lib/api/members';
	import { listCycles } from '$lib/api/cycles';
	import type { Team } from '$lib/types/team';
	import type { Project } from '$lib/types/project';
	import type { Label } from '$lib/types/label';
	import type { Cycle } from '$lib/types/cycle';
	import type { WorkspaceMember } from '$lib/types/workspace';
	import type { ViewFilter, ViewLayout } from '$lib/types/view';
	import { createKeyboardHandler } from '$lib/utils/keyboard';
	import { appToast } from '$lib/features/toast/toast';
	import { Layers, SquareUser, SquaresSubtract, ChevronRight, Share2 } from 'lucide-svelte';
	import ShareLinkDialog from '$lib/components/shared/ShareLinkDialog.svelte';
	import { sidebarState } from '$lib/features/layout/sidebar.state.svelte';
	import SidebarToggle from '$lib/components/layout/SidebarToggle.svelte';
	import { IsMobile } from '$lib/hooks/is-mobile.svelte';
	import type { Issue } from '$lib/types/issue';
	import type { IssueStatus, IssuePriority, RelationType } from '$lib/types/issue';
	import AddRelationDialog from '$lib/features/issues/AddRelationDialog.svelte';
	import { preferencesState } from '$lib/features/preferences/preferences.state.svelte';
	import { loadCollapsedGroups, saveCollapsedGroups } from '$lib/features/issues/collapsed-groups';

	const slug = $derived(page.params.workspaceSlug ?? '');
	const teamId = $derived(page.params.teamId ?? '');

	let teams = $state<Team[]>([]);
	let projects = $state<Project[]>([]);
	let labels = $state<Label[]>([]);
	let members = $state<WorkspaceMember[]>([]);
	let cycles = $state<Cycle[]>([]);
	let showCreateIssue = $state(false);
	let showShareLink = $state(false);
	let quickAddDefaults = $state<{ statusId?: string; priority?: IssuePriority; assigneeIds?: string[] }>({});
	let filters = $state<ViewFilter>({});
	let hasActiveFilters = $derived(Object.values(filters).some((value) => value !== undefined && value !== ''));
	let layout = $state<ViewLayout>('list');
	let groupByOpen = $state(false);
	let collapsedGroups = $state<Set<string>>(new Set());
	let loadedCollapsedScope = '';
	let initializationId = 0;
	let lastSelectedId = $state<string | null>(null);
	let dragOverGroup = $state<string | null>(null);
	let dragSourceGroup = $state<string | null>(null);
	let dragOverIssueId = $state<string | null>(null);
	let dropPosition = $state<'above' | 'below'>('below');
	let relationDialogOpen = $state(false);
	let relationIssue = $state<Issue | null>(null);
	let relationDefaultType = $state<RelationType>('related');
	const isMobile = new IsMobile();

	function handleAddRelation(issue: Issue, type: RelationType) {
		relationIssue = issue;
		relationDefaultType = type;
		relationDialogOpen = true;
	}

	const groupByOptions: { value: GroupByField; label: string }[] = [
		{ value: 'status', label: 'Status' },
		{ value: 'priority', label: 'Priority' },
		{ value: 'assignee', label: 'Assignee' },
		{ value: 'project', label: 'Project' },
		{ value: null, label: 'No grouping' }
	];

	$effect(() => {
		if (isMobile.current && layout === 'board') {
			layout = 'list';
		}
	});

	$effect(() => {
		const s = slug;
		const t = teamId;
		if (!s || !t) return;
		const currentInitialization = ++initializationId;
		void preferencesState
			.syncRemote()
			.then(() => {
				if (currentInitialization !== initializationId || s !== slug || t !== teamId) return;
				issuesState.groupBy = preferencesState.issuesGroupBy;
				void teamStatusesState.reload(s, t);
				return Promise.all([
					issuesState.load(s, getIssueParams()),
					Promise.all([listTeams(s), listProjects(s), listLabels(s), listMembers(s), listCycles(s, t)]).then(
						([te, p, l, m, c]) => {
							if (currentInitialization !== initializationId || s !== slug || t !== teamId) return;
							teams = te;
							projects = p;
							labels = l;
							members = m;
							cycles = c;
						}
					)
				]);
			})
			.catch(() => {
				if (currentInitialization === initializationId && s === slug && t === teamId) {
					appToast.error('Failed to load issues');
				}
			});
	});

	$effect(() => {
		const groupBy = issuesState.groupBy;
		const scope = groupBy ? `${slug}/${teamId}/${groupBy}` : '';
		if (scope === loadedCollapsedScope) return;
		loadedCollapsedScope = scope;
		collapsedGroups = loadCollapsedGroups(slug, teamId, groupBy);
	});

	function getIssueParams() {
		const params: Record<string, string> = { team: teamId };
		for (const [key, value] of Object.entries(filters)) {
			if (value !== undefined && value !== '') {
				params[key] = value;
			}
		}
		if (layout === 'list' && issuesState.groupBy) {
			params.group_by = issuesState.groupBy;
		} else {
			delete params.group_by;
		}
		params.sort ??= 'sort_order';
		params.order ??= 'asc';
		if (layout === 'board') {
			params.per_page = '200';
		}
		return params;
	}

	function loadIssues() {
		const params = getIssueParams();
		issuesState.load(slug, params);
	}

	function handleFilterChange(f: ViewFilter) {
		filters = f;
		loadIssues();
	}

	function handleLayoutChange(l: ViewLayout) {
		layout = l;
		loadIssues();
	}

	function handleGroupByChange(value: GroupByField) {
		issuesState.groupBy = value;
		preferencesState.setIssuesGroupBy(value);
		groupByOpen = false;
		loadIssues();
	}

	function handleIssueClick(issue: any) {
		lastSelectedId = issue.id;
		goto(`/${slug}/issue/${issue.identifier}`);
	}

	function getIssueGroupKey(issue: Issue, groupBy: GroupByField) {
		switch (groupBy) {
			case 'status':
				return issue.status_id ?? issue.status;
			case 'priority':
				return String(issue.priority);
			case 'assignee':
				return issue.assignee_id ?? 'unassigned';
			case 'project':
				return issue.project_id ?? 'no-project';
			default:
				return 'all';
		}
	}

	function calculateSortOrder(items: Issue[], index: number): number {
		const prev = index > 0 ? items[index - 1].sort_order : undefined;
		const next = index < items.length - 1 ? items[index + 1].sort_order : undefined;
		if (prev !== undefined && next !== undefined) return (prev + next) / 2;
		if (prev !== undefined) return prev + 1000;
		if (next !== undefined) return next - 1000;
		return 0;
	}

	function hasSameIssueOrder(a: Issue[], b: Issue[]) {
		return a.length === b.length && a.every((issue, index) => issue.id === b[index]?.id);
	}

	function applyLocalGroupOrder(orderedIssues: Issue[]) {
		const orderedIds = orderedIssues.map((issue) => issue.id);
		const groupIds = new Set(orderedIds);
		const currentIssues = new Map(issuesState.issues.map((issue) => [issue.id, issue]));
		let groupIndex = 0;
		issuesState.issues = issuesState.issues.map((issue) => {
			if (!groupIds.has(issue.id)) return issue;
			return currentIssues.get(orderedIds[groupIndex++]) ?? issue;
		});
	}

	async function handleGroupDrop(
		issueIdentifier: string,
		groupKey: string,
		targetIssueId?: string,
		targetPosition: 'above' | 'below' = dropPosition
	) {
		const gb = issuesState.groupBy;
		if (!gb) return;

		const sourceIssue = issuesState.issues.find((issue) => issue.identifier === issueIdentifier);
		if (!sourceIssue) return;

		const sourceGroupKey = getIssueGroupKey(sourceIssue, gb);
		const isSameGroup = sourceGroupKey === groupKey;
		let sortOrder: number | undefined;
		let reorderedGroup: Issue[] | null = null;

		if (targetIssueId) {
			const group = issuesState.groupedIssues.find((g) => g.key === groupKey);
			const itemsWithoutSource = group?.issues.filter((issue) => issue.id !== sourceIssue.id) ?? [];
			const targetIndex = itemsWithoutSource.findIndex((issue) => issue.id === targetIssueId);

			if (targetIndex !== -1) {
				const insertIndex = targetPosition === 'below' ? targetIndex + 1 : targetIndex;
				reorderedGroup = [...itemsWithoutSource];
				reorderedGroup.splice(insertIndex, 0, sourceIssue);

				if (!isSameGroup || !hasSameIssueOrder(group?.issues ?? [], reorderedGroup)) {
					sortOrder = calculateSortOrder(reorderedGroup, insertIndex);
				}
			}
		}

		if (isSameGroup && sortOrder === undefined) return;

		const fieldMap: Record<string, string> = {
			status: 'status_id',
			priority: 'priority',
			assignee: 'assignee_id',
			project: 'project_id'
		};
		const field = fieldMap[gb];
		if (!field) return;

		const req: Record<string, any> = {};
		if (!isSameGroup) {
			let value: any = groupKey;
			if (gb === 'assignee' && groupKey === 'unassigned') value = null;
			if (gb === 'project' && groupKey === 'no-project') value = null;
			if (gb === 'priority') value = Number(groupKey);
			req[field] = value;
		}
		if (sortOrder !== undefined) {
			req.sort_order = sortOrder;
		}

		try {
			const updatedIssue = await issuesState.update(slug, issueIdentifier, req);
			if (isSameGroup && reorderedGroup) {
				applyLocalGroupOrder(reorderedGroup.map((issue) => (issue.id === updatedIssue.id ? updatedIssue : issue)));
			}
		} catch {
			appToast.error('Failed to move issue');
		}
	}

	function toggleGroup(key: string) {
		const next = new Set(collapsedGroups);
		if (next.has(key)) {
			next.delete(key);
		} else {
			next.add(key);
		}
		collapsedGroups = next;
		saveCollapsedGroups(slug, teamId, issuesState.groupBy, next);
	}

	function handleQuickAdd(groupKey: string) {
		quickAddDefaults = {};
		const gb = issuesState.groupBy;
		if (gb === 'status') {
			quickAddDefaults = { statusId: groupKey };
		} else if (gb === 'priority') {
			quickAddDefaults = { priority: Number(groupKey) as IssuePriority };
		} else if (gb === 'assignee') {
			quickAddDefaults = { assigneeIds: groupKey === 'unassigned' ? [] : [groupKey] };
		}
		showCreateIssue = true;
	}

	function openCreateIssue() {
		quickAddDefaults = {};
		showCreateIssue = true;
	}

	function singleFilterValue(value?: string): string | undefined {
		if (!value) return undefined;
		const values = value.split(',').filter(Boolean);
		return values.length === 1 ? values[0] : undefined;
	}

	function getFilterStatusId(): string | undefined {
		const value = singleFilterValue(filters.status);
		if (!value) return undefined;
		return (
			teamStatusesState.statusById.get(value)?.id ??
			teamStatusesState.statusOrder.find((status) => status.slug === value)?.id
		);
	}

	function getFilterPriority(): IssuePriority | undefined {
		const value = singleFilterValue(filters.priority);
		const priority = value === undefined ? NaN : Number(value);
		return [0, 1, 2, 3, 4].includes(priority) ? (priority as IssuePriority) : undefined;
	}

	function getFilterProjectId(): string | null | undefined {
		const value = singleFilterValue(filters.project);
		if (value === 'none') return null;
		return value;
	}

	function getFilterAssigneeIds(): string[] | undefined {
		const value = singleFilterValue(filters.assignee);
		if (value === 'none') return [];
		return value ? [value] : undefined;
	}

	function getFilterLabelIds(): string[] | undefined {
		const value = singleFilterValue(filters.label);
		return value ? [value] : undefined;
	}

	function deleteSelectedIssues() {
		window.dispatchEvent(new CustomEvent('issues:bulk-delete-request'));
	}

	const keyHandler = createKeyboardHandler([
		{
			key: 'x',
			handler: () => {
				// Toggle selection on the last clicked / focused issue
				if (lastSelectedId) {
					issuesState.toggleSelect(lastSelectedId);
				}
			}
		},
		{
			key: 'a',
			ctrl: true,
			handler: () => {
				issuesState.selectAll();
			}
		},
		{
			key: 'Escape',
			handler: () => {
				issuesState.clearSelection();
			}
		},
		{
			key: 'Backspace',
			handler: () => {
				if (issuesState.selectionCount > 0) {
					deleteSelectedIssues();
				}
			}
		},
		{
			key: 'Delete',
			handler: () => {
				if (issuesState.selectionCount > 0) {
					deleteSelectedIssues();
				}
			}
		}
	]);

	onMount(() => {
		document.addEventListener('keydown', keyHandler);
		return () => document.removeEventListener('keydown', keyHandler);
	});
</script>

<div class="flex h-full flex-col">
	<!-- Header -->
	<div class="flex min-h-[49px] items-center justify-between gap-2 border-b border-[var(--app-border)] px-3 sm:px-4">
		<div class="flex min-w-0 items-center gap-3">
			<SidebarToggle />
			<nav class="flex min-w-0 items-center gap-1.5 text-sm">
				{#if sidebarState.getTeam(teamId)}
					<a
						href="/{slug}/teams/{teamId}"
						class="flex items-center gap-1.5 text-[var(--color-text-tertiary)] hover:text-[var(--color-text-secondary)]"
					>
						<SquareUser size={14} class="shrink-0" style="color: {sidebarState.getTeamColor(teamId)}" />
						{sidebarState.getTeam(teamId)?.name}
					</a>
					<ChevronRight size={12} class="shrink-0 text-[var(--color-text-tertiary)]" />
				{/if}
				<span class="flex items-center gap-1.5 font-medium text-[var(--color-text-primary)]">
					<SquaresSubtract size={14} class="shrink-0" />
					Issues
				</span>
			</nav>
			<button
				onclick={() => (showShareLink = true)}
				class="rounded-md p-1 text-[var(--color-text-tertiary)] hover:text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)] transition-colors"
				title="Share public link"
			>
				<Share2 size={14} />
			</button>
		</div>
		<div class="flex shrink-0 items-center gap-2">
			<!-- Group by -->
			{#if layout === 'list'}
				<Popover.Root bind:open={groupByOpen}>
					<Popover.Trigger>
						<button
							class="flex items-center gap-1 rounded-md border border-[var(--app-border)] px-2 py-1 text-xs text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-secondary)]"
							title="Group by"
						>
							<Layers size={12} />
							Group
						</button>
					</Popover.Trigger>
					<Popover.Content class="w-40 p-1" align="end">
						{#each groupByOptions as opt}
							<button
								onclick={() => handleGroupByChange(opt.value)}
								class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)] {issuesState.groupBy ===
								opt.value
									? 'bg-[var(--color-bg-hover)]'
									: ''}"
							>
								{opt.label}
							</button>
						{/each}
					</Popover.Content>
				</Popover.Root>
			{/if}

			<div class="hidden sm:block">
				<ViewSwitcher bind:layout onchange={handleLayoutChange} />
			</div>
			{#if hasActiveFilters}
				<SaveViewDialog {filters} {slug} {teams} defaultTeamId={teamId} defaultScope="team" />
			{/if}
		</div>
	</div>

	<!-- Filter bar -->
	<FilterBuilder bind:filters {teams} {projects} {labels} {members} onchange={handleFilterChange} />

	<!-- Content -->
	{#if layout === 'list'}
		<div class="relative flex-1 overflow-y-auto">
			{#if !issuesState.loading && issuesState.issues.length === 0}
				<EmptyState
					title="No issues found"
					description={hasActiveFilters
						? 'Try adjusting your filters'
						: 'Create your first issue to get started'}
					action={{ label: 'New Issue', onclick: openCreateIssue }}
				/>
			{:else if issuesState.groupBy}
				{#each issuesState.groupedIssues as group (group.key)}
					{@const dropKey = group.key}
					<!-- svelte-ignore a11y_no_static_element_interactions -->
					<div
						class="group/drop transition-all {dragOverGroup === dropKey && dragSourceGroup !== dropKey
							? 'border border-[var(--app-accent)]'
							: ''}"
						ondragstart={() => {
							dragSourceGroup = dropKey;
						}}
						ondragend={() => {
							dragSourceGroup = null;
							dragOverGroup = null;
							dragOverIssueId = null;
						}}
						ondragover={(e) => {
							e.preventDefault();
							if (e.dataTransfer) e.dataTransfer.dropEffect = 'move';
							dragOverGroup = dropKey;
						}}
						ondragleave={(e) => {
							if (!e.currentTarget.contains(e.relatedTarget as Node)) {
								dragOverGroup = null;
								dragOverIssueId = null;
							}
						}}
						ondrop={(e) => {
							e.preventDefault();
							dragOverGroup = null;
							dragOverIssueId = null;
							const id = e.dataTransfer?.getData('text/plain');
							if (id) handleGroupDrop(id, dropKey);
						}}
					>
						<IssueGroupHeader
							groupKey={group.key}
							groupBy={issuesState.groupBy}
							groupLabel={group.label}
							count={group.issues.length}
							collapsed={collapsedGroups.has(group.key)}
							{members}
							{projects}
							ontoggle={() => toggleGroup(group.key)}
							onquickadd={handleQuickAdd}
						/>
						{#if !collapsedGroups.has(group.key)}
							{#each group.issues as issue (issue.id)}
								<!-- svelte-ignore a11y_no_static_element_interactions -->
								<div
									class="relative"
									ondragover={(e) => {
										e.preventDefault();
										dragOverIssueId = issue.id;
										const rect = (e.currentTarget as HTMLElement).getBoundingClientRect();
										const midY = rect.top + rect.height / 2;
										dropPosition = e.clientY < midY ? 'above' : 'below';
									}}
									ondragleave={() => {
										dragOverIssueId = null;
									}}
									ondrop={(e) => {
										e.preventDefault();
										e.stopPropagation();
										dragOverGroup = null;
										dragOverIssueId = null;
										const id = e.dataTransfer?.getData('text/plain');
										if (id) handleGroupDrop(id, dropKey, issue.id, dropPosition);
									}}
								>
									{#if dragOverIssueId === issue.id && dragSourceGroup === dropKey}
										<div
											class="absolute {dropPosition === 'above'
												? 'top-0'
												: 'bottom-0'} left-0 right-0 h-px bg-[var(--app-accent)] z-10"
										></div>
									{/if}
									<IssueRow
										{issue}
										{slug}
										{members}
										{labels}
										{projects}
										{cycles}
										onclick={handleIssueClick}
										{lastSelectedId}
										onlastselected={(id) => (lastSelectedId = id)}
										onaddrelation={handleAddRelation}
									/>
								</div>
							{/each}
						{/if}
					</div>
				{/each}
			{:else}
				{#each issuesState.issues as issue (issue.id)}
					<IssueRow
						{issue}
						{slug}
						{members}
						{labels}
						{projects}
						{cycles}
						onclick={handleIssueClick}
						{lastSelectedId}
						onlastselected={(id) => (lastSelectedId = id)}
						onaddrelation={handleAddRelation}
					/>
				{/each}
			{/if}

			<IssueListLoadMore />

			<BulkActionBar
				{slug}
				{labels}
				{members}
				{cycles}
				onlabelcreated={(label) => (labels = [label, ...labels.filter((existing) => existing.id !== label.id)])}
			/>
		</div>
	{:else if !issuesState.loading}
		<div class="flex-1 overflow-hidden">
			<KanbanBoard
				issuesByStatus={issuesState.issuesByStatus}
				{slug}
				{members}
				{labels}
				onissueclick={handleIssueClick}
			/>
		</div>
	{/if}
</div>

{#if issuesState.selectedIssue}
	<IssueDetail issue={issuesState.selectedIssue} {slug} onclose={() => issuesState.select(null)} />
{/if}

<CreateIssueDialog
	bind:open={showCreateIssue}
	{slug}
	{teams}
	{projects}
	{labels}
	{members}
	{cycles}
	defaultTeamId={teamId}
	defaultStatusId={quickAddDefaults.statusId ?? getFilterStatusId()}
	defaultPriority={quickAddDefaults.priority ?? getFilterPriority()}
	defaultProjectId={getFilterProjectId()}
	defaultAssigneeIds={quickAddDefaults.assigneeIds ?? getFilterAssigneeIds()}
	defaultLabelIds={getFilterLabelIds()}
	onlabelcreated={(label) => (labels = [label, ...labels.filter((existing) => existing.id !== label.id)])}
	onsubmit={async (req) => {
		try {
			const created = await issuesState.create(slug, req);
			showIssueCreatedToast(slug, created);
		} catch (err: any) {
			appToast.apiError(err, 'Failed to create issue');
		}
	}}
/>

<ShareLinkDialog bind:open={showShareLink} {slug} scope="team" scopeId={teamId} {filters} />

<AddRelationDialog
	bind:open={relationDialogOpen}
	{slug}
	identifier={relationIssue?.identifier ?? ''}
	defaultType={relationDefaultType}
	oncreated={loadIssues}
/>
