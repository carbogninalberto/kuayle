<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { issuesState } from '$lib/features/issues/issues.state.svelte';
	import type { GroupByField } from '$lib/features/issues/issues.state.svelte';
	import IssueRow from '$lib/features/issues/IssueRow.svelte';
	import IssueDetail from '$lib/features/issues/IssueDetail.svelte';
	import IssueGroupHeader from '$lib/features/issues/IssueGroupHeader.svelte';
	import KanbanBoard from '$lib/features/issues/KanbanBoard.svelte';
	import BulkActionBar from '$lib/features/issues/BulkActionBar.svelte';
	import FilterBuilder from '$lib/components/shared/FilterBuilder.svelte';
	import ViewSwitcher from '$lib/components/shared/ViewSwitcher.svelte';
	import EmptyState from '$lib/components/shared/EmptyState.svelte';
	import LoadingState from '$lib/components/shared/LoadingState.svelte';
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
	import { Plus, Bookmark, Layers } from 'lucide-svelte';
	import * as issueApi from '$lib/api/issues';

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
	let filters = $state<ViewFilter>({});
	let layout = $state<ViewLayout>('list');
	let groupByOpen = $state(false);
	let collapsedGroups = $state<Set<string>>(new Set());
	let lastSelectedId = $state<string | null>(null);

	const groupByOptions: { value: GroupByField; label: string }[] = [
		{ value: 'status', label: 'Status' },
		{ value: 'priority', label: 'Priority' },
		{ value: 'assignee', label: 'Assignee' },
		{ value: 'project', label: 'Project' },
		{ value: null, label: 'No grouping' }
	];

	onMount(async () => {
		const [t, p, l, m, c] = await Promise.all([
			listTeams(slug),
			listProjects(slug),
			listLabels(slug),
			listMembers(slug),
			listCycles(slug, teamId)
		]);
		teams = t;
		projects = p;
		labels = l;
		members = m;
		cycles = c;
		loadIssues();
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

	function toggleGroup(key: string) {
		const next = new Set(collapsedGroups);
		if (next.has(key)) {
			next.delete(key);
		} else {
			next.add(key);
		}
		collapsedGroups = next;
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
		} catch {
			toast.error('Failed to delete issues');
		}
		showDeleteConfirm = false;
	}

	const keyHandler = createKeyboardHandler([
		{
			key: 'c',
			handler: () => {
				showCreateIssue = true;
			}
		},
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
			<h1 class="text-sm font-medium text-[var(--color-text-primary)]">Issues</h1>
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
			<button
				onclick={() => (showCreateIssue = true)}
				class="flex items-center gap-1 rounded-md bg-[var(--app-accent)] px-3 py-1.5 text-sm text-white hover:bg-[var(--app-accent-hover)]"
			>
				<Plus size={14} />
				New Issue
			</button>
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
			{#if issuesState.loading}
				<LoadingState />
			{:else if issuesState.issues.length === 0}
				<EmptyState
					title="No issues found"
					description={Object.keys(filters).length > 0 ? 'Try adjusting your filters' : 'Create your first issue to get started'}
					action={Object.keys(filters).length === 0 ? { label: 'New Issue', onclick: () => (showCreateIssue = true) } : undefined}
				/>
			{:else if issuesState.groupBy}
				{#each issuesState.groupedIssues as group (group.key)}
					<IssueGroupHeader
						groupKey={group.key}
						groupBy={issuesState.groupBy}
						count={group.issues.length}
						collapsed={collapsedGroups.has(group.key)}
						ontoggle={() => toggleGroup(group.key)}
					/>
					{#if !collapsedGroups.has(group.key)}
						{#each group.issues as issue (issue.id)}
							<IssueRow {issue} {slug} {members} {labels} onclick={handleIssueClick} {lastSelectedId} />
						{/each}
					{/if}
				{/each}
			{:else}
				{#each issuesState.issues as issue (issue.id)}
					<IssueRow {issue} {slug} {members} {labels} onclick={handleIssueClick} {lastSelectedId} />
				{/each}
			{/if}

			<BulkActionBar {slug} />

			<!-- Shortcuts hint -->
			<div class="py-2 text-center text-xs text-[var(--color-text-tertiary)]">
				Press <kbd class="rounded border border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-1 py-0.5 text-[10px]">?</kbd> for shortcuts
			</div>
		</div>
	{:else}
		{#if issuesState.loading}
			<LoadingState />
		{:else}
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
	{teams}
	{projects}
	{labels}
	{members}
	{cycles}
	defaultTeamId={teamId}
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
