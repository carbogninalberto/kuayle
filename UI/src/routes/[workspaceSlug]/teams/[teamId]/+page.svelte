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
	import KanbanBoard from '$lib/features/issues/KanbanBoard.svelte';
	import BulkActionBar from '$lib/features/issues/BulkActionBar.svelte';
	import FilterBuilder from '$lib/components/shared/FilterBuilder.svelte';
	import ViewSwitcher from '$lib/components/shared/ViewSwitcher.svelte';
	import EmptyState from '$lib/components/shared/EmptyState.svelte';
	import CreateIssueDialog from '$lib/features/issues/CreateIssueDialog.svelte';
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
	import { toast } from 'svelte-sonner';
	import { Bookmark, Layers, SquareUser, SquaresSubtract, ChevronRight, Share2 } from 'lucide-svelte';
	import ShareLinkDialog from '$lib/components/shared/ShareLinkDialog.svelte';
	import { sidebarState } from '$lib/features/layout/sidebar.state.svelte';
	import SidebarToggle from '$lib/components/layout/SidebarToggle.svelte';
	import * as issueApi from '$lib/api/issues';
	import type { Issue } from '$lib/types/issue';
	import type { IssueStatus, IssuePriority, RelationType } from '$lib/types/issue';
	import AddRelationDialog from '$lib/features/issues/AddRelationDialog.svelte';

	const slug = $derived(page.params.workspaceSlug ?? '');
	const teamId = $derived(page.params.teamId ?? '');
	let showDeleteConfirm = $state(false);

	let teams = $state<Team[]>([]);
	let projects = $state<Project[]>([]);
	let labels = $state<Label[]>([]);
	let members = $state<WorkspaceMember[]>([]);
	let cycles = $state<Cycle[]>([]);
	let showCreateIssue = $state(false);
	let showSaveView = $state(false);
	let showShareLink = $state(false);
	let quickAddDefaults = $state<{ statusId?: string; priority?: IssuePriority; assigneeId?: string }>({});
	let filters = $state<ViewFilter>({});
	let layout = $state<ViewLayout>('list');
	let groupByOpen = $state(false);
	let collapsedGroups = $state<Set<string>>(new Set());
	let lastSelectedId = $state<string | null>(null);
	let dragOverGroup = $state<string | null>(null);
	let dragSourceGroup = $state<string | null>(null);
	let dragOverIssueId = $state<string | null>(null);
	let dropPosition = $state<'above' | 'below'>('below');
	let relationDialogOpen = $state(false);
	let relationIssue = $state<Issue | null>(null);
	let relationDefaultType = $state<RelationType>('related');

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
		const s = slug;
		const t = teamId;
		if (!s || !t) return;
		teamStatusesState.reload(s, t);
		Promise.all([
			listTeams(s),
			listProjects(s),
			listLabels(s),
			listMembers(s),
			listCycles(s, t)
		]).then(([te, p, l, m, c]) => {
			teams = te;
			projects = p;
			labels = l;
			members = m;
			cycles = c;
			loadIssues();
		});
	});

	function loadIssues() {
		const params: Record<string, string> = { team: teamId };
		for (const [key, value] of Object.entries(filters)) {
			if (value !== undefined && value !== '') {
				params[key] = value;
			}
		}
		if (layout === 'board') {
			params.per_page = '200';
		}
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

	function handleIssueClick(issue: any) {
		lastSelectedId = issue.id;
		goto(`/${slug}/issue/${issue.identifier}`);
	}

	async function handleGroupDrop(issueIdentifier: string, groupKey: string) {
		const gb = issuesState.groupBy;
		if (!gb) return;
		const fieldMap: Record<string, string> = {
			status: 'status_id',
			priority: 'priority',
			assignee: 'assignee_id',
			project: 'project_id'
		};
		const field = fieldMap[gb];
		if (!field) return;
		let value: any = groupKey;
		if (gb === 'assignee' && groupKey === 'unassigned') value = null;
		if (gb === 'project' && groupKey === 'no-project') value = null;
		if (gb === 'priority') value = Number(groupKey);
		try {
			await issuesState.update(slug, issueIdentifier, { [field]: value });
		} catch {
			toast.error('Failed to move issue');
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
	}

	function handleQuickAdd(groupKey: string) {
		quickAddDefaults = {};
		const gb = issuesState.groupBy;
		if (gb === 'status') {
			quickAddDefaults = { statusId: groupKey };
		} else if (gb === 'priority') {
			quickAddDefaults = { priority: Number(groupKey) as IssuePriority };
		} else if (gb === 'assignee' && groupKey !== 'unassigned') {
			quickAddDefaults = { assigneeId: groupKey };
		}
		showCreateIssue = true;
	}

	async function deleteSelectedIssues() {
		const ids = Array.from(issuesState.selectedIds);
		if (ids.length === 0) return;
		try {
			await issueApi.bulkDeleteIssues(slug, { issue_ids: ids });
			issuesState.issues = issuesState.issues.filter(i => !issuesState.selectedIds.has(i.id));
			issuesState.totalCount -= ids.length;
			issuesState.clearSelection();
			toast.success(`Deleted ${ids.length} issue${ids.length > 1 ? 's' : ''}`);
		} catch (err: any) {
			toast.error(err?.error?.message || 'Failed to delete issues');
		}
		showDeleteConfirm = false;
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
					showDeleteConfirm = true;
				}
			}
		},
		{
			key: 'Delete',
			handler: () => {
				if (issuesState.selectionCount > 0) {
					showDeleteConfirm = true;
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
	<div
		class="flex h-[49px] items-center justify-between border-b border-[var(--app-border)] px-6"
	>
		<div class="flex items-center gap-3">
			<SidebarToggle />
			<nav class="flex items-center gap-1.5 text-sm">
				{#if sidebarState.getTeam(teamId)}
					<a href="/{slug}/teams/{teamId}" class="flex items-center gap-1.5 text-[var(--color-text-tertiary)] hover:text-[var(--color-text-secondary)]">
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
		<div class="flex items-center gap-2">
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
								onclick={() => { issuesState.groupBy = opt.value; groupByOpen = false; }}
								class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)] {issuesState.groupBy === opt.value ? 'bg-[var(--color-bg-hover)]' : ''}"
							>
								{opt.label}
							</button>
						{/each}
					</Popover.Content>
				</Popover.Root>
			{/if}

			<ViewSwitcher bind:layout onchange={handleLayoutChange} />
			{#if Object.keys(filters).length > 0}
				<button
					onclick={() => (showSaveView = true)}
					class="flex items-center gap-1 rounded-md border border-[var(--app-border)] px-2 py-1 text-xs text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-secondary)]"
					title="Save as view"
				>
					<Bookmark size={12} />
					Save view
				</button>
			{/if}
		</div>
	</div>

	<!-- Filter bar -->
	<FilterBuilder
		bind:filters
		{teams}
		{projects}
		{labels}
		{members}
		onchange={handleFilterChange}
	/>

	<!-- Content -->
	{#if layout === 'list'}
		<div class="relative flex-1 overflow-y-auto">
			{#if !issuesState.loading && issuesState.issues.length === 0}
				<EmptyState
					title="No issues found"
					description={Object.keys(filters).length > 0 ? 'Try adjusting your filters' : 'Create your first issue to get started'}
					action={Object.keys(filters).length === 0 ? { label: 'New Issue', onclick: () => (showCreateIssue = true) } : undefined}
				/>
			{:else if issuesState.groupBy}
				{#each issuesState.groupedIssues as group (group.key)}
					{@const dropKey = group.key}
					<!-- svelte-ignore a11y_no_static_element_interactions -->
					<div
						class="group/drop transition-all {dragOverGroup === dropKey && dragSourceGroup !== dropKey ? 'border border-[var(--app-accent)]' : ''}"
						ondragstart={() => { dragSourceGroup = dropKey; }}
						ondragend={() => { dragSourceGroup = null; dragOverGroup = null; dragOverIssueId = null; }}
						ondragover={(e) => { e.preventDefault(); if (e.dataTransfer) e.dataTransfer.dropEffect = 'move'; dragOverGroup = dropKey; }}
						ondragleave={(e) => { if (!e.currentTarget.contains(e.relatedTarget as Node)) { dragOverGroup = null; dragOverIssueId = null; } }}
						ondrop={(e) => { e.preventDefault(); dragOverGroup = null; dragOverIssueId = null; const id = e.dataTransfer?.getData('text/plain'); if (id) handleGroupDrop(id, dropKey); }}
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
										dragOverIssueId = issue.id;
										const rect = (e.currentTarget as HTMLElement).getBoundingClientRect();
										const midY = rect.top + rect.height / 2;
										dropPosition = e.clientY < midY ? 'above' : 'below';
									}}
									ondragleave={() => { dragOverIssueId = null; }}
								>
									{#if dragOverIssueId === issue.id && dragSourceGroup === dropKey}
										<div class="absolute {dropPosition === 'above' ? 'top-0' : 'bottom-0'} left-0 right-0 h-px bg-[var(--app-accent)] z-10"></div>
									{/if}
									<IssueRow {issue} {slug} {members} {labels} {projects} {cycles} onclick={handleIssueClick} {lastSelectedId} onlastselected={(id) => lastSelectedId = id} onaddrelation={handleAddRelation} />
								</div>
							{/each}
						{/if}
					</div>
				{/each}
			{:else}
				{#each issuesState.issues as issue (issue.id)}
					<IssueRow {issue} {slug} {members} {labels} {projects} {cycles} onclick={handleIssueClick} {lastSelectedId} onlastselected={(id) => lastSelectedId = id} onaddrelation={handleAddRelation} />
				{/each}
			{/if}

			<BulkActionBar {slug} />

			<!-- Shortcuts hint -->
			<div class="py-2 text-center text-xs text-[var(--color-text-tertiary)]">
				Press <kbd class="rounded border border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-1 py-0.5 text-[10px]">?</kbd> for shortcuts
			</div>
		</div>
	{:else}
		{#if !issuesState.loading}
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
	{/if}
</div>

{#if issuesState.selectedIssue}
	<IssueDetail
		issue={issuesState.selectedIssue}
		{slug}
		onclose={() => issuesState.select(null)}
	/>
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
	defaultStatusId={quickAddDefaults.statusId}
	defaultPriority={quickAddDefaults.priority}
	defaultAssigneeId={quickAddDefaults.assigneeId}
	onsubmit={async (req) => {
		try {
			await issuesState.create(slug, req);
			toast.success('Issue created');
		} catch (err: any) {
			toast.error(err?.error?.message || 'Failed to create issue');
		}
	}}
/>

<SaveViewDialog
	bind:open={showSaveView}
	{filters}
	{slug}
/>

<ShareLinkDialog
	bind:open={showShareLink}
	{slug}
	scope="team"
	scopeId={teamId}
	{filters}
/>

<AddRelationDialog
	bind:open={relationDialogOpen}
	{slug}
	identifier={relationIssue?.identifier ?? ''}
	defaultType={relationDefaultType}
/>

{#if showDeleteConfirm}
	<!-- Delete confirmation overlay -->
	<div class="fixed inset-0 z-50 flex items-center justify-center bg-black/50">
		<div class="w-96 rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-primary)] p-6 shadow-xl">
			<h3 class="text-sm font-medium text-[var(--color-text-primary)]">Delete {issuesState.selectionCount} issue{issuesState.selectionCount > 1 ? 's' : ''}?</h3>
			<p class="mt-2 text-sm text-[var(--color-text-tertiary)]">This action cannot be undone.</p>
			<div class="mt-4 flex justify-end gap-2">
				<button
					onclick={() => (showDeleteConfirm = false)}
					class="rounded-md border border-[var(--app-border)] px-3 py-1.5 text-sm text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)]"
				>
					Cancel
				</button>
				<button
					onclick={deleteSelectedIssues}
					class="rounded-md bg-red-600 px-3 py-1.5 text-sm text-white hover:bg-red-700"
				>
					Delete
				</button>
			</div>
		</div>
	</div>
{/if}
