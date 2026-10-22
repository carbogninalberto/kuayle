<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { Plus, Server, GitBranch, Clock3, ChevronLeft, ChevronRight, Trash2 } from '@lucide/svelte';
	import * as AlertDialog from '$lib/components/ui/alert-dialog';
	import * as Card from '$lib/components/ui/card';
	import { bulkPermanentDeleteDevMachines, listDevMachines } from '$lib/api/dev-machines';
	import { getWorkspace } from '$lib/api/workspaces';
	import type { DevMachine } from '$lib/types/dev-machine';
	import EmptyState from '$lib/components/shared/EmptyState.svelte';
	import LoadingState from '$lib/components/shared/LoadingState.svelte';
	import ErrorState from '$lib/components/shared/ErrorState.svelte';
	import SidebarToggle from '$lib/components/layout/SidebarToggle.svelte';
	import CreateMachineDialog from '$lib/features/dev-machines/CreateMachineDialog.svelte';
	import MachineStatusBadge from '$lib/features/dev-machines/MachineStatusBadge.svelte';
	import { appToast } from '$lib/features/toast/toast';
	import { Button } from '$lib/components/ui/button';
	import { m } from '$lib/paraglide/messages.js';
	import { getLocale, setLocale } from '$lib/paraglide/runtime.js';

	const slug = $derived(page.params.workspaceSlug ?? '');
	const issueId = $derived(page.url.searchParams.get('issue_id') ?? '');
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
			appToast.apiError(error, 'Failed to load Dev Machines');
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

<div class="flex h-full flex-col">
	<header class="flex h-[49px] items-center justify-between border-b border-[var(--app-border)] px-4 sm:px-6">
		<div class="flex items-center gap-2">
			<SidebarToggle />
			<Server size={16} />
			<h1 class="text-sm font-medium">{m['machines.title']()}</h1>
			{#if issueId}<span class="ml-2 text-[10px] text-[var(--color-text-tertiary)]">{m['machines.filtered_by_issue']()}</span>{/if}
		</div>
		<div class="flex gap-2">{#if canAdminDevMachines}<Button size="sm" variant="outline" onclick={() => (cleanupConfirm = true)}><Trash2 size={14} />{m['machines.delete_old']()}</Button>{/if}<Button size="sm" onclick={() => (createOpen = true)}><Plus size={14} />{m['machines.new_machine']()}</Button></div>
	</header>

	<div class="min-h-0 flex-1 overflow-y-auto p-4 sm:p-6">
		{#if loading}
			<LoadingState />
		{:else if failed}
			<ErrorState message={m['machines.failed_load']()} onretry={() => load(currentPage)} />
		{:else if machines.length === 0}
			<EmptyState title={issueId ? m['machines.no_machines_for_issue']() : m['machines.no_machines']()} description={issueId ? m['machines.create_from_issue']() : m['machines.create_isolated_env']()} action={{ label: m['machines.new_machine'](), onclick: () => (createOpen = true) }} />
		{:else}
			<div class="grid gap-3 lg:grid-cols-2 xl:grid-cols-3">
				{#each machines as machine}
					<a href="/{slug}/machines/{machine.id}" class="group block"><Card.Root class="h-full transition group-hover:border-[var(--color-text-tertiary)]"><Card.Header><div class="flex items-start justify-between gap-3"><div class="min-w-0"><Card.Title class="truncate text-sm">{machine.name}</Card.Title><Card.Description class="truncate">{machine.repo_owner && machine.repo_name ? `${machine.repo_owner}/${machine.repo_name}` : m['machines.no_repository']()}</Card.Description></div><MachineStatusBadge status={machine.status} /></div></Card.Header><Card.Content><div class="flex items-center gap-3 text-[11px] text-muted-foreground"><span class="flex items-center gap-1"><GitBranch size={12} />{machine.working_branch || m['machines.waiting_checkout']()}</span></div></Card.Content><Card.Footer class="justify-between border-t pt-3 text-[11px]"><span class="capitalize text-muted-foreground">{machine.machine_size} · {machine.cpu_millis / 1000} CPU · {machine.memory_mb / 1024} GB</span><span class="flex items-center gap-1 text-muted-foreground"><Clock3 size={12} />{remaining(machine)}</span></Card.Footer></Card.Root></a>
				{/each}
			</div>
			{#if totalCount > machines.length || hasMore}
				<div class="mt-4 flex items-center justify-center gap-2">
					<Button variant="outline" size="sm" disabled={currentPage <= 1} onclick={() => load(currentPage - 1)}><ChevronLeft size={14} /> {m['machines.prev']()}</Button>
					<span class="text-xs text-[var(--color-text-tertiary)]">{m['machines.pagination']({ page: currentPage, total: totalCount })}</span>
					<Button variant="outline" size="sm" disabled={!hasMore} onclick={() => load(currentPage + 1)}>{m['machines.next']()} <ChevronRight size={14} /></Button>
				</div>
			{/if}
		{/if}
	</div>
</div>

<CreateMachineDialog bind:open={createOpen} {slug} oncreated={(machine) => (window.location.href = `/${slug}/machines/${machine.id}`)} />
<AlertDialog.Root bind:open={cleanupConfirm}><AlertDialog.Content><AlertDialog.Header><AlertDialog.Title>{m['machines.cleanup_title']()}</AlertDialog.Title><AlertDialog.Description>{m['machines.cleanup_desc']()}</AlertDialog.Description></AlertDialog.Header><AlertDialog.Footer><AlertDialog.Cancel>{m['common.cancel']()}</AlertDialog.Cancel><AlertDialog.Action variant="destructive" onclick={cleanupOldMachines} disabled={cleanupBusy}>{cleanupBusy ? m['machines.deleting']() : m['machines.cleanup_action']()}</AlertDialog.Action></AlertDialog.Footer></AlertDialog.Content></AlertDialog.Root>
