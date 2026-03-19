<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { getCycle, completeCycle, updateCycle, deleteCycle } from '$lib/api/cycles';
	import { issuesState } from '$lib/features/issues/issues.state.svelte';
	import type { Cycle } from '$lib/types/cycle';
	import IssueRow from '$lib/features/issues/IssueRow.svelte';
	import IssueDetail from '$lib/features/issues/IssueDetail.svelte';
	import CycleProgress from '$lib/features/cycles/CycleProgress.svelte';
	import EmptyState from '$lib/components/shared/EmptyState.svelte';
	import LoadingState from '$lib/components/shared/LoadingState.svelte';
	import { Badge } from '$lib/components/ui/badge';
	import { Button } from '$lib/components/ui/button';
	import * as Popover from '$lib/components/ui/popover';
	import { toast } from 'svelte-sonner';
	import { formatRelativeTime } from '$lib/utils/format';
	import { ArrowLeft, CheckCircle2, Play, Clock, Trash2, MoreHorizontal } from 'lucide-svelte';

	const slug = $derived(page.params.workspaceSlug ?? '');
	const teamId = $derived(page.params.teamId ?? '');
	const cycleId = $derived(page.params.cycleId ?? '');

	let cycle = $state<Cycle | null>(null);
	let loading = $state(true);
	let actionsOpen = $state(false);

	onMount(async () => {
		try {
			cycle = await getCycle(slug, teamId, cycleId);
			// Load issues for this cycle
			issuesState.load(slug, { team: teamId, per_page: '200' });
		} catch {
			toast.error('Cycle not found');
			goto(`/${slug}/teams/${teamId}/cycles`);
		} finally {
			loading = false;
		}
	});

	// Filter issues by cycle_id on the client side since we already have the issue data
	// A proper implementation would filter server-side, but cycle_id filtering isn't exposed in query params yet
	let cycleIssues = $derived(
		issuesState.issues.filter((i) => i.cycle_id === cycleId)
	);

	async function handleComplete() {
		if (!cycle) return;
		try {
			cycle = await completeCycle(slug, teamId, cycle.id);
			toast.success('Cycle completed');
		} catch (err: any) {
			toast.error(err?.error?.message || 'Failed to complete cycle');
		}
	}

	async function handleActivate() {
		if (!cycle) return;
		try {
			cycle = await updateCycle(slug, teamId, cycle.id, { status: 'active' });
			toast.success('Cycle activated');
		} catch (err: any) {
			toast.error(err?.error?.message || 'Failed to activate cycle');
		}
	}

	async function handleDelete() {
		if (!cycle) return;
		try {
			await deleteCycle(slug, teamId, cycle.id);
			toast.success('Cycle deleted');
			goto(`/${slug}/teams/${teamId}/cycles`);
		} catch (err: any) {
			toast.error(err?.error?.message || 'Failed to delete cycle');
		}
	}

	function formatDate(date: string | null): string {
		if (!date) return '—';
		return new Date(date).toLocaleDateString('en-US', { month: 'short', day: 'numeric', year: 'numeric' });
	}

	const STATUS_ICONS = {
		upcoming: Clock,
		active: Play,
		completed: CheckCircle2
	} as const;
</script>

<div class="flex h-full flex-col">
	{#if loading}
		<LoadingState />
	{:else if cycle}
		<!-- Header -->
		<div class="flex h-[49px] items-center justify-between border-b border-[var(--app-border)] px-6">
			<div class="flex items-center gap-3">
				<a
					href="/{slug}/teams/{teamId}/cycles"
					class="text-[var(--color-text-tertiary)] hover:text-[var(--color-text-secondary)]"
				>
					<ArrowLeft size={16} />
				</a>
				<h1 class="text-sm font-medium text-[var(--color-text-primary)]">{cycle.name}</h1>
				<Badge variant={cycle.status === 'active' ? 'default' : cycle.status === 'completed' ? 'secondary' : 'outline'} class="text-[10px]">
					{cycle.status}
				</Badge>
			</div>
			<div class="flex items-center gap-2">
				{#if cycle.status === 'upcoming'}
					<Button size="sm" onclick={handleActivate}>
						<Play size={14} class="mr-1" />
						Start cycle
					</Button>
				{/if}
				{#if cycle.status === 'active'}
					<Button size="sm" onclick={handleComplete}>
						<CheckCircle2 size={14} class="mr-1" />
						Complete
					</Button>
				{/if}
				<Popover.Root bind:open={actionsOpen}>
					<Popover.Trigger>
						<Button variant="ghost" size="icon-sm">
							<MoreHorizontal size={14} />
						</Button>
					</Popover.Trigger>
					<Popover.Content class="w-40 p-1" align="end">
						<button
							onclick={() => { actionsOpen = false; handleDelete(); }}
							class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-error)] hover:bg-[var(--color-bg-hover)]"
						>
							<Trash2 size={14} />
							Delete cycle
						</button>
					</Popover.Content>
				</Popover.Root>
			</div>
		</div>

		<!-- Cycle info -->
		<div class="border-b border-[var(--app-border)] px-6 py-4">
			<div class="flex items-center gap-6">
				<div class="flex items-center gap-2 text-xs text-[var(--color-text-tertiary)]">
					<Clock size={12} />
					{formatDate(cycle.start_date)} → {formatDate(cycle.end_date)}
				</div>
				{#if cycle.progress}
					<div class="flex items-center gap-2 text-xs text-[var(--color-text-tertiary)]">
						<span>{cycle.progress.completed} of {cycle.progress.total} issues done</span>
					</div>
				{/if}
				{#if cycle.completed_at}
					<div class="text-xs text-[var(--color-text-tertiary)]">
						Completed {formatRelativeTime(cycle.completed_at)}
					</div>
				{/if}
			</div>
			{#if cycle.description}
				<p class="mt-2 text-sm text-[var(--color-text-secondary)]">{cycle.description}</p>
			{/if}
			{#if cycle.progress && cycle.progress.total > 0}
				<div class="mt-3 w-64">
					<CycleProgress progress={cycle.progress} />
				</div>
			{/if}
		</div>

		<!-- Issues list -->
		<div class="flex-1 overflow-y-auto">
			{#if issuesState.loading}
				<LoadingState />
			{:else if cycleIssues.length === 0}
				<EmptyState
					title="No issues in this cycle"
					description="Assign issues to this cycle from the issue detail panel"
				/>
			{:else}
				{#each cycleIssues as issue (issue.id)}
					<IssueRow {issue} onclick={(i) => issuesState.select(i)} />
				{/each}
			{/if}
		</div>
	{/if}
</div>

{#if issuesState.selectedIssue}
	<IssueDetail
		issue={issuesState.selectedIssue}
		{slug}
		onclose={() => issuesState.select(null)}
	/>
{/if}
