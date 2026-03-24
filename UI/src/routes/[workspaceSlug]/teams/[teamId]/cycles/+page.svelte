<script lang="ts">
	import { page } from '$app/state';
	import { listCycles, createCycle, deleteCycle, completeCycle, updateCycle, getCycleBurndown } from '$lib/api/cycles';
	import type { Cycle, CycleBurndownPoint } from '$lib/types/cycle';
	import CreateCycleDialog from '$lib/features/cycles/CreateCycleDialog.svelte';
	import CycleTimelineRow from '$lib/features/cycles/CycleTimelineRow.svelte';
	import CycleBurndownChart from '$lib/features/cycles/CycleBurndownChart.svelte';
	import EmptyState from '$lib/components/shared/EmptyState.svelte';
	import { toast } from 'svelte-sonner';
	import { Plus, SquareUser, RefreshCcwDot, ChevronRight, Clock } from 'lucide-svelte';
	import SidebarToggle from '$lib/components/layout/SidebarToggle.svelte';
	import { sidebarState } from '$lib/features/layout/sidebar.state.svelte';
	import { cubicOut } from 'svelte/easing';

	function slideFade(node: HTMLElement, params: { duration?: number } = {}) {
		const duration = params.duration ?? 200;
		const h = node.offsetHeight;
		return {
			duration,
			easing: cubicOut,
			css: (t: number) => `overflow: hidden; height: ${t * h}px; opacity: ${t};`
		};
	}

	const slug = $derived(page.params.workspaceSlug ?? '');
	const teamId = $derived(page.params.teamId ?? '');

	let cycles = $state<Cycle[]>([]);
	let loading = $state(true);
	let showCreate = $state(false);
	let expandedCycleId = $state<string | null>(null);
	let burndownData = $state<CycleBurndownPoint[]>([]);
	let burndownLoading = $state(false);
	let archivedExpanded = $state(false);

	const MAX_VISIBLE_COMPLETED = 5;

	// Single flat sorted list: active first, then upcoming by start_date, then completed by completed_at desc
	const sortedCycles = $derived.by(() => {
		const active = cycles.filter(c => c.status === 'active');
		const upcoming = cycles.filter(c => c.status === 'upcoming')
			.sort((a, b) => {
				const aDate = a.start_date ? new Date(a.start_date).getTime() : Infinity;
				const bDate = b.start_date ? new Date(b.start_date).getTime() : Infinity;
				return aDate - bDate;
			});
		const completed = cycles.filter(c => c.status === 'completed')
			.sort((a, b) => {
				const aDate = a.completed_at ? new Date(a.completed_at).getTime() : 0;
				const bDate = b.completed_at ? new Date(b.completed_at).getTime() : 0;
				return bDate - aDate;
			});
		return [...active, ...upcoming, ...completed];
	});

	const visibleCycles = $derived(
		sortedCycles.filter((c, i) => {
			if (c.status !== 'completed') return true;
			const completedBefore = sortedCycles.filter((cc, ii) => ii < i && cc.status === 'completed').length;
			return completedBefore < MAX_VISIBLE_COMPLETED;
		})
	);

	const archivedCycles = $derived(
		sortedCycles.filter((c, i) => {
			if (c.status !== 'completed') return false;
			const completedBefore = sortedCycles.filter((cc, ii) => ii < i && cc.status === 'completed').length;
			return completedBefore >= MAX_VISIBLE_COMPLETED;
		})
	);

	$effect(() => {
		const s = slug;
		const t = teamId;
		if (!s || !t) return;
		loading = true;
		listCycles(s, t).then((c) => {
			cycles = c;
			// Auto-expand active cycle
			const active = c.find(cy => cy.status === 'active');
			if (active) {
				expandedCycleId = active.id;
			}
		}).finally(() => {
			loading = false;
		});
	});

	// Fetch burndown when expanded cycle changes
	$effect(() => {
		const id = expandedCycleId;
		if (!id || !slug || !teamId) {
			burndownData = [];
			return;
		}
		const cycle = cycles.find(c => c.id === id);
		if (!cycle || !cycle.start_date || !cycle.end_date) {
			burndownData = [];
			return;
		}
		burndownLoading = true;
		getCycleBurndown(slug, teamId, id).then((d) => {
			burndownData = d;
		}).catch(() => {
			burndownData = [];
		}).finally(() => {
			burndownLoading = false;
		});
	});

	async function handleCreate(data: { name: string; description?: string; start_date?: string; end_date?: string }) {
		try {
			const cycle = await createCycle(slug, teamId, data);
			cycles = [cycle, ...cycles];
			toast.success('Cycle created');
		} catch (err: any) {
			toast.error(err?.error?.message || 'Failed to create cycle');
		}
	}

	async function handleComplete(cycleId: string) {
		try {
			const updated = await completeCycle(slug, teamId, cycleId);
			cycles = cycles.map((c) => (c.id === cycleId ? updated : c));
			toast.success('Cycle completed');
		} catch (err: any) {
			toast.error(err?.error?.message || 'Failed to complete cycle');
		}
	}

	async function handleDelete(cycleId: string) {
		try {
			await deleteCycle(slug, teamId, cycleId);
			cycles = cycles.filter((c) => c.id !== cycleId);
			toast.success('Cycle deleted');
		} catch (err: any) {
			toast.error(err?.error?.message || 'Failed to delete cycle');
		}
	}

	function formatTimelineDate(dateStr: string | null): { month: string; day: string } | null {
		if (!dateStr) return null;
		const d = new Date(dateStr);
		return {
			month: d.toLocaleDateString('en-US', { month: 'short' }),
			day: String(d.getDate())
		};
	}

	function getTimelineDate(cycle: Cycle): { month: string; day: string } | null {
		if (cycle.status === 'completed' && cycle.completed_at) {
			return formatTimelineDate(cycle.completed_at);
		}
		return formatTimelineDate(cycle.start_date);
	}

	function toggleExpand(cycleId: string) {
		if (expandedCycleId === cycleId) {
			expandedCycleId = null;
		} else {
			expandedCycleId = cycleId;
		}
	}
</script>

<div class="flex h-full flex-col">
	<div class="flex h-[49px] items-center justify-between border-b border-[var(--app-border)] px-6">
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
					<RefreshCcwDot size={14} class="shrink-0" />
					Cycles
				</span>
			</nav>
		</div>
		<button
			onclick={() => (showCreate = true)}
			class="rounded-md p-1 text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]"
			title="New Cycle"
		>
			<Plus size={16} />
		</button>
	</div>

	<div class="flex-1 overflow-y-auto">
		{#if !loading && cycles.length === 0}
			<EmptyState
				title="No cycles yet"
				description="Create a cycle to plan your team's work in sprints"
				action={{ label: 'New Cycle', onclick: () => (showCreate = true) }}
			/>
		{:else if !loading}
			<div class="relative pl-16 pt-2">
				<!-- Vertical timeline line -->
				<div class="absolute left-[34px] top-0 bottom-0 w-px bg-[var(--app-border)]"></div>

				{#each visibleCycles as cycle (cycle.id)}
					{@const timelineDate = getTimelineDate(cycle)}
					{@const isExpanded = expandedCycleId === cycle.id}
					<div class="relative">
						<!-- Date label -->
						{#if timelineDate}
							<div class="absolute left-0 top-2.5 w-[26px] text-right text-[10px] leading-tight text-[var(--color-text-tertiary)]">
								<div>{timelineDate.month}</div>
								<div>{timelineDate.day}</div>
							</div>
						{/if}

						<!-- Timeline dot -->
						<div
							class="absolute top-3.5 rounded-full {cycle.status === 'active'
								? 'left-[30px] h-[9px] w-[9px] bg-[var(--app-accent)]'
								: 'left-[31px] h-[7px] w-[7px] bg-[var(--color-text-tertiary)]'}"
						></div>

						<!-- Cycle row -->
						<!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
						<div
							class="ml-6"
							onclick={(e) => {
								// Don't toggle if clicking the link itself
								if ((e.target as HTMLElement).closest('a')) return;
								toggleExpand(cycle.id);
							}}
						>
							<CycleTimelineRow {cycle} {slug} {teamId} />
						</div>

						<!-- Expanded burndown chart -->
						{#if isExpanded && cycle.start_date && cycle.end_date}
							<div transition:slideFade class="ml-12 mr-4 mb-2 rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)] p-4">
								{#if burndownLoading}
									<div class="flex h-[200px] items-center justify-center text-sm text-[var(--color-text-tertiary)]">
										Loading...
									</div>
								{:else if burndownData.length > 0}
									<CycleBurndownChart {cycle} data={burndownData} />
								{:else}
									<div class="flex h-[200px] items-center justify-center text-sm text-[var(--color-text-tertiary)]">
										No burndown data available
									</div>
								{/if}
							</div>
						{/if}
					</div>
				{/each}

				<!-- Archived cycles -->
				{#if archivedCycles.length > 0}
					<div class="relative py-3">
						<button
							onclick={() => archivedExpanded = !archivedExpanded}
							class="ml-6 flex items-center gap-2 rounded-md px-3 py-2 text-xs text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-secondary)]"
						>
							<Clock size={12} />
							{archivedCycles.length} older cycle{archivedCycles.length > 1 ? 's' : ''} (archived)
						</button>

						{#if archivedExpanded}
							<div transition:slideFade>
								{#each archivedCycles as cycle (cycle.id)}
									{@const timelineDate = getTimelineDate(cycle)}
									<div class="relative">
										{#if timelineDate}
											<div class="absolute left-0 top-2.5 w-[26px] text-right text-[10px] leading-tight text-[var(--color-text-tertiary)]">
												<div>{timelineDate.month}</div>
												<div>{timelineDate.day}</div>
											</div>
										{/if}
										<div class="absolute left-[31px] top-3.5 h-[7px] w-[7px] rounded-full bg-[var(--color-text-tertiary)]"></div>
										<div class="ml-6 opacity-60">
											<CycleTimelineRow {cycle} {slug} {teamId} />
										</div>
									</div>
								{/each}
							</div>
						{/if}
					</div>
				{/if}
			</div>
		{/if}
	</div>
</div>

<CreateCycleDialog bind:open={showCreate} onsubmit={handleCreate} />
