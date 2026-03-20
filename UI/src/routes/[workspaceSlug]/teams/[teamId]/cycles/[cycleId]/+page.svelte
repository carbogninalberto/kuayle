<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { getCycle, completeCycle, updateCycle, deleteCycle } from '$lib/api/cycles';
	import { updateIssue } from '$lib/api/issues';
	import { listMembers } from '$lib/api/members';
	import { listLabels } from '$lib/api/labels';
	import { issuesState } from '$lib/features/issues/issues.state.svelte';
	import type { Cycle } from '$lib/types/cycle';
	import type { Issue } from '$lib/types/issue';
	import type { WorkspaceMember } from '$lib/types/workspace';
	import type { Label } from '$lib/types/label';
	import IssueRow from '$lib/features/issues/IssueRow.svelte';
import CycleProgress from '$lib/features/cycles/CycleProgress.svelte';
	import DatePickerPopover from '$lib/components/shared/DatePickerPopover.svelte';
	import EmptyState from '$lib/components/shared/EmptyState.svelte';
	import { Badge } from '$lib/components/ui/badge';
	import { Button } from '$lib/components/ui/button';
	import * as Popover from '$lib/components/ui/popover';
	import { toast } from 'svelte-sonner';
	import { formatRelativeTime } from '$lib/utils/format';
	import { ArrowLeft, CheckCircle2, Play, Clock, Trash2, MoreHorizontal, Search, Plus } from 'lucide-svelte';
	import { createKeyboardHandler } from '$lib/utils/keyboard';

	const slug = $derived(page.params.workspaceSlug ?? '');
	const teamId = $derived(page.params.teamId ?? '');
	const cycleId = $derived(page.params.cycleId ?? '');

	let cycle = $state<Cycle | null>(null);
	let loading = $state(true);
	let actionsOpen = $state(false);
	let members = $state<WorkspaceMember[]>([]);
	let labels = $state<Label[]>([]);

	let lastSelectedId = $state<string | null>(null);

	// Add issues search
	let addSearchQuery = $state('');
	let addSearchOpen = $state(false);

	onMount(async () => {
		try {
			const [c, m, l] = await Promise.all([
				getCycle(slug, teamId, cycleId),
				listMembers(slug),
				listLabels(slug)
			]);
			cycle = c;
			members = m;
			labels = l;
			// Load issues for this cycle using server-side cycle filter
			issuesState.load(slug, { cycle: cycleId, per_page: '200' });
		} catch {
			toast.error('Cycle not found');
			goto(`/${slug}/teams/${teamId}/cycles`);
		} finally {
			loading = false;
		}
	});

	// Issues available to add (same team, no cycle assigned) — requires a separate search
	let availableIssues = $state<import('$lib/types/issue').Issue[]>([]);
	let searchingAvailable = $state(false);

	async function searchAvailableIssues() {
		if (!addSearchQuery.trim()) {
			availableIssues = [];
			return;
		}
		searchingAvailable = true;
		try {
			const { listIssues } = await import('$lib/api/issues');
			const q = addSearchQuery.toLowerCase();
			const results = await listIssues(slug, { team: teamId, per_page: '50' });
			availableIssues = results.data
				.filter((i: import('$lib/types/issue').Issue) => i.cycle_id !== cycleId && (i.title.toLowerCase().includes(q) || i.identifier.toLowerCase().includes(q)))
				.slice(0, 10);
		} catch {
			availableIssues = [];
		} finally {
			searchingAvailable = false;
		}
	}

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

	async function handleStartDateChange(date: string | null) {
		if (!cycle) return;
		try {
			cycle = await updateCycle(slug, teamId, cycle.id, { start_date: date ?? undefined });
			toast.success('Start date updated');
		} catch (err: any) {
			toast.error(err?.error?.message || 'Failed to update start date');
		}
	}

	async function handleEndDateChange(date: string | null) {
		if (!cycle) return;
		try {
			cycle = await updateCycle(slug, teamId, cycle.id, { end_date: date ?? undefined });
			toast.success('End date updated');
		} catch (err: any) {
			toast.error(err?.error?.message || 'Failed to update end date');
		}
	}

	async function handleAddIssueToCycle(issue: Issue) {
		try {
			await updateIssue(slug, issue.identifier, { cycle_id: cycleId });
			// Reload to reflect change
			issuesState.load(slug, { cycle: cycleId, per_page: '200' });
			addSearchQuery = '';
			toast.success(`Added ${issue.identifier} to cycle`);
		} catch (err: any) {
			toast.error(err?.error?.message || 'Failed to add issue');
		}
	}

	const keyHandler = createKeyboardHandler([
		{ key: 'a', ctrl: true, handler: () => issuesState.selectAll() },
		{ key: 'Escape', handler: () => issuesState.clearSelection() },
	]);

	onMount(() => {
		document.addEventListener('keydown', keyHandler);
		return () => document.removeEventListener('keydown', keyHandler);
	});

	const STATUS_ICONS = {
		upcoming: Clock,
		active: Play,
		completed: CheckCircle2
	} as const;
</script>

<div class="flex h-full flex-col">
	{#if !loading && cycle}
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
					<DatePickerPopover
						value={cycle.start_date}
						onchange={handleStartDateChange}
						placeholder="Start date"
					/>
					<span>→</span>
					<DatePickerPopover
						value={cycle.end_date}
						onchange={handleEndDateChange}
						placeholder="End date"
					/>
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

		<!-- Add issues section -->
		<div class="border-b border-[var(--app-border)] px-6 py-3">
			<div class="relative">
				<div class="flex items-center gap-2 rounded-md border border-[var(--app-border)] bg-[var(--color-bg-secondary)] px-3 py-1.5">
					<Search size={14} class="text-[var(--color-text-tertiary)]" />
					<input
						type="text"
						bind:value={addSearchQuery}
						oninput={() => searchAvailableIssues()}
						onfocus={() => (addSearchOpen = true)}
						onblur={() => setTimeout(() => (addSearchOpen = false), 200)}
						placeholder="Search issues to add to this cycle..."
						class="w-full bg-transparent text-sm text-[var(--color-text-primary)] placeholder:text-[var(--color-text-tertiary)] outline-none"
					/>
				</div>
				{#if addSearchOpen && availableIssues.length > 0}
					<div class="absolute left-0 right-0 top-full z-50 mt-1 max-h-60 overflow-y-auto rounded-md border border-[var(--app-border)] bg-[var(--color-bg-primary)] shadow-lg">
						{#each availableIssues as issue}
							<button
								onmousedown={() => handleAddIssueToCycle(issue)}
								class="flex w-full items-center gap-3 px-3 py-2 text-left text-sm hover:bg-[var(--color-bg-hover)]"
							>
								<Plus size={14} class="shrink-0 text-[var(--color-text-tertiary)]" />
								<span class="shrink-0 text-xs text-[var(--color-text-tertiary)]">{issue.identifier}</span>
								<span class="truncate text-[var(--color-text-primary)]">{issue.title}</span>
							</button>
						{/each}
					</div>
				{/if}
			</div>
		</div>

		<!-- Issues list -->
		<div class="flex-1 overflow-y-auto">
			{#if !issuesState.loading && issuesState.issues.length === 0}
				<EmptyState
					title="No issues in this cycle"
					description="Search and add issues above, or assign issues from the issue detail panel"
				/>
			{:else}
				{#each issuesState.issues as issue (issue.id)}
					<IssueRow {issue} {slug} {members} {labels} {lastSelectedId} onlastselected={(id) => lastSelectedId = id} onclick={(i) => { lastSelectedId = i.id; goto(`/${slug}/issue/${i.identifier}`); }} />
				{/each}
			{/if}
		</div>
	{/if}
</div>
