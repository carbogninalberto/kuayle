<script lang="ts">
	import { onMount, untrack } from 'svelte';
	import { goto } from '$app/navigation';
	import { listTeams } from '$lib/api/teams';
	import { listProjects } from '$lib/api/projects';
	import { listLabels } from '$lib/api/labels';
	import { listMembers } from '$lib/api/members';
	import { listCycles } from '$lib/api/cycles';
	import { listTeamStatuses } from '$lib/api/team-statuses';
	import { createIssue, updateIssue } from '$lib/api/issues';
	import type { Issue, CreateIssueRequest, IssuePriority, RelationType } from '$lib/types/issue';
	import type { Team } from '$lib/types/team';
	import type { TeamStatus } from '$lib/types/team-status';
	import type { Project } from '$lib/types/project';
	import type { Label } from '$lib/types/label';
	import type { Cycle } from '$lib/types/cycle';
	import type { WorkspaceMember } from '$lib/types/workspace';
	import type { ViewFilter, ViewLayout } from '$lib/types/view';
	import type { Role } from '$lib/security/roles';
	import { hasPermission } from '$lib/security/permissions';
	import { authState } from '$lib/features/auth/auth.state.svelte';
	import { issuesState, type GroupByField } from './issues.state.svelte';
	import { teamStatusesState } from './team-statuses.state.svelte';
	import { belongsToScope, scopedIssueParams, updateIssueMembership, type IssueScope } from './scoped-issues';
	import IssueRow from './IssueRow.svelte';
	import IssueGroupHeader from './IssueGroupHeader.svelte';
	import IssueListLoadMore from './IssueListLoadMore.svelte';
	import KanbanBoard from './KanbanBoard.svelte';
	import BulkActionBar from './BulkActionBar.svelte';
	import CreateIssueDialog from './CreateIssueDialog.svelte';
	import IssuePickerDialog from './IssuePickerDialog.svelte';
	import AddRelationDialog from './AddRelationDialog.svelte';
	import { showIssueCreatedToast } from './issue-created-toast';
	import FilterBuilder from '$lib/components/shared/FilterBuilder.svelte';
	import ViewSwitcher from '$lib/components/shared/ViewSwitcher.svelte';
	import EmptyState from '$lib/components/shared/EmptyState.svelte';
	import GanttChart from '$lib/features/projects/GanttChart.svelte';
	import { Button } from '$lib/components/ui/button';
	import * as Popover from '$lib/components/ui/popover';
	import { IsMobile } from '$lib/hooks/is-mobile.svelte';
	import { createKeyboardHandler } from '$lib/utils/keyboard';
	import { appToast } from '$lib/features/toast/toast';
	import { m } from '$lib/paraglide/messages.js';
	import { Plus, Layers, BarChart3 } from 'lucide-svelte';

	let {
		slug,
		scope,
		role,
		teamOptions,
		cycleOptions,
		allowGantt = false,
		refreshVersion = 0,
		onchanged
	}: {
		slug: string;
		scope: IssueScope;
		role: Role;
		teamOptions?: Team[];
		cycleOptions?: Cycle[];
		allowGantt?: boolean;
		refreshVersion?: number;
		onchanged?: () => void;
	} = $props();
	let teams = $state<Team[]>([]);
	let projects = $state<Project[]>([]);
	let labels = $state<Label[]>([]);
	let members = $state<WorkspaceMember[]>([]);
	let cycles = $state<Cycle[]>([]);
	let statuses = $state<TeamStatus[]>([]);
	let loading = $state(true);
	let failed = $state(false);
	let showPicker = $state(false);
	let showCreate = $state(false);
	let layout = $state<ViewLayout>('list');
	let gantt = $state(false);
	let groupBy = $state<GroupByField>('status');
	let filters = $state<ViewFilter>({});
	let collapsed = $state(new Set<string>());
	let groupOpen = $state(false);
	let lastSelectedId = $state<string | null>(null);
	let saving = $state(false);
	let relationIssue = $state<Issue | null>(null);
	let relationOpen = $state(false);
	let relationType = $state<RelationType>('related');
	let quickDefaults = $state<{
		statusId?: string;
		priority?: IssuePriority;
		assigneeIds?: string[];
		projectId?: string | null;
	}>({});
	let active = true;
	const mobile = new IsMobile();
	const canUpdate = $derived(hasPermission(role, 'issue:update'));
	const canCreate = $derived(hasPermission(role, 'issue:create'));
	const removeLabel = $derived(
		scope.kind === 'cycle' ? m['scopedIssues.remove_cycle']() : m['scopedIssues.remove_project']()
	);
	const selectedIssues = $derived(issuesState.issues.filter((issue) => issuesState.selectedIds.has(issue.id)));
	const selectedTeamId = $derived(
		selectedIssues.length && selectedIssues.every((issue) => issue.team_id === selectedIssues[0].team_id)
			? selectedIssues[0].team_id
			: null
	);
	const groupOptions = $derived([
		{ value: 'status' as GroupByField, label: m['common.group_by.status']() },
		{ value: 'priority' as GroupByField, label: m['common.group_by.priority']() },
		{ value: 'assignee' as GroupByField, label: m['common.group_by.assignee']() },
		{ value: 'project' as GroupByField, label: m['common.group_by.project']() },
		{ value: null, label: m['common.group_by.no_grouping']() }
	]);

	function canDeleteIssue(issue: Issue) {
		return (
			hasPermission(role, 'issue:delete') ||
			(hasPermission(role, 'issue:delete_own') && issue.creator_id === authState.user?.id)
		);
	}
	function params() {
		return scopedIssueParams(scope, filters, layout === 'list' && !gantt ? groupBy : null);
	}
	async function loadIssues(showLoading = true) {
		if (!active) return;
		issuesState.groupBy = groupBy;
		await issuesState.load(slug, params(), showLoading);
	}
	function changed() {
		if (!active) return;
		void loadIssues(false);
		onchanged?.();
	}
	async function initialize() {
		loading = true;
		failed = false;
		try {
			const [te, pr, la, me] = await Promise.all([
				teamOptions ? Promise.resolve(teamOptions) : listTeams(slug),
				listProjects(slug),
				listLabels(slug),
				listMembers(slug)
			]);
			if (!active) return;
			teams = scope.teamId ? te.filter((team) => team.id === scope.teamId) : te;
			projects = pr;
			labels = la;
			members = me;
			const [statusLists, cycleLists] = await Promise.all([
				Promise.all(teams.map((team) => listTeamStatuses(slug, team.id))),
				cycleOptions ? Promise.resolve([cycleOptions]) : Promise.all(teams.map((team) => listCycles(slug, team.id)))
			]);
			if (!active) return;
			statuses = statusLists.flat();
			cycles = cycleLists.flat();
			teamStatusesState.clear();
			teamStatusesState.statuses = statuses;
			await loadIssues();
		} catch {
			if (active) failed = true;
		} finally {
			if (active) loading = false;
		}
	}
	onMount(() => {
		active = true;
		void initialize();
		const handler = createKeyboardHandler([
			{
				key: 'a',
				ctrl: true,
				handler: () => {
					if (canUpdate) issuesState.selectAll();
				}
			},
			{
				key: 'x',
				handler: () => {
					if (canUpdate && lastSelectedId) issuesState.toggleSelect(lastSelectedId);
				}
			},
			{ key: 'Escape', handler: () => issuesState.clearSelection() },
			...['Delete', 'Backspace'].map((key) => ({
				key,
				handler: () => {
					if (selectedIssues.length && selectedIssues.every(canDeleteIssue))
						window.dispatchEvent(new CustomEvent('issues:bulk-delete-request'));
				}
			}))
		]);
		const keydown = (event: KeyboardEvent) => {
			if (
				showPicker ||
				showCreate ||
				relationOpen ||
				(event.target as HTMLElement).closest('[role="dialog"], [role="alertdialog"], [role="menu"]')
			)
				return;
			handler(event);
		};
		document.addEventListener('keydown', keydown);
		return () => {
			active = false;
			document.removeEventListener('keydown', keydown);
		};
	});
	$effect(() => {
		if (mobile.current && layout === 'board') {
			layout = 'list';
			untrack(() => void loadIssues());
		}
	});
	$effect(() => {
		if (!showCreate && statuses.length)
			untrack(() => {
				teamStatusesState.clear();
				teamStatusesState.statuses = statuses;
			});
	});
	$effect(() => {
		const version = refreshVersion;
		if (version) untrack(() => void loadIssues());
	});

	function openIssue(issue: Issue) {
		lastSelectedId = issue.id;
		void goto(`/${slug}/issue/${issue.identifier}`);
	}
	function openCreate(key?: string) {
		if (!canCreate) return;
		quickDefaults = {};
		if (key && groupBy === 'status') quickDefaults.statusId = key;
		if (key && groupBy === 'priority') quickDefaults.priority = Number(key) as IssuePriority;
		if (key && groupBy === 'assignee') quickDefaults.assigneeIds = key === 'unassigned' ? [] : [key];
		if (key && groupBy === 'project') quickDefaults.projectId = key === 'no-project' ? null : key;
		showCreate = true;
	}
	async function create(req: CreateIssueRequest) {
		if (!canCreate) return;
		try {
			const issue = await createIssue(slug, req);
			showIssueCreatedToast(slug, issue);
			changed();
		} catch (error) {
			appToast.apiError(error, m['scopedIssues.failed_create']());
		}
	}
	async function assign(issues: Issue[], remove = false) {
		if (!canUpdate || saving) return issues;
		saving = true;
		try {
			const result = await updateIssueMembership(issues, scope, remove, (identifier, patch) =>
				updateIssue(slug, identifier, patch)
			);
			if (active) {
				await loadIssues(false);
				issuesState.selectedIds = new Set(remove ? result.failed.map((issue) => issue.id) : []);
				onchanged?.();
				if (remove && result.failed.length)
					appToast.error(m['issuePicker.partial_failure']({ count: result.failed.length }));
			}
			return result.failed;
		} finally {
			saving = false;
		}
	}
</script>

<div class="flex min-h-0 flex-1 flex-col" data-testid="scoped-issues">
	<div class="flex flex-wrap items-center gap-2 border-b border-[var(--app-border)] px-3 py-2 sm:px-4">
		{#if canUpdate}<Button variant="outline" size="sm" disabled={loading || failed} onclick={() => (showPicker = true)}
				>{m['scopedIssues.add']()}</Button
			>{/if}
		{#if canCreate}<Button size="sm" disabled={loading || failed} onclick={() => openCreate()}
				><Plus size={14} />{m['sidebar.new_issue']()}</Button
			>{/if}
		{#if canUpdate && selectedIssues.length}<Button
				variant="outline"
				size="sm"
				disabled={saving}
				onclick={() => assign(selectedIssues, true)}>{removeLabel} ({selectedIssues.length})</Button
			>{/if}
		<div class="ml-auto flex items-center gap-2">
			{#if layout === 'list' && !gantt}
				<Popover.Root bind:open={groupOpen}>
					<Popover.Trigger
						class="flex items-center gap-1 rounded-md border border-[var(--app-border)] px-2 py-1 text-xs"
						aria-label={m['common.group_by']()}><Layers size={12} />{m['common.group']()}</Popover.Trigger
					>
					<Popover.Content class="w-40 p-1" align="end">
						{#each groupOptions as option}<button
								class="w-full rounded-md px-2 py-1.5 text-left text-sm hover:bg-[var(--color-bg-hover)]"
								onclick={() => {
									groupBy = option.value;
									groupOpen = false;
									void loadIssues();
								}}>{option.label}</button
							>{/each}
					</Popover.Content>
				</Popover.Root>
			{/if}
			<div class="hidden sm:block">
				<ViewSwitcher
					{layout}
					inactive={gantt}
					onchange={(value) => {
						layout = value;
						gantt = false;
						void loadIssues();
					}}
				/>
			</div>
			{#if allowGantt}<Button
					variant={gantt ? 'secondary' : 'outline'}
					size="sm"
					aria-pressed={gantt}
					onclick={() => {
						gantt = !gantt;
						void loadIssues();
					}}><BarChart3 size={14} />{m['projects.gantt_view']()}</Button
				>{/if}
		</div>
	</div>
	<FilterBuilder
		bind:filters
		{teams}
		{projects}
		{labels}
		{members}
		excludedFilters={[scope.kind]}
		onchange={() => void loadIssues()}
	/>
	{#if loading || issuesState.loading}
		<div role="status" class="animate-pulse space-y-3 p-4 text-sm text-[var(--color-text-tertiary)]">
			{m['common.loading']()}{#each [1, 2, 3] as row}<div
					class="h-8 rounded bg-[var(--color-bg-secondary)]"
				></div>{/each}
		</div>
	{:else if failed || issuesState.loadError}
		<EmptyState
			title={m['issuePicker.failed_load']()}
			action={{ label: m['scopedIssues.retry'](), onclick: () => (failed ? void initialize() : void loadIssues()) }}
		/>
	{:else}
		<div class="relative min-h-0 flex-1 overflow-auto pb-16">
			{#if !issuesState.issues.length}
				<EmptyState
					title={m['issuePicker.empty']()}
					description={m['scopedIssues.empty']()}
					action={canCreate ? { label: m['sidebar.new_issue'](), onclick: () => openCreate() } : undefined}
				/>
			{:else if gantt}
				<div class="min-h-64 p-3"><GanttChart issues={issuesState.issues} {cycles} onissueclick={openIssue} /></div>
			{:else if layout === 'board'}
				<KanbanBoard
					issuesByStatus={issuesState.issuesByStatus}
					{slug}
					{members}
					{labels}
					{projects}
					{cycles}
					onissueclick={openIssue}
					readOnly={!canUpdate}
					membershipClearValue=""
					restrictToTeam
					accessibleRow
					{canDeleteIssue}
					onremove={canUpdate
						? (issue) => {
								void assign([issue], true);
							}
						: undefined}
					{removeLabel}
					onchanged={changed}
					onaddrelation={(issue, type) => {
						relationIssue = issue;
						relationType = type;
						relationOpen = true;
					}}
				/>
			{:else if groupBy}
				{#each issuesState.groupedIssues as group (group.key)}
					<IssueGroupHeader
						groupKey={group.key}
						{groupBy}
						groupLabel={group.label}
						count={group.issues.length}
						collapsed={collapsed.has(group.key)}
						{members}
						{projects}
						ontoggle={() => {
							const next = new Set(collapsed);
							if (next.has(group.key)) next.delete(group.key);
							else next.add(group.key);
							collapsed = next;
						}}
						onquickadd={canCreate ? openCreate : undefined}
					/>
					{#if !collapsed.has(group.key)}{#each group.issues as issue (issue.id)}{@render row(issue)}{/each}{/if}
				{/each}
			{:else}{#each issuesState.issues as issue (issue.id)}{@render row(issue)}{/each}{/if}
			<IssueListLoadMore />
			{#if canUpdate}<BulkActionBar
					{slug}
					{labels}
					{members}
					cycles={cycles.filter((cycle) => cycle.team_id === selectedTeamId)}
					statusOptions={statuses.filter((status) => status.team_id === selectedTeamId)}
					canDelete={selectedIssues.every(canDeleteIssue)}
					onchanged={changed}
				/>{/if}
		</div>
	{/if}
</div>

{#snippet row(issue: Issue)}
	<IssueRow
		{issue}
		{slug}
		{members}
		{labels}
		{projects}
		cycles={cycles.filter((cycle) => cycle.team_id === issue.team_id)}
		onclick={openIssue}
		{lastSelectedId}
		onlastselected={(id) => (lastSelectedId = id)}
		readOnly={!canUpdate}
		membershipClearValue=""
		canDelete={canDeleteIssue(issue)}
		restrictToTeam
		accessibleRow
		onremove={canUpdate
			? () => {
					void assign([issue], true);
				}
			: undefined}
		{removeLabel}
		onchanged={changed}
		onaddrelation={(issue, type) => {
			relationIssue = issue;
			relationType = type;
			relationOpen = true;
		}}
	/>
{/snippet}
{#if canUpdate}
	<IssuePickerDialog
		bind:open={showPicker}
		{slug}
		title={m['scopedIssues.add']()}
		description={scope.kind === 'cycle' ? m['scopedIssues.add_cycle_desc']() : m['scopedIssues.add_project_desc']()}
		actionLabel={m['scopedIssues.add']()}
		multiple
		params={scope.teamId ? { team: scope.teamId } : {}}
		filterIssue={(issue) => !belongsToScope(issue, scope)}
		onsubmit={(issues) => assign(issues)}
	/>
	<AddRelationDialog
		bind:open={relationOpen}
		{slug}
		identifier={relationIssue?.identifier ?? ''}
		defaultType={relationType}
		oncreated={changed}
	/>
{/if}
{#if canCreate}
	<CreateIssueDialog
		bind:open={showCreate}
		{slug}
		{teams}
		{projects}
		{labels}
		{members}
		{cycles}
		defaultTeamId={scope.teamId ?? statuses.find((status) => status.id === quickDefaults.statusId)?.team_id}
		defaultCycleId={scope.kind === 'cycle' ? scope.id : null}
		defaultProjectId={scope.kind === 'project' ? scope.id : quickDefaults.projectId}
		defaultStatusId={quickDefaults.statusId}
		defaultPriority={quickDefaults.priority}
		defaultAssigneeIds={quickDefaults.assigneeIds}
		onsubmit={create}
		onlabelcreated={(label) => (labels = [label, ...labels])}
	/>
{/if}
