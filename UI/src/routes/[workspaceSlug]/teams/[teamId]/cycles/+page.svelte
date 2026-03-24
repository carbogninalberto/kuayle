<script lang="ts">
	import { page } from '$app/state';
	import { listCycles, createCycle, deleteCycle, completeCycle, updateCycle, getCycleBurndown } from '$lib/api/cycles';
	import type { Cycle, CycleBurndownPoint } from '$lib/types/cycle';
	import CreateCycleDialog from '$lib/features/cycles/CreateCycleDialog.svelte';
	import EditCycleDialog from '$lib/features/cycles/EditCycleDialog.svelte';
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
	let burndownData = $state<CycleBurndownPoint[]>([]);
	let burndownLoading = $state(false);
	let archivedExpanded = $state(false);
	let burndownVersion = $state(0);

	const activeCycle = $derived(cycles.find(c => c.status === 'active') ?? null);

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
		listCycles(s, t).then((c) => {
			cycles = c;
			burndownVersion++;
		}).finally(() => {
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
		getCycleBurndown(slug, teamId, cycle.id).then((d) => {
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

	async function handleActivate(cycleId: string) {
		try {
			const updated = await updateCycle(slug, teamId, cycleId, { status: 'active' });
			cycles = cycles.map((c) => (c.id === cycleId ? updated : c));
			toast.success('Cycle activated');
		} catch (err: any) {
			toast.error(err?.error?.message || 'Failed to activate cycle');
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

	let editingCycle = $state<Cycle | null>(null);
	let showEdit = $state(false);

	function handleEdit(cycle: Cycle) {
		editingCycle = cycle;
		showEdit = true;
	}

	async function handleEditSubmit(data: { name: string; description?: string; start_date?: string; end_date?: string }) {
		if (!editingCycle) return;
		try {
			const updated = await updateCycle(slug, teamId, editingCycle.id, {
				name: data.name,
				description: data.description,
				start_date: data.start_date,
				end_date: data.end_date
			});
			cycles = cycles.map((c) => (c.id === updated.id ? updated : c));
			toast.success('Cycle updated');
		} catch (err: any) {
			toast.error(err?.error?.message || 'Failed to update cycle');
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

	function getCycleDates(cycle: Cycle): { start: { month: string; day: string } | null; end: { month: string; day: string } | null } {
		return {
			start: formatTimelineDate(cycle.start_date),
			end: formatTimelineDate(cycle.end_date)
		};
	}

	function navigateToCycle(cycleId: string) {
		window.location.href = `/${slug}/teams/${teamId}/cycles/${cycleId}`;
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
			<div class="pt-2">
				{#each visibleCycles as cycle (cycle.id)}
					{@const dates = getCycleDates(cycle)}
					{@const isActive = cycle.status === 'active'}
					{@const isUpcoming = cycle.status === 'upcoming'}
					{@const lineColor = isActive ? 'bg-[var(--app-accent)]' : 'bg-[var(--app-border)]'}
					<div class="relative flex">
						<!-- Timeline spine -->
						<div class="relative shrink-0 pl-5" style="width: 76px;">
							<!-- Continuous vertical line -->
							<div class="absolute top-0 bottom-0 right-[3.25px] {lineColor}" style="width: 1.5px;"></div>
							<!-- Start date + dot (top, aligned to cycle name) -->
							<div class="relative flex h-full flex-col items-end">
								{#if dates.start}
									<div class="flex w-full items-start gap-2">
										<div class="flex-1 text-right text-[11px] leading-tight text-[var(--color-text-tertiary)] opacity-50">
											<div>{dates.start.month}</div>
											<div class="pl-1">{dates.start.day}</div>
										</div>
										<div class="mt-0.5 h-2 w-2 shrink-0 rounded-full {isActive ? 'bg-[var(--app-accent)]' : 'bg-[var(--color-text-tertiary)]'} opacity-60"></div>
									</div>
								{/if}
								<div class="flex-1"></div>
								<!-- End date + dot (bottom) -->
								{#if dates.end && isActive}
									<div class="flex w-full items-end gap-2">
										<div class="flex-1 text-right text-[11px] leading-tight text-[var(--color-text-tertiary)] opacity-50">
											<div>{dates.end.month}</div>
											<div class="pl-1">{dates.end.day}</div>
										</div>
										<div class="mb-0.5 h-2 w-2 shrink-0 rounded-full border-[1.5px] border-[var(--color-text-tertiary)] bg-[var(--color-bg)] opacity-60"></div>
									</div>
								{/if}
							</div>
						</div>

						<!-- Cycle content (full row clickable + hover) -->
						<!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
						<div
							class="group my-2 mr-2 min-w-0 flex-1 cursor-pointer rounded-md hover:bg-[var(--color-bg-hover)]/30"
							onclick={() => navigateToCycle(cycle.id)}
						>
							<CycleTimelineRow
								{cycle} {slug} {teamId} clickable={false}
								onedit={handleEdit}
								onactivate={handleActivate}
								oncomplete={handleComplete}
								ondelete={handleDelete}
							/>

							<!-- Chart shown only for active cycle -->
							{#if isActive && cycle.start_date && cycle.end_date}
								<div class="px-3 pb-3">
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
					</div>
				{/each}

				<!-- Archived cycles -->
				{#if archivedCycles.length > 0}
					<div class="flex">
						<div class="relative shrink-0 pl-5" style="width: 76px;">
							<div class="absolute top-0 bottom-0 right-[3.25px] bg-[var(--app-border)]" style="width: 1.5px;"></div>
						</div>
						<div class="min-w-0 flex-1 py-1">
							<button
								onclick={() => archivedExpanded = !archivedExpanded}
								class="flex items-center gap-2 rounded-md px-3 py-2 text-xs text-[var(--color-text-tertiary)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-text-secondary)]"
							>
								<Clock size={12} />
								{archivedCycles.length} older cycle{archivedCycles.length > 1 ? 's' : ''} (archived)
							</button>

							{#if archivedExpanded}
								<div transition:slideFade>
									{#each archivedCycles as cycle (cycle.id)}
										{@const dates = getCycleDates(cycle)}
										<div class="relative flex">
											<div class="relative shrink-0 pl-5" style="width: 76px;">
												<div class="absolute top-0 bottom-0 right-[3.25px] bg-[var(--app-border)]" style="width: 1.5px;"></div>
												<div class="relative flex h-full flex-col items-end justify-center py-3">
													{#if dates.start}
														<div class="flex w-full items-center gap-2">
															<div class="flex-1 text-right text-[11px] leading-tight text-[var(--color-text-tertiary)] opacity-50">
																<div>{dates.start.month}</div>
																<div class="pl-1">{dates.start.day}</div>
															</div>
															<div class="h-2 w-2 shrink-0 rounded-full bg-[var(--color-text-tertiary)] opacity-50"></div>
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
		{/if}
	</div>
</div>

<CreateCycleDialog bind:open={showCreate} onsubmit={handleCreate} />
<EditCycleDialog bind:open={showEdit} cycle={editingCycle} onsubmit={handleEditSubmit} />
