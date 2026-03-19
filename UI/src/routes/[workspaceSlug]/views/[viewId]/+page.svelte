<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { getView, deleteView } from '$lib/api/views';
	import { issuesState } from '$lib/features/issues/issues.state.svelte';
	import IssueRow from '$lib/features/issues/IssueRow.svelte';
	import IssueDetail from '$lib/features/issues/IssueDetail.svelte';
	import KanbanBoard from '$lib/features/issues/KanbanBoard.svelte';
	import ViewSwitcher from '$lib/components/shared/ViewSwitcher.svelte';
	import EmptyState from '$lib/components/shared/EmptyState.svelte';
	import LoadingState from '$lib/components/shared/LoadingState.svelte';
	import { Button } from '$lib/components/ui/button';
	import { Badge } from '$lib/components/ui/badge';
	import type { View, ViewLayout, ViewFilter } from '$lib/types/view';
	import { STATUS_LABELS, PRIORITY_LABELS } from '$lib/types/issue';
	import { toast } from 'svelte-sonner';
	import { goto } from '$app/navigation';
	import { Trash2, Share2 } from 'lucide-svelte';

	const slug = $derived(page.params.workspaceSlug ?? '');
	const viewId = $derived(page.params.viewId ?? '');

	let view = $state<View | null>(null);
	let layout = $state<ViewLayout>('list');
	let loading = $state(true);

	onMount(async () => {
		try {
			view = await getView(slug, viewId);
			loadIssues();
		} catch {
			toast.error('View not found');
			goto(`/${slug}/my-issues`);
		} finally {
			loading = false;
		}
	});

	function loadIssues() {
		if (!view) return;
		const params: Record<string, string> = {};
		const filters = view.filters as ViewFilter;
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

	function handleLayoutChange(l: ViewLayout) {
		layout = l;
		loadIssues();
	}

	async function handleDelete() {
		if (!view) return;
		try {
			await deleteView(slug, view.id);
			toast.success('View deleted');
			goto(`/${slug}/my-issues`);
		} catch (err: any) {
			toast.error(err?.error?.message || 'Failed to delete view');
		}
	}

	function getFilterLabels(filters: ViewFilter): { key: string; label: string }[] {
		const result: { key: string; label: string }[] = [];
		if (filters.status) result.push({ key: 'Status', label: filters.status.split(',').map(s => STATUS_LABELS[s as keyof typeof STATUS_LABELS] || s).join(', ') });
		if (filters.priority) result.push({ key: 'Priority', label: filters.priority.split(',').map(p => PRIORITY_LABELS[Number(p) as keyof typeof PRIORITY_LABELS] || p).join(', ') });
		if (filters.assignee) result.push({ key: 'Assignee', label: filters.assignee === 'none' ? 'Unassigned' : 'Filtered' });
		if (filters.project) result.push({ key: 'Project', label: filters.project === 'none' ? 'None' : 'Filtered' });
		if (filters.search) result.push({ key: 'Search', label: filters.search });
		return result;
	}
</script>

<div class="flex h-full flex-col">
	{#if loading}
		<LoadingState />
	{:else if view}
		<!-- Header -->
		<div class="flex h-[49px] items-center justify-between border-b border-[var(--app-border)] px-6">
			<div class="flex items-center gap-3">
				<h1 class="text-sm font-medium text-[var(--color-text-primary)]">{view.name}</h1>
				{#if view.is_shared}
					<Badge variant="outline" class="text-[10px]">
						<Share2 size={10} class="mr-1" />
						Shared
					</Badge>
				{/if}
			</div>
			<div class="flex items-center gap-2">
				<ViewSwitcher bind:layout onchange={handleLayoutChange} />
				<Button variant="ghost" size="icon-sm" onclick={handleDelete} class="text-[var(--color-text-tertiary)] hover:text-[var(--color-error)]">
					<Trash2 size={14} />
				</Button>
			</div>
		</div>

		<!-- Filter summary -->
		{#if getFilterLabels(view.filters).length > 0}
			<div class="flex items-center gap-1.5 border-b border-[var(--app-border)] px-4 py-2">
				<span class="text-xs text-[var(--color-text-tertiary)]">Filters:</span>
				{#each getFilterLabels(view.filters) as filter}
					<span class="rounded-md bg-[var(--app-accent)]/10 border border-[var(--app-accent)]/30 px-2 py-0.5 text-xs text-[var(--app-accent-light)]">
						{filter.key}: {filter.label}
					</span>
				{/each}
			</div>
		{/if}

		<!-- Content -->
		{#if layout === 'list'}
			<div class="flex-1 overflow-y-auto">
				{#if issuesState.loading}
					<LoadingState />
				{:else if issuesState.issues.length === 0}
					<EmptyState
						title="No issues match this view"
						description="Try adjusting the view's filters"
					/>
				{:else}
					{#each issuesState.issues as issue (issue.id)}
						<IssueRow {issue} onclick={(i) => issuesState.select(i)} />
					{/each}
				{/if}
			</div>
		{:else}
			{#if issuesState.loading}
				<LoadingState />
			{:else}
				<div class="flex-1 overflow-hidden">
					<KanbanBoard
						issuesByStatus={issuesState.issuesByStatus}
						onissueclick={(i) => issuesState.select(i)}
					/>
				</div>
			{/if}
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
