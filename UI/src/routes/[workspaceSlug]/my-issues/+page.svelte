<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { authState } from '$lib/features/auth/auth.state.svelte';
	import { issuesState } from '$lib/features/issues/issues.state.svelte';
	import IssueRow from '$lib/features/issues/IssueRow.svelte';
	import KanbanBoard from '$lib/features/issues/KanbanBoard.svelte';
	import FilterBuilder from '$lib/components/shared/FilterBuilder.svelte';
	import ViewSwitcher from '$lib/components/shared/ViewSwitcher.svelte';
	import EmptyState from '$lib/components/shared/EmptyState.svelte';
	import IssueGroupHeader from '$lib/features/issues/IssueGroupHeader.svelte';
	import * as Tabs from '$lib/components/ui/tabs';
	import { listProjects } from '$lib/api/projects';
	import { listLabels } from '$lib/api/labels';
	import { listMembers } from '$lib/api/members';
	import type { Project } from '$lib/types/project';
	import type { Label } from '$lib/types/label';
	import type { WorkspaceMember } from '$lib/types/workspace';
	import type { ViewFilter, ViewLayout } from '$lib/types/view';
	import type { Issue, RelationType } from '$lib/types/issue';
	import AddRelationDialog from '$lib/features/issues/AddRelationDialog.svelte';
	import { CircleUser, PenLine } from 'lucide-svelte';
	import { createKeyboardHandler } from '$lib/utils/keyboard';
	import BulkActionBar from '$lib/features/issues/BulkActionBar.svelte';

	const slug = $derived(page.params.workspaceSlug ?? '');

	type MyTab = 'assigned' | 'created';

	let activeTab = $state<MyTab>('assigned');
	let filters = $state<ViewFilter>({});
	let layout = $state<ViewLayout>('list');
	let projects = $state<Project[]>([]);
	let labels = $state<Label[]>([]);
	let members = $state<WorkspaceMember[]>([]);
	let collapsedGroups = $state<Set<string>>(new Set());
	let lastSelectedId = $state<string | null>(null);
	let relationDialogOpen = $state(false);
	let relationIssue = $state<Issue | null>(null);
	let relationDefaultType = $state<RelationType>('related');

	function handleAddRelation(issue: Issue, type: RelationType) {
		relationIssue = issue;
		relationDefaultType = type;
		relationDialogOpen = true;
	}

	onMount(async () => {
		const [p, l, m] = await Promise.all([
			listProjects(slug),
			listLabels(slug),
			listMembers(slug)
		]);
		projects = p;
		labels = l;
		members = m;
		loadIssues();
	});

	function loadIssues() {
		if (!authState.user) return;
		const params: Record<string, string> = {};

		// Tab-specific filter
		if (activeTab === 'assigned') {
			params.assignee = authState.user.id;
		} else {
			params.creator = authState.user.id;
		}

		// Apply user filters
		for (const [key, value] of Object.entries(filters)) {
			if (value !== undefined && value !== '') {
				params[key] = value;
			}
		}

		// Sort by priority (urgent first) then created_at
		params.sort = 'priority';
		params.order = 'asc';

		if (layout === 'board') {
			params.per_page = '200';
		}

		issuesState.groupBy = 'status';
		issuesState.load(slug, params);
	}

	function handleTabChange(tab: string) {
		activeTab = tab as MyTab;
		loadIssues();
	}

	function handleFilterChange(f: ViewFilter) {
		filters = f;
		loadIssues();
	}

	function handleLayoutChange(l: ViewLayout) {
		layout = l;
		loadIssues();
	}

	function handleIssueClick(issue: Issue) {
		lastSelectedId = issue.id;
		goto(`/${slug}/issue/${issue.identifier}`);
	}

	const keyHandler = createKeyboardHandler([
		{ key: 'a', ctrl: true, handler: () => issuesState.selectAll() },
		{ key: 'Escape', handler: () => issuesState.clearSelection() },
	]);

	onMount(() => {
		document.addEventListener('keydown', keyHandler);
		return () => document.removeEventListener('keydown', keyHandler);
	});

	function toggleGroup(key: string) {
		const next = new Set(collapsedGroups);
		if (next.has(key)) {
			next.delete(key);
		} else {
			next.add(key);
		}
		collapsedGroups = next;
	}
</script>

<div class="flex h-full flex-col">
	<!-- Header -->
	<div class="flex h-[49px] items-center justify-between border-b border-[var(--app-border)] px-6">
		<h1 class="text-sm font-medium text-[var(--color-text-primary)]">My Issues</h1>
		<ViewSwitcher bind:layout onchange={handleLayoutChange} />
	</div>

	<!-- Tabs -->
	<Tabs.Root value={activeTab} onValueChange={handleTabChange}>
		<Tabs.List class="w-full justify-start gap-0 rounded-lg border-b border-[var(--app-border)] bg-transparent px-4">
			<Tabs.Trigger value="assigned" class="relative rounded-md border-b-2 border-transparent px-3 py-2 text-xs data-[state=active]:border-[var(--app-accent)] data-[state=active]:bg-transparent data-[state=active]:shadow-none">
				<CircleUser size={13} class="mr-1.5" />
				Assigned to me
			</Tabs.Trigger>
			<Tabs.Trigger value="created" class="relative rounded-md border-b-2 border-transparent px-3 py-2 text-xs data-[state=active]:border-[var(--app-accent)] data-[state=active]:bg-transparent data-[state=active]:shadow-none">
				<PenLine size={13} class="mr-1.5" />
				Created by me
			</Tabs.Trigger>
		</Tabs.List>
	</Tabs.Root>

	<!-- Filter bar -->
	<FilterBuilder
		bind:filters
		{projects}
		{labels}
		{members}
		onchange={handleFilterChange}
	/>

	<!-- Content -->
	{#if layout === 'list'}
		<div class="flex-1 overflow-y-auto">
			{#if !issuesState.loading && issuesState.issues.length === 0}
				<EmptyState
					title={activeTab === 'assigned' ? 'No issues assigned to you' : 'No issues created by you'}
					description={activeTab === 'assigned' ? 'Issues assigned to you will appear here' : 'Issues you created will appear here'}
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
							<IssueRow {issue} {slug} {members} {labels} {projects} onclick={handleIssueClick} {lastSelectedId} onlastselected={(id) => lastSelectedId = id} onaddrelation={handleAddRelation} />
						{/each}
					{/if}
				{/each}
			{:else}
				{#each issuesState.issues as issue (issue.id)}
					<IssueRow {issue} {slug} {members} {labels} {projects} onclick={handleIssueClick} {lastSelectedId} onlastselected={(id) => lastSelectedId = id} onaddrelation={handleAddRelation} />
				{/each}
			{/if}

			<BulkActionBar {slug} />
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

<AddRelationDialog
	bind:open={relationDialogOpen}
	{slug}
	identifier={relationIssue?.identifier ?? ''}
	defaultType={relationDefaultType}
/>
