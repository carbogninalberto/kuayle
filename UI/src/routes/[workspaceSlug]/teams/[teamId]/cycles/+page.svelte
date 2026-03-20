<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { listCycles, createCycle, deleteCycle, completeCycle, updateCycle } from '$lib/api/cycles';
	import type { Cycle } from '$lib/types/cycle';
	import CreateCycleDialog from '$lib/features/cycles/CreateCycleDialog.svelte';
	import CycleProgress from '$lib/features/cycles/CycleProgress.svelte';
	import DatePickerPopover from '$lib/components/shared/DatePickerPopover.svelte';
	import EmptyState from '$lib/components/shared/EmptyState.svelte';
	import LoadingState from '$lib/components/shared/LoadingState.svelte';
	import { Badge } from '$lib/components/ui/badge';
	import { Button } from '$lib/components/ui/button';
	import { toast } from 'svelte-sonner';
	import { formatRelativeTime } from '$lib/utils/format';
	import { Plus, Play, CheckCircle2, Clock, Trash2 } from 'lucide-svelte';

	const slug = $derived(page.params.workspaceSlug ?? '');
	const teamId = $derived(page.params.teamId ?? '');

	let cycles = $state<Cycle[]>([]);
	let loading = $state(true);
	let showCreate = $state(false);

	let activeCycles = $derived(cycles.filter((c) => c.status === 'active'));
	let upcomingCycles = $derived(cycles.filter((c) => c.status === 'upcoming'));
	let completedCycles = $derived(cycles.filter((c) => c.status === 'completed'));

	onMount(async () => {
		try {
			cycles = await listCycles(slug, teamId);
		} finally {
			loading = false;
		}
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

	async function handleDateChange(cycleId: string, field: 'start_date' | 'end_date', date: string | null) {
		try {
			const updated = await updateCycle(slug, teamId, cycleId, { [field]: date ?? undefined });
			cycles = cycles.map((c) => (c.id === cycleId ? updated : c));
			toast.success(`${field === 'start_date' ? 'Start' : 'End'} date updated`);
		} catch (err: any) {
			toast.error(err?.error?.message || 'Failed to update date');
		}
	}

	function statusBadgeVariant(status: string): 'default' | 'secondary' | 'outline' {
		switch (status) {
			case 'active': return 'default';
			case 'completed': return 'secondary';
			default: return 'outline';
		}
	}
</script>

<div class="flex h-full flex-col">
	<div class="flex h-[49px] items-center justify-between border-b border-[var(--app-border)] px-6">
		<div class="flex items-center gap-3">
			<h1 class="text-sm font-medium text-[var(--color-text-primary)]">Cycles</h1>
			<a
				href="/{slug}/teams/{teamId}"
				class="text-xs text-[var(--color-text-tertiary)] hover:text-[var(--color-text-secondary)]"
			>
				Issues
			</a>
		</div>
		<button
			onclick={() => (showCreate = true)}
			class="flex items-center gap-1 rounded-md bg-[var(--app-accent)] px-3 py-1.5 text-sm text-white hover:bg-[var(--app-accent-hover)]"
		>
			<Plus size={14} />
			New Cycle
		</button>
	</div>

	<div class="flex-1 overflow-y-auto">
		{#if loading}
			<LoadingState />
		{:else if cycles.length === 0}
			<EmptyState
				title="No cycles yet"
				description="Create a cycle to plan your team's work in sprints"
				action={{ label: 'New Cycle', onclick: () => (showCreate = true) }}
			/>
		{:else}
			<!-- Active cycles -->
			{#if activeCycles.length > 0}
				<div class="px-6 pt-4">
					<h2 class="flex items-center gap-2 text-xs font-medium uppercase text-[var(--color-text-tertiary)]">
						<Play size={12} />
						Active
					</h2>
				</div>
				{#each activeCycles as cycle}
					<a
						href="/{slug}/teams/{teamId}/cycles/{cycle.id}"
						class="flex items-center gap-4 border-b border-[var(--app-border)] px-6 py-3 hover:bg-[var(--color-bg-hover)]"
					>
						<div class="flex-1 min-w-0">
							<div class="flex items-center gap-2">
								<span class="text-sm font-medium text-[var(--color-text-primary)]">{cycle.name}</span>
								<Badge variant={statusBadgeVariant(cycle.status)} class="text-[10px]">{cycle.status}</Badge>
							</div>
							<div class="mt-1 flex items-center gap-3 text-xs text-[var(--color-text-tertiary)]">
								<!-- svelte-ignore a11y_click_events_have_key_events -->
								<!-- svelte-ignore a11y_no_static_element_interactions -->
								<span onclick={(e: MouseEvent) => { e.preventDefault(); e.stopPropagation(); }}>
									<DatePickerPopover
										value={cycle.start_date}
										onchange={(d) => handleDateChange(cycle.id, 'start_date', d)}
										placeholder="Start date"
									/>
								</span>
								<span>&#8594;</span>
								<!-- svelte-ignore a11y_click_events_have_key_events -->
								<!-- svelte-ignore a11y_no_static_element_interactions -->
								<span onclick={(e: MouseEvent) => { e.preventDefault(); e.stopPropagation(); }}>
									<DatePickerPopover
										value={cycle.end_date}
										onchange={(d) => handleDateChange(cycle.id, 'end_date', d)}
										placeholder="End date"
									/>
								</span>
								{#if cycle.progress}
									<span>{cycle.progress.completed}/{cycle.progress.total} done</span>
								{/if}
							</div>
						</div>
						{#if cycle.progress}
							<div class="w-32">
								<CycleProgress progress={cycle.progress} />
							</div>
						{/if}
						<Button
							variant="ghost"
							size="sm"
							onclick={(e) => { e.preventDefault(); e.stopPropagation(); handleComplete(cycle.id); }}
						>
							<CheckCircle2 size={14} />
							<span class="ml-1">Complete</span>
						</Button>
					</a>
				{/each}
			{/if}

			<!-- Upcoming cycles -->
			{#if upcomingCycles.length > 0}
				<div class="px-6 pt-4">
					<h2 class="flex items-center gap-2 text-xs font-medium uppercase text-[var(--color-text-tertiary)]">
						<Clock size={12} />
						Upcoming
					</h2>
				</div>
				{#each upcomingCycles as cycle}
					<a
						href="/{slug}/teams/{teamId}/cycles/{cycle.id}"
						class="flex items-center gap-4 border-b border-[var(--app-border)] px-6 py-3 hover:bg-[var(--color-bg-hover)]"
					>
						<div class="flex-1 min-w-0">
							<div class="flex items-center gap-2">
								<span class="text-sm font-medium text-[var(--color-text-primary)]">{cycle.name}</span>
								<Badge variant={statusBadgeVariant(cycle.status)} class="text-[10px]">{cycle.status}</Badge>
							</div>
							<div class="mt-1 flex items-center gap-2 text-xs text-[var(--color-text-tertiary)]">
								<!-- svelte-ignore a11y_click_events_have_key_events -->
								<!-- svelte-ignore a11y_no_static_element_interactions -->
								<span onclick={(e: MouseEvent) => { e.preventDefault(); e.stopPropagation(); }}>
									<DatePickerPopover
										value={cycle.start_date}
										onchange={(d) => handleDateChange(cycle.id, 'start_date', d)}
										placeholder="Start date"
									/>
								</span>
								<span>&#8594;</span>
								<!-- svelte-ignore a11y_click_events_have_key_events -->
								<!-- svelte-ignore a11y_no_static_element_interactions -->
								<span onclick={(e: MouseEvent) => { e.preventDefault(); e.stopPropagation(); }}>
									<DatePickerPopover
										value={cycle.end_date}
										onchange={(d) => handleDateChange(cycle.id, 'end_date', d)}
										placeholder="End date"
									/>
								</span>
							</div>
						</div>
						<Button
							variant="ghost"
							size="icon-sm"
							onclick={(e) => { e.preventDefault(); e.stopPropagation(); handleDelete(cycle.id); }}
							class="text-[var(--color-text-tertiary)] hover:text-[var(--color-error)]"
						>
							<Trash2 size={14} />
						</Button>
					</a>
				{/each}
			{/if}

			<!-- Completed cycles -->
			{#if completedCycles.length > 0}
				<div class="px-6 pt-4">
					<h2 class="flex items-center gap-2 text-xs font-medium uppercase text-[var(--color-text-tertiary)]">
						<CheckCircle2 size={12} />
						Completed
					</h2>
				</div>
				{#each completedCycles as cycle}
					<a
						href="/{slug}/teams/{teamId}/cycles/{cycle.id}"
						class="flex items-center gap-4 border-b border-[var(--app-border)] px-6 py-3 opacity-60 hover:bg-[var(--color-bg-hover)]"
					>
						<div class="flex-1 min-w-0">
							<div class="flex items-center gap-2">
								<span class="text-sm font-medium text-[var(--color-text-primary)]">{cycle.name}</span>
								<Badge variant={statusBadgeVariant(cycle.status)} class="text-[10px]">{cycle.status}</Badge>
							</div>
							<div class="mt-1 text-xs text-[var(--color-text-tertiary)]">
								<!-- svelte-ignore a11y_click_events_have_key_events -->
								<!-- svelte-ignore a11y_no_static_element_interactions -->
								<span onclick={(e: MouseEvent) => { e.preventDefault(); e.stopPropagation(); }}>
									<DatePickerPopover
										value={cycle.start_date}
										onchange={(d) => handleDateChange(cycle.id, 'start_date', d)}
										placeholder="Start date"
									/>
								</span>
								<span>&#8594;</span>
								<!-- svelte-ignore a11y_click_events_have_key_events -->
								<!-- svelte-ignore a11y_no_static_element_interactions -->
								<span onclick={(e: MouseEvent) => { e.preventDefault(); e.stopPropagation(); }}>
									<DatePickerPopover
										value={cycle.end_date}
										onchange={(d) => handleDateChange(cycle.id, 'end_date', d)}
										placeholder="End date"
									/>
								</span>
								{#if cycle.completed_at}
									· Completed {formatRelativeTime(cycle.completed_at)}
								{/if}
							</div>
						</div>
						{#if cycle.progress}
							<div class="w-32">
								<CycleProgress progress={cycle.progress} />
							</div>
						{/if}
					</a>
				{/each}
			{/if}
		{/if}
	</div>
</div>

<CreateCycleDialog bind:open={showCreate} onsubmit={handleCreate} />
