<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { Plus, ChevronLeft, ChevronRight, Trash2 } from '@lucide/svelte';
	import * as AlertDialog from '$lib/components/ui/alert-dialog';
	import * as Table from '$lib/components/ui/table';
	import { bulkPermanentDeleteDevMachines, listDevMachines } from '$lib/api/dev-machines';
	import { getWorkspace } from '$lib/api/workspaces';
	import type { DevMachine } from '$lib/types/dev-machine';
	import EmptyState from '$lib/components/shared/EmptyState.svelte';
	import LoadingState from '$lib/components/shared/LoadingState.svelte';
	import ErrorState from '$lib/components/shared/ErrorState.svelte';
	import CreateMachineDialog from '$lib/features/dev-machines/CreateMachineDialog.svelte';
	import MachineStatusBadge from '$lib/features/dev-machines/MachineStatusBadge.svelte';
	import { appToast } from '$lib/features/toast/toast';
	import { Button } from '$lib/components/ui/button';
	import { m } from '$lib/paraglide/messages.js';

	// The user's Dev Machines with manual creation and cleanup; shown in
	// Settings → Dev Machines. Task cards and issues link to each machine page.
	let { slug, issueId = '' }: { slug: string; issueId?: string } = $props();
	let machines = $state<DevMachine[]>([]);
	let totalCount = $state(0);
	let hasMore = $state(false);
	let currentPage = $state(1);
	let loading = $state(true);
	let failed = $state(false);
	let createOpen = $state(false);
	let clearConfirm = $state(false);
	let clearBusy = $state(false);
	let workspaceRole = $state('');
	onMount(() => {
		void load();
	});

	// Machines whose runtime has ended; they are listed apart and can be cleared.
	const endedStatuses = new Set(['destroyed', 'failed', 'expired']);
	const activeMachines = $derived(machines.filter((machine) => !endedStatuses.has(machine.status)));
	const endedMachines = $derived(machines.filter((machine) => endedStatuses.has(machine.status)));

	async function load(pageNum = 1) {
		loading = true;
		failed = false;
		currentPage = pageNum;
		try {
			// The API uses issue_id filtering — pagination is implicit via page param
			const [result, workspace] = await Promise.all([
				listDevMachines(slug, issueId || undefined, pageNum),
				workspaceRole ? Promise.resolve(null) : getWorkspace(slug).catch(() => null)
			]);
			machines = result.data ?? [];
			totalCount = result.total_count ?? 0;
			hasMore = result.has_more ?? false;
			if (workspace?.current_user_role) workspaceRole = workspace.current_user_role;
		} catch (error) {
			failed = true;
			appToast.apiError(error, m['machines.failed_load']());
		} finally {
			loading = false;
		}
	}

	async function clearEndedMachines() {
		clearBusy = true;
		try {
			// An explicit clear removes the listed machines whatever their age;
			// the backend's retention window only guards automatic cleanup.
			const result = await bulkPermanentDeleteDevMachines(slug, {
				machine_ids: endedMachines.map((machine) => machine.id),
				older_than_days: 0,
				include_failed: true,
				include_expired: true
			});
			const kept = [
				result.skipped_active ? m['machines.clear_kept_active']({ count: result.skipped_active }) : '',
				result.skipped_runtime ? m['machines.clear_kept_runtime']({ count: result.skipped_runtime }) : '',
				result.skipped_recent ? m['machines.clear_kept_recent']({ count: result.skipped_recent }) : ''
			].filter(Boolean);
			const title = result.count === 1 ? m['machines.clear_removed_one']() : m['machines.clear_removed']({ count: result.count });
			const options = kept.length ? { description: kept.join(' ') } : undefined;
			if (result.count === 0 && kept.length) appToast.warning(title, options);
			else appToast.success(title, options);
			clearConfirm = false;
			await load(currentPage);
			// Clearing every row of a later page leaves it out of range; step back
			// so the remaining machines and the pager stay reachable.
			while (!failed && machines.length === 0 && currentPage > 1) await load(currentPage - 1);
		} catch (error) {
			appToast.apiError(error, m['machines.clear_failed']());
		} finally {
			clearBusy = false;
		}
	}

	function sizeLabel(machine: DevMachine) {
		return `${machine.machine_size} · ${machine.cpu_millis / 1000} CPU · ${machine.memory_mb / 1024} GB`;
	}

	const canAdminDevMachines = $derived(workspaceRole === 'owner' || workspaceRole === 'admin');
</script>

{#snippet machineTable(rows: DevMachine[])}
	<div class="overflow-hidden rounded-lg border border-[var(--app-border)]">
		<Table.Root>
			<Table.Header>
				<Table.Row class="hover:bg-transparent">
					<Table.Head class="text-xs text-muted-foreground">{m['machines.column_machine']()}</Table.Head>
					<Table.Head class="w-px text-xs text-muted-foreground">{m['machines.column_status']()}</Table.Head>
					<Table.Head class="hidden w-px text-xs text-muted-foreground sm:table-cell">{m['machines.column_size']()}</Table.Head>
					<Table.Head class="w-px p-0"><span class="sr-only">{m['machines.open_machine']()}</span></Table.Head>
				</Table.Row>
			</Table.Header>
			<Table.Body>
				{#each rows as machine (machine.id)}
					{@const repository = machine.repo_owner && machine.repo_name ? `${machine.repo_owner}/${machine.repo_name}` : m['machines.no_repository']()}
					{@const notice = machine.resource_limit_message || (machine.status === 'failed' ? machine.last_error_message : '')}
					<Table.Row data-testid="machine-row" class="relative">
						<!-- max-w-0 lets the name column take the remaining width and truncate inside it. -->
						<Table.Cell class="w-full max-w-0 py-2.5 pl-3 align-top">
							<a href="/{slug}/machines/{machine.id}" class="block truncate text-sm font-medium after:absolute after:inset-0 focus-visible:outline-none focus-visible:after:ring-2 focus-visible:after:ring-ring focus-visible:after:ring-inset" title={machine.name} data-testid="machine-name">{machine.name}</a>
							<p class="truncate text-xs text-muted-foreground" title={machine.working_branch ? `${repository} · ${machine.working_branch}` : repository}>{repository}{#if machine.working_branch}{` · ${machine.working_branch}`}{/if}</p>
							{#if machine.pending_cpu_millis || machine.pending_memory_mb || machine.pending_disk_gb}
								<p class="truncate text-xs text-blue-400">{m['machines.list_resize_pending']()}</p>
							{/if}
							{#if notice}
								<p class="truncate text-xs text-red-400" title={notice}>{notice}</p>
							{/if}
							<p class="truncate text-xs capitalize text-muted-foreground sm:hidden">{sizeLabel(machine)}</p>
						</Table.Cell>
						<!-- Minimum widths keep the columns of both sections aligned. -->
						<Table.Cell class="py-2.5 align-top"><div class="sm:min-w-32"><MachineStatusBadge status={machine.status} /></div></Table.Cell>
						<Table.Cell class="hidden py-2.5 align-top text-xs capitalize text-muted-foreground sm:table-cell"><div class="min-w-40">{sizeLabel(machine)}</div></Table.Cell>
						<Table.Cell class="py-2.5 pr-2 pl-0 align-top text-muted-foreground"><ChevronRight size={14} aria-hidden="true" /></Table.Cell>
					</Table.Row>
				{/each}
			</Table.Body>
		</Table.Root>
	</div>
{/snippet}

<section class="space-y-4" aria-label={m['machines.title']()}>
	<div class="flex flex-wrap items-center justify-between gap-3">
		<p class="min-w-0 flex-1 text-xs text-[var(--color-text-tertiary)]">{issueId ? m['machines.filtered_by_issue']() : m['machines.list_desc']()}</p>
		<Button size="sm" onclick={() => (createOpen = true)}><Plus size={14} />{m['machines.new_machine']()}</Button>
	</div>

	{#if loading}
		<LoadingState />
	{:else if failed}
		<ErrorState message={m['machines.failed_load']()} onretry={() => load(currentPage)} />
	{:else if machines.length === 0}
		<EmptyState title={issueId ? m['machines.no_machines_for_issue']() : m['machines.no_machines']()} description={issueId ? m['machines.create_from_issue']() : m['machines.create_isolated_env']()} action={{ label: m['machines.new_machine'](), onclick: () => (createOpen = true) }} />
	{:else}
		<section class="space-y-2" aria-labelledby="active-machines-title">
			<h2 id="active-machines-title" class="text-xs font-medium text-muted-foreground">{m['machines.section_active']()}</h2>
			{#if activeMachines.length}
				{@render machineTable(activeMachines)}
			{:else}
				<p class="rounded-lg border border-dashed border-[var(--app-border)] p-3 text-xs text-muted-foreground">{m['machines.no_active_machines']()}</p>
			{/if}
		</section>
		{#if endedMachines.length}
			<section class="space-y-2" aria-labelledby="ended-machines-title">
				<div class="flex min-h-8 items-center justify-between gap-3">
					<h2 id="ended-machines-title" class="text-xs font-medium text-muted-foreground">{m['machines.section_ended']()}</h2>
					{#if canAdminDevMachines}<Button size="sm" variant="outline" onclick={() => (clearConfirm = true)}><Trash2 size={14} />{m['machines.clear']()}</Button>{/if}
				</div>
				{@render machineTable(endedMachines)}
			</section>
		{/if}
		{#if totalCount > machines.length || hasMore}
			<div class="flex items-center justify-center gap-2">
				<Button variant="outline" size="sm" disabled={currentPage <= 1} onclick={() => load(currentPage - 1)}><ChevronLeft size={14} /> {m['machines.prev']()}</Button>
				<span class="text-xs text-[var(--color-text-tertiary)]">{m['machines.pagination']({ page: currentPage, total: totalCount })}</span>
				<Button variant="outline" size="sm" disabled={!hasMore} onclick={() => load(currentPage + 1)}>{m['machines.next']()} <ChevronRight size={14} /></Button>
			</div>
		{/if}
	{/if}
</section>

<CreateMachineDialog bind:open={createOpen} {slug} oncreated={(machine) => goto(`/${slug}/machines/${machine.id}`)} />
<AlertDialog.Root bind:open={clearConfirm}><AlertDialog.Content><AlertDialog.Header><AlertDialog.Title>{m['machines.clear_title']()}</AlertDialog.Title><AlertDialog.Description>{m['machines.clear_desc']()}</AlertDialog.Description></AlertDialog.Header><AlertDialog.Footer><AlertDialog.Cancel>{m['common.cancel']()}</AlertDialog.Cancel><AlertDialog.Action variant="destructive" onclick={clearEndedMachines} disabled={clearBusy}>{clearBusy ? m['machines.deleting']() : m['machines.clear_action']({ count: endedMachines.length })}</AlertDialog.Action></AlertDialog.Footer></AlertDialog.Content></AlertDialog.Root>
