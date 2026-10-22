<script lang="ts">
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { getCycle, completeCycle, updateCycle, deleteCycle, listCycles } from '$lib/api/cycles';
	import { getWorkspace } from '$lib/api/workspaces';
	import type { Cycle, UpdateCycleRequest } from '$lib/types/cycle';
	import CompleteCycleDialog from '$lib/features/cycles/CompleteCycleDialog.svelte';
	import EditCycleDialog from '$lib/features/cycles/EditCycleDialog.svelte';
	import ScopedIssueList from '$lib/features/issues/ScopedIssueList.svelte';
	import ScopeDeleteDialog from '$lib/features/issues/ScopeDeleteDialog.svelte';
	import CycleProgress from '$lib/features/cycles/CycleProgress.svelte';
	import DateRangePickerPopover from '$lib/components/shared/DateRangePickerPopover.svelte';
	import { Badge } from '$lib/components/ui/badge';
	import { Button } from '$lib/components/ui/button';
	import * as Popover from '$lib/components/ui/popover';
	import { appToast } from '$lib/features/toast/toast';
	import { m } from '$lib/paraglide/messages.js';
	import { getLocale } from '$lib/paraglide/runtime.js';
	import { formatRelativeTime } from '$lib/utils/format';
	import {
		CheckCircle2,
		Play,
		Trash2,
		MoreHorizontal,
		Pencil,
		SquareUser,
		RefreshCcwDot,
		ChevronRight
	} from '@lucide/svelte';
	import SidebarToggle from '$lib/components/layout/SidebarToggle.svelte';
	import { sidebarState } from '$lib/features/layout/sidebar.state.svelte';
	import { hasPermission } from '$lib/security/permissions';
	import type { Role } from '$lib/security/roles';

	const slug = $derived(page.params.workspaceSlug ?? '');
	const teamId = $derived(page.params.teamId ?? '');
	const cycleId = $derived(page.params.cycleId ?? '');
	let cycle = $state<Cycle | null>(null);
	let allCycles = $state<Cycle[]>([]);
	let loading = $state(true);
	let actionsOpen = $state(false);
	let showComplete = $state(false);
	let showEdit = $state(false);
	let showDelete = $state(false);
	let workspaceRole = $state<Role>('guest');
	let refreshVersion = $state(0);
	let requestVersion = 0;
	const canManageCycles = $derived(hasPermission(workspaceRole, 'cycle:manage'));
	const nextUpcomingCycle = $derived(
		allCycles
			.filter((c) => c.status === 'upcoming')
			.sort((a, b) => (a.start_date ?? '9999').localeCompare(b.start_date ?? '9999'))[0] ?? null
	);
	const incompleteCount = $derived(
		cycle?.progress ? cycle.progress.total - cycle.progress.completed - cycle.progress.cancelled : 0
	);

	$effect(() => {
		const s = slug,
			t = teamId,
			id = cycleId,
			version = ++requestVersion;
		loading = true;
		cycle = null;
		showEdit = false;
		showDelete = false;
		showComplete = false;
		Promise.all([getCycle(s, t, id), listCycles(s, t), getWorkspace(s)])
			.then(([c, cycles, workspace]) => {
				if (version !== requestVersion) return;
				cycle = c;
				allCycles = cycles;
				workspaceRole = workspace.current_user_role as Role;
			})
			.catch((error) => {
				if (version !== requestVersion) return;
				appToast.apiError(error, m['cycles.toast.not_found']());
				void goto(`/${s}/teams/${t}/cycles`);
			})
			.finally(() => {
				if (version === requestVersion) loading = false;
			});
		return () => {
			requestVersion++;
		};
	});
	async function refreshCycle() {
		const s = slug,
			t = teamId,
			id = cycleId,
			version = requestVersion;
		try {
			const current = await getCycle(s, t, id);
			if (version === requestVersion) cycle = current;
		} catch (error) {
			if (version === requestVersion) appToast.apiError(error, m['cycles.toast.not_found']());
		}
	}
	function handleComplete() {
		if (canManageCycles) showComplete = true;
	}
	async function handleCompleteSubmit(data: { retrospective?: string; carry_over: boolean }) {
		if (!cycle || !canManageCycles) return;
		try {
			await completeCycle(slug, teamId, cycle.id, data);
			await refreshCycle();
			refreshVersion++;
			appToast.success(m['cycles.toast.completed']());
		} catch (error) {
			appToast.apiError(error, m['cycles.toast.failed_complete']());
		}
	}
	async function handleEdit(
		data: UpdateCycleRequest,
		successMessage = m['cycles.toast.updated'](),
		failureMessage = m['cycles.toast.failed_update']()
	) {
		if (!cycle || !canManageCycles) return;
		try {
			await updateCycle(slug, teamId, cycle.id, data);
			await refreshCycle();
			appToast.success(successMessage);
		} catch (error) {
			appToast.apiError(error, failureMessage);
		}
	}
	async function handleActivate() {
		await handleEdit({ status: 'active' }, m['cycles.toast.activated'](), m['cycles.toast.failed_activate']());
	}
	async function handleDateRangeChange(start: string, end: string) {
		await handleEdit(
			{ start_date: start, end_date: end },
			m['cycles.toast.dates_updated'](),
			m['cycles.toast.failed_update_dates']()
		);
	}
	async function handleDelete() {
		if (!cycle || !canManageCycles) return;
		try {
			await deleteCycle(slug, teamId, cycle.id);
			showDelete = false;
			appToast.success(m['cycles.toast.deleted']());
			void goto(`/${slug}/teams/${teamId}/cycles`);
		} catch (error) {
			appToast.apiError(error, m['cycles.toast.failed_delete']());
		}
	}
</script>

<div class="flex h-full min-w-0 flex-col">
	{#if !loading && cycle}
		<!-- Header -->
		<div class="flex min-h-[49px] items-center justify-between gap-2 border-b border-[var(--app-border)] px-3 sm:px-4">
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
						<a
							href="/{slug}/teams/{teamId}/cycles"
							class="flex items-center gap-1.5 text-[var(--color-text-tertiary)] hover:text-[var(--color-text-secondary)]"
						>
							<RefreshCcwDot size={14} class="shrink-0" />
							<span class="hidden sm:inline">{m['cycles.title']()}</span>
						</a>
						<ChevronRight size={12} class="shrink-0 text-[var(--color-text-tertiary)]" />
					{/if}
					<span class="truncate font-medium text-[var(--color-text-primary)]">{cycle.name}</span>
				</nav>
				<Badge
					variant={cycle.status === 'active' ? 'default' : cycle.status === 'completed' ? 'secondary' : 'outline'}
					class="shrink-0 text-[10px]"
				>
					{cycle.status === 'active'
						? m['cycles.status.current']()
						: cycle.status === 'completed'
							? m['cycles.status.completed']()
							: m['cycles.status.upcoming']()}
				</Badge>
			</div>
			{#if canManageCycles}
				<div class="flex shrink-0 items-center gap-2">
					{#if cycle.status === 'upcoming'}
						<Button size="sm" onclick={handleActivate}>
							<Play size={14} class="mr-1" />
							{m['cycles.start_cycle']()}
						</Button>
					{/if}
					{#if cycle.status === 'active'}
						<Button size="sm" onclick={handleComplete}>
							<CheckCircle2 size={14} class="mr-1" />
							{m['cycles.complete']()}
						</Button>
					{/if}
					<Popover.Root bind:open={actionsOpen}>
						<Popover.Trigger
							aria-label={m['cycles.actions']({ name: cycle.name })}
							class="rounded-md p-1.5 hover:bg-[var(--color-bg-hover)]"><MoreHorizontal size={14} /></Popover.Trigger
						>
						<Popover.Content class="w-40 p-1" align="end">
							<Button variant="plain" class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm hover:bg-[var(--color-bg-hover)]"
								onclick={() => {
									actionsOpen = false;
									showEdit = true;
								}}><Pencil size={14} />{m['cycles.edit_cycle']()}</Button
							>
							<Button variant="plain" onclick={() => {
									actionsOpen = false;
									showDelete = true;
								}}
								class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-[var(--color-error)] hover:bg-[var(--color-bg-hover)]"
							>
								<Trash2 size={14} />
								{m['cycles.delete_cycle']()}
							</Button>
						</Popover.Content>
					</Popover.Root>
				</div>
			{/if}
		</div>

		<!-- Cycle info -->
		<div class="border-b border-[var(--app-border)] px-3 py-3 sm:px-4 sm:py-4">
			<div class="flex flex-wrap items-center gap-3 sm:gap-6">
				<div class="flex items-center gap-2 text-xs text-[var(--color-text-tertiary)]">
					{#if canManageCycles}
						<DateRangePickerPopover
							startDate={cycle.start_date}
							endDate={cycle.end_date}
							onchange={handleDateRangeChange}
							placeholder={m['cycles.select_dates']()}
						/>
					{:else if cycle.start_date || cycle.end_date}
						<span>{cycle.start_date?.slice(0, 10) ?? ''} – {cycle.end_date?.slice(0, 10) ?? ''}</span>
					{/if}
				</div>
				{#if cycle.progress}
					<div class="flex items-center gap-2 text-xs text-[var(--color-text-tertiary)]">
						<span>{m['cycles.issues_done']({ completed: cycle.progress.completed, total: cycle.progress.total })}</span>
					</div>
				{/if}
				{#if cycle.completed_at}
					<div class="text-xs text-[var(--color-text-tertiary)]">
						{m['cycles.completed_at']({ time: formatRelativeTime(cycle.completed_at, getLocale()) })}
					</div>
				{/if}
			</div>
			{#if cycle.description}
				<p class="mt-2 text-sm text-[var(--color-text-secondary)]">{cycle.description}</p>
			{/if}
			{#if cycle.goals}
				<div class="mt-2">
					<span class="text-xs font-medium text-[var(--color-text-tertiary)]">{m['cycles.goals']()}</span>
					<p class="mt-0.5 text-sm text-[var(--color-text-secondary)]">{cycle.goals}</p>
				</div>
			{/if}
			{#if cycle.retrospective}
				<div class="mt-2">
					<span class="text-xs font-medium text-[var(--color-text-tertiary)]">{m['cycles.retrospective']()}</span>
					<p class="mt-0.5 text-sm text-[var(--color-text-secondary)]">{cycle.retrospective}</p>
				</div>
			{/if}
			{#if cycle.progress && cycle.progress.total > 0}
				<div class="mt-3 w-full sm:w-64">
					<CycleProgress progress={cycle.progress} />
				</div>
			{/if}
		</div>

		{#key `${slug}/${teamId}/${cycleId}`}
			<ScopedIssueList
				{slug}
				scope={{ kind: 'cycle', id: cycleId, teamId }}
				role={workspaceRole}
				cycleOptions={allCycles}
				{refreshVersion}
				onchanged={refreshCycle}
			/>
		{/key}
	{:else if loading}
		<div role="status" class="animate-pulse p-4 text-sm text-[var(--color-text-tertiary)]">{m['common.loading']()}</div>
	{/if}
</div>
{#if canManageCycles}
	<CompleteCycleDialog
		bind:open={showComplete}
		{cycle}
		{incompleteCount}
		{nextUpcomingCycle}
		onsubmit={handleCompleteSubmit}
	/>
	<EditCycleDialog bind:open={showEdit} {cycle} cycles={allCycles} onsubmit={handleEdit} />
	{#if showDelete}<ScopeDeleteDialog
			title={m['cycles.delete_cycle']()}
			message={m['cycles.delete_confirm']()}
			oncancel={() => (showDelete = false)}
			onconfirm={handleDelete}
		/>{/if}
{/if}
