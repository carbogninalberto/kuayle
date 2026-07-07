<script lang="ts">
	import { page } from '$app/state';
	import {
		listCycles,
		createCycle,
		deleteCycle,
		completeCycle,
		updateCycle,
		getCycleBurndown,
		getCycleVelocity
	} from '$lib/api/cycles';
	import type { Cycle, CycleBurndownPoint, VelocityPoint } from '$lib/types/cycle';
	import CreateCycleDialog from '$lib/features/cycles/CreateCycleDialog.svelte';
	import EditCycleDialog from '$lib/features/cycles/EditCycleDialog.svelte';
	import CompleteCycleDialog from '$lib/features/cycles/CompleteCycleDialog.svelte';
	import CycleTimelineRow from '$lib/features/cycles/CycleTimelineRow.svelte';
	import CycleBurndownChart from '$lib/features/cycles/CycleBurndownChart.svelte';
	import CycleVelocityChart from '$lib/features/cycles/CycleVelocityChart.svelte';
	import EmptyState from '$lib/components/shared/EmptyState.svelte';
	import { appToast } from '$lib/features/toast/toast';
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
	let burndownData = $state<CycleBurndownPoint[]>([]);
	let burndownLoading = $state(false);
	let archivedExpanded = $state(false);
	let burndownVersion = $state(0);
	let velocityData = $state<VelocityPoint[]>([]);
	let velocityExpanded = $state(false);
	let showComplete = $state(false);
	let completingCycle = $state<Cycle | null>(null);

	const activeCycle = $derived(cycles.find((c) => c.status === 'active') ?? null);
	const nextNumber = $derived(cycles.length > 0 ? Math.max(...cycles.map((c) => c.number)) + 1 : 1);
	const nextUpcomingCycle = $derived(
		cycles
			.filter((c) => c.status === 'upcoming')
			.sort((a, b) => {
				const aD = a.start_date ? new Date(a.start_date).getTime() : Infinity;
				const bD = b.start_date ? new Date(b.start_date).getTime() : Infinity;
				return aD - bD;
			})[0] ?? null
	);
	const completingIncompleteCount = $derived.by(() => {
		if (!completingCycle?.progress) return 0;
		const { total, completed, cancelled } = completingCycle.progress;
		return total - completed - cancelled;
	});

	const MAX_VISIBLE_COMPLETED = 5;

	// Single flat sorted list: active first, then upcoming by start_date, then completed by completed_at desc
	const sortedCycles = $derived.by(() => {
		const active = cycles.filter((c) => c.status === 'active');
		const upcoming = cycles
			.filter((c) => c.status === 'upcoming')
			.sort((a, b) => {
				const aDate = a.start_date ? new Date(a.start_date).getTime() : Infinity;
				const bDate = b.start_date ? new Date(b.start_date).getTime() : Infinity;
				return aDate - bDate;
			});
		const completed = cycles
			.filter((c) => c.status === 'completed')
			.sort((a, b) => {
				const aDate = a.completed_at ? new Date(a.completed_at).getTime() : 0;
				const bDate = b.completed_at ? new Date(b.completed_at).getTime() : 0;
				return bDate - aDate;
			});
		return [...upcoming, ...active, ...completed];
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
		Promise.all([listCycles(s, t), getCycleVelocity(s, t).catch(() => [] as VelocityPoint[])])
			.then(([c, v]) => {
				cycles = c;
				velocityData = v ?? [];
				burndownVersion++;
			})
			.finally(() => {
				loading = false;
			});
	});

	// Fetch burndown for the active cycle
	$effect(() => {
		const _v = burndownVersion;
		const cycle = activeCycle;
		if (!cycle || !slug || !teamId || !cycle.start_date || !cycle.end_date) {
			burndownData = [];
			return;
		}
		burndownLoading = true;
		getCycleBurndown(slug, teamId, cycle.id)
			.then((d) => {
				burndownData = d;
			})
			.catch(() => {
				burndownData = [];
			})
			.finally(() => {
				burndownLoading = false;
			});
	});

	async function handleCreate(data: { name: string; description?: string; start_date: string; end_date: string }) {
		try {
			const cycle = await createCycle(slug, teamId, data);
			cycles = [cycle, ...cycles];
			appToast.success('Cycle created');
		} catch (err: any) {
			appToast.apiError(err, 'Failed to create cycle');
		}
	}

	function handleComplete(cycleId: string) {
		const cycle = cycles.find((c) => c.id === cycleId);
		if (!cycle) return;
		completingCycle = cycle;
		showComplete = true;
	}

	async function handleCompleteSubmit(data: { retrospective?: string; carry_over: boolean }) {
		if (!completingCycle) return;
		try {
			const result = await completeCycle(slug, teamId, completingCycle.id, {
				retrospective: data.retrospective,
				carry_over: data.carry_over
			});
			cycles = cycles.map((c) => (c.id === completingCycle!.id ? result.cycle : c));
			if (result.carried_over_count > 0) {
				appToast.success(
					`Cycle completed. ${result.carried_over_count} issue${result.carried_over_count > 1 ? 's' : ''} carried over.`
				);
			} else {
				appToast.success('Cycle completed');
			}
			burndownVersion++;
		} catch (err: any) {
			appToast.apiError(err, 'Failed to complete cycle');
		}
	}

	async function handleActivate(cycleId: string) {
		try {
			const updated = await updateCycle(slug, teamId, cycleId, { status: 'active' });
			cycles = cycles.map((c) => (c.id === cycleId ? updated : c));
			appToast.success('Cycle activated');
		} catch (err: any) {
			appToast.apiError(err, 'Failed to activate cycle');
		}
	}

	async function handleDelete(cycleId: string) {
		try {
			await deleteCycle(slug, teamId, cycleId);
			cycles = cycles.filter((c) => c.id !== cycleId);
			appToast.success('Cycle deleted');
		} catch (err: any) {
			appToast.apiError(err, 'Failed to delete cycle');
		}
	}

	let editingCycle = $state<Cycle | null>(null);
	let showEdit = $state(false);

	function handleEdit(cycle: Cycle) {
		editingCycle = cycle;
		showEdit = true;
	}

	async function handleEditSubmit(data: {
		name: string;
		description?: string;
		goals?: string;
		retrospective?: string;
		start_date?: string;
		end_date?: string;
	}) {
		if (!editingCycle) return;
		try {
			const updated = await updateCycle(slug, teamId, editingCycle.id, {
				name: data.name,
				description: data.description,
				goals: data.goals,
				retrospective: data.retrospective,
				start_date: data.start_date,
				end_date: data.end_date
			});
			cycles = cycles.map((c) => (c.id === updated.id ? updated : c));
			appToast.success('Cycle updated');
		} catch (err: any) {
			appToast.apiError(err, 'Failed to update cycle');
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

	function getCycleDates(cycle: Cycle): {
		start: { month: string; day: string } | null;
		end: { month: string; day: string } | null;
	} {
		return {
			start: formatTimelineDate(cycle.start_date),
			end: formatTimelineDate(cycle.end_date)
		};
	}

	function navigateToCycle(cycleId: string) {
		window.location.href = `/${slug}/teams/${teamId}/cycles/${cycleId}`;
	}
</script>

<div class="flex h-full min-w-0 flex-col">
	<div class="flex min-h-[49px] items-center justify-between gap-2 border-b border-[var(--app-border)] px-3 sm:px-6">
		<div class="flex min-w-0 items-center gap-3">
			<SidebarToggle />
			<nav class="flex min-w-0 items-center gap-1.5 text-sm">
				{#if sidebarState.getTeam(teamId)}
					<a
						href="/{slug}/teams/{teamId}"
						class="flex items-center gap-1.5 text-[var(--color-text-tertiary)] hover:text-[var(--color-text-secondary)]"
					>
						<SquareUser size={14} class="shrink-0" style="color: {sidebarState.getTeamColor(teamId)}" />
						<span class="hidden sm:inline truncate">{sidebarState.getTeam(teamId)?.name}</span>
					</a>
					<ChevronRight size={12} class="shrink-0 text-[var(--color-text-tertiary)]" />
				{/if}
				<span class="flex items-center gap-1.5 font-medium text-[var(--color-text-primary)]">
					<RefreshCcwDot size={14} class="shrink-0" />
					<span class="truncate">Cycles</span>
				</span>
			</nav>
		</div>
		<button
			onclick={() => (showCreate = true)}
			class="shrink-0 rounded-md p-1 text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-primary)]"
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
			<div class="pt-2">
				{#each visibleCycles as cycle, idx (cycle.id)}
					{@const dates = getCycleDates(cycle)}
					{@const isActive = cycle.status === 'active'}
					{@const isUpcoming = cycle.status === 'upcoming'}
					{@const lineColor = isActive ? 'bg-[var(--app-accent)]' : 'bg-[var(--app-border)]'}
					{@const prevCycle = idx > 0 ? visibleCycles[idx - 1] : null}
					{@const prevStartDate = prevCycle?.start_date?.slice(0, 10) ?? ''}
					{@const thisEndDate = cycle.end_date?.slice(0, 10) ?? ''}
					{@const collapseTop = prevStartDate !== '' && thisEndDate !== '' && prevStartDate === thisEndDate}
					{@const today = new Date().toISOString().slice(0, 10)}
					{@const startPassed = cycle.start_date ? cycle.start_date.slice(0, 10) <= today : false}
					{@const endPassed = cycle.end_date ? cycle.end_date.slice(0, 10) <= today : false}
					<div class="relative flex min-w-0">
						<!-- Timeline spine -->
						<div class="relative w-[60px] shrink-0 pl-3 sm:w-[76px] sm:pl-5">
							<!-- Continuous vertical line -->
							<div class="absolute right-[3.25px] {lineColor}" style="width: 1.5px; top: 0; bottom: 4px;"></div>
							<div class="relative flex h-full flex-col items-end">
								<!-- End date + dot (top = most recent) — hidden if collapsed with prev -->
								{#if dates.end && !collapseTop}
									<div class="relative z-10 flex w-full items-start gap-2">
										<div
											class="flex-1 text-right text-[11px] leading-tight text-[var(--color-text-tertiary)] opacity-50"
										>
											<div>{dates.end.month}</div>
											<div class="pl-1">{dates.end.day}</div>
										</div>
										{#if endPassed}
											<div
												class="mt-0.5 h-2 w-2 shrink-0 rounded-full {isActive
													? 'bg-[var(--app-accent)]'
													: 'bg-[var(--color-text-tertiary)] opacity-60'}"
											></div>
										{:else}
											<div
												class="mt-0.5 h-2 w-2 shrink-0 rounded-full border-2 {isActive
													? 'border-[var(--app-accent)]'
													: 'border-[var(--color-text-tertiary)]'} bg-[var(--color-bg)] {isActive ? '' : 'opacity-60'}"
											></div>
										{/if}
									</div>
								{/if}
								<div class="flex-1 min-h-3"></div>
								<!-- Start date + dot (bottom = oldest) -->
								{#if dates.start}
									<div class="relative z-10 flex w-full items-end gap-2">
										<div
											class="flex-1 text-right text-[11px] leading-tight text-[var(--color-text-tertiary)] opacity-50"
										>
											<div>{dates.start.month}</div>
											<div class="pl-1">{dates.start.day}</div>
										</div>
										{#if startPassed}
											<div
												class="mb-0.5 h-2 w-2 shrink-0 rounded-full {isActive
													? 'bg-[var(--app-accent)]'
													: 'bg-[var(--color-text-tertiary)] opacity-60'}"
											></div>
										{:else}
											<div
												class="mb-0.5 h-2 w-2 shrink-0 rounded-full border-2 {isActive
													? 'border-[var(--app-accent)]'
													: 'border-[var(--color-text-tertiary)]'} bg-[var(--color-bg)] {isActive ? '' : 'opacity-60'}"
											></div>
										{/if}
									</div>
								{/if}
							</div>
						</div>

						<!-- Cycle content (full row clickable + hover) -->
						<div
							role="button"
							tabindex="0"
							class="group my-2 mr-2 min-w-0 flex-1 cursor-pointer rounded-md hover:bg-[var(--color-bg-hover)]/30"
							onclick={() => navigateToCycle(cycle.id)}
							onkeydown={(e) => {
								if (e.key === 'Enter' || e.key === ' ') {
									e.preventDefault();
									navigateToCycle(cycle.id);
								}
							}}
						>
							<CycleTimelineRow
								{cycle}
								{slug}
								{teamId}
								clickable={false}
								onedit={handleEdit}
								onactivate={handleActivate}
								oncomplete={handleComplete}
								ondelete={handleDelete}
							/>

							<!-- Chart shown only for active cycle -->
							{#if isActive && cycle.start_date && cycle.end_date}
								<div class="hidden px-3 pb-3 sm:block">
									{#if burndownLoading}
										<div
											class="flex h-[200px] items-center justify-center text-sm text-[var(--color-text-tertiary)]"
										></div>
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
					</div>
				{/each}

				<!-- Archived cycles -->
				{#if archivedCycles.length > 0}
					<div class="flex min-w-0">
						<div class="relative w-[60px] shrink-0 pl-3 sm:w-[76px] sm:pl-5">
							<div class="absolute top-0 bottom-0 right-[3.25px] bg-[var(--app-border)]" style="width: 1.5px;"></div>
						</div>
						<div class="min-w-0 flex-1 py-1">
							<button
								onclick={() => (archivedExpanded = !archivedExpanded)}
								class="flex items-center gap-2 rounded-md px-3 py-2 text-xs text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-secondary)]"
							>
								<Clock size={12} />
								{archivedCycles.length} older cycle{archivedCycles.length > 1 ? 's' : ''} (archived)
							</button>

							{#if archivedExpanded}
								<div transition:slideFade>
									{#each archivedCycles as cycle (cycle.id)}
										{@const dates = getCycleDates(cycle)}
										<div class="relative flex min-w-0">
											<div class="relative w-[60px] shrink-0 pl-3 sm:w-[76px] sm:pl-5">
												<div
													class="absolute top-0 bottom-0 right-[3.25px] bg-[var(--app-border)]"
													style="width: 1.5px;"
												></div>
												<div class="relative flex h-full flex-col items-end justify-center py-3">
													{#if dates.start}
														<div class="flex w-full items-center gap-2">
															<div
																class="flex-1 text-right text-[11px] leading-tight text-[var(--color-text-tertiary)] opacity-50"
															>
																<div>{dates.start.month}</div>
																<div class="pl-1">{dates.start.day}</div>
															</div>
															<div
																class="h-2 w-2 shrink-0 rounded-full bg-[var(--color-text-tertiary)] opacity-50"
															></div>
														</div>
													{/if}
												</div>
											</div>
											<div class="min-w-0 flex-1 opacity-60">
												<CycleTimelineRow {cycle} {slug} {teamId} />
											</div>
										</div>
									{/each}
								</div>
							{/if}
						</div>
					</div>
				{/if}
			</div>

			<!-- Velocity chart -->
			{#if velocityData.length > 0}
				<div class="mt-4 px-3 pb-4 sm:px-6">
					<button
						onclick={() => (velocityExpanded = !velocityExpanded)}
						class="flex items-center gap-2 text-xs font-medium text-[var(--color-text-tertiary)] hover:text-[var(--color-text-secondary)]"
					>
						<ChevronRight size={12} class="transition-transform {velocityExpanded ? 'rotate-90' : ''}" />
						Velocity ({velocityData.length} completed cycle{velocityData.length > 1 ? 's' : ''})
					</button>
					{#if velocityExpanded}
						<div
							class="mt-2 rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)] p-3"
							transition:slideFade
						>
							<CycleVelocityChart data={velocityData} />
						</div>
					{/if}
				</div>
			{/if}
		{/if}
	</div>
</div>

<CreateCycleDialog bind:open={showCreate} {cycles} {nextNumber} onsubmit={handleCreate} />
<EditCycleDialog bind:open={showEdit} cycle={editingCycle} {cycles} onsubmit={handleEditSubmit} />
<CompleteCycleDialog
	bind:open={showComplete}
	cycle={completingCycle}
	incompleteCount={completingIncompleteCount}
	{nextUpcomingCycle}
	onsubmit={handleCompleteSubmit}
/>
