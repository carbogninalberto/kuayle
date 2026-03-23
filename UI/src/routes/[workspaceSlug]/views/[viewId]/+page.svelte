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
	import type { View } from '$lib/types/view';
	import type { Issue } from '$lib/types/issue';
	import type { WorkspaceMember } from '$lib/types/workspace';
	import type { Label } from '$lib/types/label';
	import type { Team } from '$lib/types/team';
	import type { Project } from '$lib/types/project';
	import IssueRow from '$lib/features/issues/IssueRow.svelte';
	import { issuesState } from '$lib/features/issues/issues.state.svelte';
	import EmptyState from '$lib/components/shared/EmptyState.svelte';
	import { Button } from '$lib/components/ui/button';
	import * as Popover from '$lib/components/ui/popover';
	import { toast } from 'svelte-sonner';
	import { ArrowLeft, Pencil, Trash2, MoreHorizontal, Check, X } from 'lucide-svelte';
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
	let loading = $state(true);
	let actionsOpen = $state(false);
	let lastSelectedId = $state<string | null>(null);

	// Edit name state
	let editingName = $state(false);
	let editNameValue = $state('');

	$effect(() => {
		const s = slug;
		const v = viewId;
		if (!s || !v) return;
		loading = true;
		Promise.all([
			getView(s, v),
			listMembers(s),
			listLabels(s),
			listTeams(s),
			listProjects(s)
		]).then(async ([viewData, m, l, t, p]) => {
			view = viewData;
			members = m;
			labels = l;
			teams = t;
			projects = p;
			await loadIssues(viewData);
		}).catch(() => {
			toast.error('View not found');
			goto(`/${s}`);
		}).finally(() => {
			loading = false;
		});
	});

	async function loadIssues(v: View) {
		const params: Record<string, string> = { per_page: '200' };
		if (v.filters) {
			for (const [key, val] of Object.entries(v.filters)) {
				if (val) params[key] = val;
			}
		}
		try {
			const res = await listIssues(slug, params);
			issues = res.data;
			const firstTeamId = issues[0]?.team_id;
			if (firstTeamId) {
				teamStatusesState.load(slug, firstTeamId);
			}
		} catch {
			issues = [];
		}
	}

	function resolveFilterValue(key: string, value: string): string {
		switch (key) {
			case 'label': {
				const names = value.split(',').map((id) => {
					const label = labels.find((l) => l.id === id);
					return label ? label.name : id;
				});
				return names.join(', ');
			}
			case 'assignee':
			case 'creator': {
				const names = value.split(',').map((id) => {
					const member = members.find((m) => m.user_id === id);
					return member ? member.name : id;
				});
				return names.join(', ');
			}
			case 'team': {
				const names = value.split(',').map((id) => {
					const team = teams.find((t) => t.id === id);
					return team ? team.name : id;
				});
				return names.join(', ');
			}
			case 'project': {
				const names = value.split(',').map((id) => {
					const project = projects.find((p) => p.id === id);
					return project ? project.name : id;
				});
				return names.join(', ');
			}
			case 'status':
			case 'priority':
			case 'search':
			default:
				return value;
		}
	}

	function filterLabel(key: string): string {
		const labels: Record<string, string> = {
			status: 'Status',
			priority: 'Priority',
			assignee: 'Assignee',
			creator: 'Creator',
			team: 'Team',
			project: 'Project',
			label: 'Label',
			search: 'Search',
			due_before: 'Due before',
			due_after: 'Due after'
		};
		return labels[key] ?? key;
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
			goto(`/${slug}`);
		} catch (err: any) {
			toast.error(err?.error?.message || 'Failed to delete view');
		}
	}

	function handleIssueClick(issue: Issue) {
		lastSelectedId = issue.id;
		goto(`/${slug}/issue/${issue.identifier}`);
	}

	const keyHandler = createKeyboardHandler([
		{ key: 'a', ctrl: true, handler: () => issuesState.selectAll() },
		{ key: 'Escape', handler: () => issuesState.clearSelection() }
	]);

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
		<div
			class="flex h-[49px] items-center justify-between border-b border-[var(--app-border)] px-6"
		>
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
								handleDelete();
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

		<!-- Active filters -->
		{#if view.filters && Object.keys(view.filters).length > 0}
			<div
				class="flex flex-wrap items-center gap-2 border-b border-[var(--app-border)] px-6 py-2"
			>
				<span class="text-xs text-[var(--color-text-tertiary)]">Filters:</span>
				{#each Object.entries(view.filters) as [key, val]}
					{#if val}
						<span
							class="rounded bg-[var(--color-bg-tertiary)] px-2 py-0.5 text-xs text-[var(--color-text-secondary)]"
						>
							{filterLabel(key)}: {resolveFilterValue(key, val)}
						</span>
					{/if}
				{/each}
			</div>
		{/if}

		<!-- Description -->
		{#if view.description}
			<div class="border-b border-[var(--app-border)] px-6 py-2">
				<p class="text-sm text-[var(--color-text-secondary)]">{view.description}</p>
			</div>
		{/if}

		<!-- Issues list -->
		<div class="flex-1 overflow-y-auto">
			{#if issues.length === 0}
				<EmptyState
					title="No issues match this view"
					description="Adjust the filters or add new issues"
				/>
			{:else}
				{#each issues as issue (issue.id)}
					<IssueRow
						{issue}
						{slug}
						{members}
						{labels}
						{projects}
						{lastSelectedId}
						onlastselected={(id) => (lastSelectedId = id)}
						onclick={handleIssueClick}
					/>
				{/each}
			{/if}
		</div>
	{/if}
</div>
