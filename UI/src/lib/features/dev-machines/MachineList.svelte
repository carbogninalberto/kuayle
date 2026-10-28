<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { Plus, GitBranch, Clock3, ChevronLeft, ChevronRight, Trash2 } from '@lucide/svelte';
	import * as AlertDialog from '$lib/components/ui/alert-dialog';
	import * as Card from '$lib/components/ui/card';
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
	let cleanupConfirm = $state(false);
	let cleanupBusy = $state(false);
	let workspaceRole = $state('');
	let now = $state(Date.now());
	let clock: ReturnType<typeof setInterval> | undefined;

	onMount(() => {
		void load();
		clock = setInterval(() => (now = Date.now()), 30000);
		return () => clock && clearInterval(clock);
	});

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

	function remaining(machine: DevMachine) {
		const minutes = Math.max(0, Math.ceil((new Date(machine.expires_at).getTime() - now) / 60000));
		return minutes > 60 ? `${Math.floor(minutes / 60)}h ${minutes % 60}m` : `${minutes}m`;
	}

	async function cleanupOldMachines() {
		cleanupBusy = true;
		try {
			const result = await bulkPermanentDeleteDevMachines(slug, { include_failed: true, include_expired: true });
			appToast.success(`${result.count} old ${result.count === 1 ? 'machine' : 'machines'} permanently deleted`);
			cleanupConfirm = false;
			await load(1);
		} catch (error) {
			appToast.apiError(error, 'Failed to permanently delete old machines');
		} finally {
			cleanupBusy = false;
		}
	}

	const canAdminDevMachines = $derived(workspaceRole === 'owner' || workspaceRole === 'admin');
</script>

<section class="space-y-4" aria-label={m['machines.title']()}>
	<div class="flex flex-wrap items-center justify-between gap-3">
		<p class="min-w-0 text-xs text-[var(--color-text-tertiary)]">{issueId ? m['machines.filtered_by_issue']() : m['machines.list_desc']()}</p>
		<div class="flex gap-2">{#if canAdminDevMachines}<Button size="sm" variant="outline" onclick={() => (cleanupConfirm = true)}><Trash2 size={14} />{m['machines.delete_old']()}</Button>{/if}<Button size="sm" onclick={() => (createOpen = true)}><Plus size={14} />{m['machines.new_machine']()}</Button></div>
	</div>

	{#if loading}
		<LoadingState />
	{:else if failed}
		<ErrorState message={m['machines.failed_load']()} onretry={() => load(currentPage)} />
	{:else if machines.length === 0}
		<EmptyState title={issueId ? m['machines.no_machines_for_issue']() : m['machines.no_machines']()} description={issueId ? m['machines.create_from_issue']() : m['machines.create_isolated_env']()} action={{ label: m['machines.new_machine'](), onclick: () => (createOpen = true) }} />
	{:else}
		<div class="grid gap-3 sm:grid-cols-2">
			{#each machines as machine (machine.id)}
				<a href="/{slug}/machines/{machine.id}" class="group block"><Card.Root class="h-full transition group-hover:border-[var(--color-text-tertiary)]"><Card.Header><div class="flex items-start justify-between gap-3"><div class="min-w-0"><Card.Title class="truncate text-sm">{machine.name}</Card.Title><Card.Description class="truncate">{machine.repo_owner && machine.repo_name ? `${machine.repo_owner}/${machine.repo_name}` : m['machines.no_repository']()}</Card.Description></div><MachineStatusBadge status={machine.status} /></div></Card.Header><Card.Content><div class="flex items-center gap-3 text-[11px] text-muted-foreground"><span class="flex min-w-0 items-center gap-1"><GitBranch size={12} class="shrink-0" /><span class="truncate">{machine.working_branch || m['machines.waiting_checkout']()}</span></span></div></Card.Content><Card.Footer class="justify-between border-t pt-3 text-[11px]"><span class="capitalize text-muted-foreground">{machine.machine_size} · {machine.cpu_millis / 1000} CPU · {machine.memory_mb / 1024} GB</span><span class="flex items-center gap-1 text-muted-foreground"><Clock3 size={12} />{remaining(machine)}</span></Card.Footer></Card.Root></a>
			{/each}
		</div>
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
<AlertDialog.Root bind:open={cleanupConfirm}><AlertDialog.Content><AlertDialog.Header><AlertDialog.Title>{m['machines.cleanup_title']()}</AlertDialog.Title><AlertDialog.Description>{m['machines.cleanup_desc']()}</AlertDialog.Description></AlertDialog.Header><AlertDialog.Footer><AlertDialog.Cancel>{m['common.cancel']()}</AlertDialog.Cancel><AlertDialog.Action variant="destructive" onclick={cleanupOldMachines} disabled={cleanupBusy}>{cleanupBusy ? m['machines.deleting']() : m['machines.cleanup_action']()}</AlertDialog.Action></AlertDialog.Footer></AlertDialog.Content></AlertDialog.Root>
