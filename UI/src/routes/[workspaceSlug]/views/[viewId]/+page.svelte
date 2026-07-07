<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { getView, updateView, deleteView } from '$lib/api/views';
	import { listIssues } from '$lib/api/issues';
	import { listMembers } from '$lib/api/members';
	import { listLabels } from '$lib/api/labels';
	import { listTeams } from '$lib/api/teams';
	import { listProjects } from '$lib/api/projects';
	import { teamStatusesState } from '$lib/features/issues/team-statuses.state.svelte';
	import { authState } from '$lib/features/auth/auth.state.svelte';
	import type { View, ViewFilter } from '$lib/types/view';
	import type { Issue } from '$lib/types/issue';
	import type { WorkspaceMember } from '$lib/types/workspace';
	import type { Label } from '$lib/types/label';
	import type { Team } from '$lib/types/team';
	import type { Project } from '$lib/types/project';
	import IssueTreeItem from '$lib/features/issues/IssueTreeItem.svelte';
	import { issuesState } from '$lib/features/issues/issues.state.svelte';
	import EmptyState from '$lib/components/shared/EmptyState.svelte';
	import { Button } from '$lib/components/ui/button';
	import * as Popover from '$lib/components/ui/popover';
	import * as Tooltip from '$lib/components/ui/tooltip';
	import * as AlertDialog from '$lib/components/ui/alert-dialog';
	import { toast } from 'svelte-sonner';
	import { ArrowLeft, Pencil, Trash2, MoreHorizontal, Check, X, Share2 } from 'lucide-svelte';
	import FilterBuilder from '$lib/components/shared/FilterBuilder.svelte';
	import ShareLinkDialog from '$lib/components/shared/ShareLinkDialog.svelte';
	import SidebarToggle from '$lib/components/layout/SidebarToggle.svelte';
	import { createKeyboardHandler } from '$lib/utils/keyboard';

	const slug = $derived(page.params.workspaceSlug ?? '');
	const viewId = $derived(page.params.viewId ?? '');

	let view = $state<View | null>(null);
	let issues = $state<Issue[]>([]);
	let members = $state<WorkspaceMember[]>([]);
	let labels = $state<Label[]>([]);
	let teams = $state<Team[]>([]);
	let projects = $state<Project[]>([]);

	const isOwner = $derived(!!authState.user && !!view && authState.user.id === view.creator_id);
	const owner = $derived(members.find((m) => m.user_id === view?.creator_id));
	let loading = $state(true);
	let actionsOpen = $state(false);
	let showShareLink = $state(false);
	let deleteOpen = $state(false);
	let lastSelectedId = $state<string | null>(null);
	let filters = $state<ViewFilter>({});
	let saveTimeout: ReturnType<typeof setTimeout> | null = null;
	let visibleTreeIssues = $derived(
		issues.filter((issue) => !issue.parent_id || !issues.some((parent) => parent.id === issue.parent_id))
	);

	// Edit name state
	let editingName = $state(false);
	let editNameValue = $state('');

	$effect(() => {
		const s = slug;
		const v = viewId;
		if (!s || !v) return;
		loading = true;
		Promise.all([getView(s, v), listMembers(s), listLabels(s), listTeams(s), listProjects(s)])
			.then(async ([viewData, m, l, t, p]) => {
				view = viewData;
				members = m;
				labels = l;
				teams = t;
				projects = p;
				filters = { ...viewData.filters };
				await loadIssues();
			})
			.catch(() => {
				toast.error('View not found');
				goto(`/${s}/inbox`);
			})
			.finally(() => {
				loading = false;
			});
	});

	async function loadIssues() {
		const params: Record<string, string> = { per_page: '200' };
		for (const [key, val] of Object.entries(filters)) {
			if (val) params[key] = val;
		}
		try {
			const res = await listIssues(slug, params);
			issues = res.data;
			await loadTeamStatuses();
		} catch {
			issues = [];
		}
	}

	async function loadTeamStatuses() {
		const teamId = filters.team || issues[0]?.team_id || teams[0]?.id;
		if (!teamId || !filters.status) return;
		await teamStatusesState.load(slug, teamId);
	}

	function startEditName() {
		if (!view) return;
		editNameValue = view.name;
		editingName = true;
	}

	async function saveName() {
		if (!view || !editNameValue.trim()) return;
		try {
			view = await updateView(slug, view.id, { name: editNameValue.trim() });
			editingName = false;
			toast.success('View name updated');
		} catch (err: any) {
			toast.error(err?.error?.message || 'Failed to update view');
		}
	}

	function cancelEditName() {
		editingName = false;
	}

	async function handleDelete() {
		if (!view) return;
		try {
			await deleteView(slug, view.id);
			toast.success('View deleted');
			goto(`/${slug}/inbox`);
		} catch (err: any) {
			toast.error(err?.error?.message || 'Failed to delete view');
		} finally {
			deleteOpen = false;
		}
	}

	function handleIssueClick(issue: Issue) {
		lastSelectedId = issue.id;
		goto(`/${slug}/issue/${issue.identifier}`);
	}

	function handleFilterChange(f: ViewFilter) {
		filters = f;
		loadIssues();
		if (saveTimeout) clearTimeout(saveTimeout);
		saveTimeout = setTimeout(saveFilters, 500);
	}

	async function saveFilters() {
		if (!view) return;
		try {
			view = await updateView(slug, view.id, { filters });
		} catch (err: any) {
			toast.error(err?.error?.message || 'Failed to save filters');
		}
	}

	const keyHandler = createKeyboardHandler([{ key: 'Escape', handler: () => issuesState.clearSelection() }]);

	onMount(() => {
		document.addEventListener('keydown', keyHandler);
		return () => document.removeEventListener('keydown', keyHandler);
	});

	function handleEditNameKeydown(e: KeyboardEvent) {
		if (e.key === 'Enter') saveName();
		if (e.key === 'Escape') cancelEditName();
	}
</script>

<div class="flex h-full flex-col">
	{#if !loading && view}
		<!-- Header -->
		<div class="flex h-[49px] items-center justify-between border-b border-[var(--app-border)] px-6">
			<div class="flex items-center gap-3">
				<SidebarToggle />
				<button
					onclick={() => history.back()}
					class="text-[var(--color-text-tertiary)] hover:text-[var(--color-text-secondary)]"
				>
					<ArrowLeft size={16} />
				</button>
				{#if editingName}
					<div class="flex items-center gap-1">
						<input
							type="text"
							bind:value={editNameValue}
							onkeydown={handleEditNameKeydown}
							class="rounded border border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-2 py-0.5 text-sm text-[var(--color-text-primary)] outline-none focus:border-[var(--app-accent)]"
						/>
						<Button variant="ghost" size="icon-sm" onclick={saveName}>
							<Check size={14} />
						</Button>
						<Button variant="ghost" size="icon-sm" onclick={cancelEditName}>
							<X size={14} />
						</Button>
					</div>
				{:else}
					<h1 class="text-sm font-medium text-[var(--color-text-primary)]">{view.name}</h1>
					<button
						onclick={startEditName}
						class="text-[var(--color-text-tertiary)] hover:text-[var(--color-text-secondary)]"
					>
						<Pencil size={12} />
					</button>
				{/if}
				{#if view.is_shared}
					<span
						class="rounded bg-[var(--color-bg-tertiary)] px-1.5 py-0.5 text-[10px] text-[var(--color-text-tertiary)]"
						>Shared</span
					>
				{/if}
			</div>
			<div class="flex items-center gap-2">
				{#if owner}
					<Tooltip.Root>
						<Tooltip.Trigger>
							<div
								class="flex h-7 w-7 items-center justify-center rounded-full bg-[var(--app-accent)] text-xs font-medium text-[var(--app-accent-foreground)]"
							>
								{#if authState.user?.id === owner.user_id && authState.user?.avatar_url}
									<img src={authState.user.avatar_url} alt="" class="h-7 w-7 rounded-full" />
								{:else}
									{(owner.name ?? owner.email ?? 'U').charAt(0).toUpperCase()}
								{/if}
							</div>
						</Tooltip.Trigger>
						<Tooltip.Content>
							<p class="text-sm font-medium">{owner.name || owner.email}</p>
							<p class="text-xs text-[var(--color-text-tertiary)]">Owner</p>
						</Tooltip.Content>
					</Tooltip.Root>
				{/if}
				<Popover.Root bind:open={actionsOpen}>
					<Popover.Trigger>
						<Button variant="ghost" size="icon-sm">
							<MoreHorizontal size={14} />
						</Button>
					</Popover.Trigger>
					<Popover.Content class="w-40 p-1" align="end">
						<button
							onclick={() => {
								actionsOpen = false;
								showShareLink = true;
							}}
							class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-text-primary)] hover:bg-[var(--color-bg-hover)]"
						>
							<Share2 size={14} />
							Share link
						</button>
						<button
							onclick={() => {
								actionsOpen = false;
								deleteOpen = true;
							}}
							class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-error)] hover:bg-[var(--color-bg-hover)]"
						>
							<Trash2 size={14} />
							Delete view
						</button>
					</Popover.Content>
				</Popover.Root>
			</div>
		</div>

		<!-- Filter bar -->
		<div class="border-b border-[var(--app-border)]">
			<FilterBuilder
				bind:filters
				{teams}
				{projects}
				{labels}
				{members}
				readonly={!isOwner}
				onchange={handleFilterChange}
			/>
		</div>

		<!-- Description -->
		{#if view.description}
			<div class="border-b border-[var(--app-border)] px-6 py-2">
				<p class="text-sm text-[var(--color-text-secondary)]">{view.description}</p>
			</div>
		{/if}

		<!-- Issues list -->
		<div class="flex-1 overflow-y-auto">
			{#if issues.length === 0}
				<EmptyState title="No issues match this view" description="Adjust the filters or add new issues" />
			{:else}
				{#each visibleTreeIssues as issue (issue.id)}
					<IssueTreeItem
						{issue}
						{slug}
						{members}
						{labels}
						{projects}
						{lastSelectedId}
						singleSelect
						onlastselected={(id) => (lastSelectedId = id)}
						onclick={handleIssueClick}
						onupdated={loadIssues}
					/>
				{/each}
			{/if}
		</div>
	{/if}
</div>

{#if view}
	<ShareLinkDialog bind:open={showShareLink} {slug} scope="view" scopeId={viewId} filters={view.filters} />

	<AlertDialog.Root bind:open={deleteOpen}>
		<AlertDialog.Content>
			<AlertDialog.Header>
				<AlertDialog.Title>Delete view?</AlertDialog.Title>
				<AlertDialog.Description>This will permanently delete {view.name}.</AlertDialog.Description>
			</AlertDialog.Header>
			<AlertDialog.Footer>
				<AlertDialog.Cancel variant="outline">Cancel</AlertDialog.Cancel>
				<AlertDialog.Action variant="destructive" onclick={handleDelete}>Delete view</AlertDialog.Action>
			</AlertDialog.Footer>
		</AlertDialog.Content>
	</AlertDialog.Root>
{/if}
