<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { page } from '$app/state';
	import { ArrowLeft, Bot, Code2, ExternalLink, GitBranch, Pause, Play, Save, Server, ServerOff, Square, SquareTerminal, Trash2, Loader } from 'lucide-svelte';
	import { goto, replaceState } from '$app/navigation';
	import * as AlertDialog from '$lib/components/ui/alert-dialog';
	import * as Card from '$lib/components/ui/card';
	import * as Dialog from '$lib/components/ui/dialog';
	import {
		getDevMachine, launchMachineServiceWithResume, listMachineServices, listMachineEvents, listMachineLogs,
		listMachineAgentRuns, listResourceUsage, pauseDevMachine, startDevMachine, stopDevMachine, teardownDevMachine, cancelAgentRun,
		checkoutIssue, deleteDevMachine, listMachineCheckouts, snapshotDevMachineEnvironment, updateDevMachine
	} from '$lib/api/dev-machines';
	import { getWorkspace } from '$lib/api/workspaces';
	import type { AgentRun, DevMachine, DevMachineCheckout, DevMachineEvent, DevMachineLogChunk, DevMachineService, ResourceSample } from '$lib/types/dev-machine';
	import LoadingState from '$lib/components/shared/LoadingState.svelte';
	import ErrorState from '$lib/components/shared/ErrorState.svelte';
	import MachineStatusBadge from '$lib/features/dev-machines/MachineStatusBadge.svelte';
	import AgentRunDialog from '$lib/features/dev-machines/AgentRunDialog.svelte';
	import AgentRunTraceSheet from '$lib/features/dev-machines/AgentRunTraceSheet.svelte';
	import { useTerminalDock } from '$lib/features/dev-machines/terminal-dock-context.svelte';
	import { appToast } from '$lib/features/toast/toast';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { Switch } from '$lib/components/ui/switch';

	const slug = $derived(page.params.workspaceSlug ?? '');
	const machineId = $derived(page.params.machineId ?? '');
	let machine = $state<DevMachine | null>(null);
	let services = $state<DevMachineService[]>([]);
	let checkouts = $state<DevMachineCheckout[]>([]);
	let events = $state<DevMachineEvent[]>([]);
	let eventsAfterId = 0;
	let logs = $state<DevMachineLogChunk[]>([]);
	let logsAfterId = 0;
	let runs = $state<AgentRun[]>([]);
	let runsPage = $state(1);
	let runsHasMore = $state(false);
	let usage = $state<ResourceSample[]>([]);
	let loading = $state(true);
	let failed = $state(false);
	let actionBusy = $state(false);
	let teardownConfirm = $state(false);
	let deleteConfirm = $state(false);
	let snapshotOpen = $state(false);
	let snapshotName = $state('');
	let snapshotBusy = $state(false);
	let runOpen = $state(false);
	let cancelBusy = $state<Record<string, boolean>>({});
	let timer: ReturnType<typeof setInterval> | undefined;
	let polling = false;
	let checkoutAttempted = false;
	let workspaceRole = $state('');
	let launchBusy = $state<Record<string, boolean>>({});
	let terminalQueryConsumed = false;
	let traceRunId = $state<string | null>(null);
	let traceOpen = $state(false);
	const dock = useTerminalDock();

	// Treat desired_status != status as pending ("transitioning")
	const isPending = $derived(machine ? machine.desired_status !== machine.status : false);
	// Controls are disabled when pending or action busy
	const controlsDisabled = $derived(isPending || actionBusy);

	const latestUsage = $derived(usage[0]);
	const canAdminDevMachines = $derived(workspaceRole === 'owner' || workspaceRole === 'admin');
	const readyCheckouts = $derived(checkouts.filter((checkout) => checkout.status === 'ready'));
	const ideService = $derived(services.find((item) => item.service_type === 'ide'));
	const terminalService = $derived(services.find((item) => item.service_type === 'terminal'));

	$effect(() => {
		const queryRunId = page.url.searchParams.get('agent_run_id');
		const hashMatch = page.url.hash.match(/^#agent-run-([0-9a-fA-F-]{36})$/);
		const linkedRunId = queryRunId || hashMatch?.[1] || null;
		if (linkedRunId) {
			traceRunId = linkedRunId;
			traceOpen = true;
		} else {
			traceRunId = null;
			traceOpen = false;
		}
	});

	onMount(() => {
		void load();
		timer = setInterval(() => void poll(), 4000);
	});
	onDestroy(() => timer && clearInterval(timer));

	async function load() {
		loading = true;
		failed = false;
		try {
			await refreshAll();
		} catch (error) {
			failed = true;
			appToast.apiError(error, 'Failed to load Dev Machine');
		} finally {
			loading = false;
		}
	}

	async function refreshAll() {
		const nextMachine = await getDevMachine(slug, machineId);
		machine = nextMachine;
		// Serialized polling per panel — each fetch is independent and resilient
		await Promise.allSettled([
			listMachineServices(slug, machineId).then((s) => { services = s ?? []; }).catch(() => {}),
			listMachineCheckouts(slug, machineId).then((items) => { checkouts = items ?? []; }).catch(() => {}),
			listResourceUsage(slug, machineId).then((u) => { usage = u ?? []; }).catch(() => {}),
			listMachineAgentRuns(slug, machineId).then((r) => {
				const latest = r.data ?? [];
				if (runsPage === 1) runs = latest;
				else {
					const latestIds = new Set(latest.map((run) => run.id));
					runs = [...latest, ...runs.filter((run) => !latestIds.has(run.id))];
				}
				runsHasMore = runsPage === 1 ? r.has_more : runsHasMore;
			}).catch(() => {})
		]);
		if (!workspaceRole) {
			getWorkspace(slug).then((workspace) => { workspaceRole = workspace.current_user_role; }).catch(() => {});
		}
		if (!checkoutAttempted && nextMachine.status === 'running' && nextMachine.issue_id && checkouts.length === 0) {
			checkoutAttempted = true;
			checkoutIssue(slug, machineId, nextMachine.issue_id)
				.then((checkout) => { checkouts = [checkout]; })
				.catch((error) => appToast.apiError(error, 'Set a development repository before preparing this issue'));
		}
		// Append/dedupe events and logs with after_id tracking
		await listMachineEvents(slug, machineId, eventsAfterId).then((e) => {
			if (e && e.length > 0) {
				const existingIds = new Set(events.map((ev) => ev.id));
				const newEvents = e.filter((ev) => !existingIds.has(ev.id));
				events = [...events, ...newEvents];
				eventsAfterId = Math.max(eventsAfterId, ...e.map((ev) => ev.id));
			}
		}).catch(() => {});
		await listMachineLogs(slug, machineId, logsAfterId).then((l) => {
			if (l && l.length > 0) {
				const existingIds = new Set(logs.map((lg) => lg.id));
				const newLogs = l.filter((lg) => !existingIds.has(lg.id));
				logs = [...logs, ...newLogs];
				logsAfterId = Math.max(logsAfterId, ...l.map((lg) => lg.id));
			}
		}).catch(() => {});
		const terminalParam = page.url.searchParams.get('terminal');
		if (terminalParam === '1' && !terminalQueryConsumed) {
			terminalQueryConsumed = true;
			const checkoutId = page.url.searchParams.get('checkout_id') ?? undefined;
			const checkout = checkoutId ? checkouts.find((c) => c.id === checkoutId) : undefined;
			dock.open({
				slug,
				machineId: machine.id,
				machineName: machine.name,
				checkoutId,
				checkoutLabel: checkout ? `${checkout.repository_full_name} - ${checkout.working_branch}` : undefined
			});
			const url = new URL(page.url);
			url.searchParams.delete('terminal');
			url.searchParams.delete('checkout_id');
			replaceState(url, page.state);
		}
	}

	async function poll() {
		if (polling) return;
		polling = true;
		try {
			await refreshAll();
		} catch {
			// Background poll failures are silent, keep stale data
		} finally {
			polling = false;
		}
	}

	function openTrace(runId: string) {
		const url = new URL(page.url);
		url.searchParams.set('agent_run_id', runId);
		url.hash = `agent-run-${runId}`;
		void goto(url, { noScroll: true, keepFocus: true });
	}

	function closeTrace() {
		traceOpen = false;
		traceRunId = null;
		const url = new URL(page.url);
		url.searchParams.delete('agent_run_id');
		if (url.hash.startsWith('#agent-run-')) url.hash = '';
		void goto(url, { replaceState: true, noScroll: true, keepFocus: true });
	}

	async function lifecycle(action: 'start' | 'pause' | 'stop') {
		actionBusy = true;
		try {
			if (action === 'start') await startDevMachine(slug, machineId);
			if (action === 'pause') await pauseDevMachine(slug, machineId);
			if (action === 'stop') await stopDevMachine(slug, machineId);
			appToast.success(`Machine ${action} queued`);
			await refreshAll();
		} catch (error) {
			appToast.apiError(error, `Failed to ${action} machine`);
		} finally {
			actionBusy = false;
		}
	}

	async function launch(service: DevMachineService, checkoutId?: string) {
		if (!machine) return;
		if (!serviceActionAvailable(service)) {
			appToast.warning(machine.status === 'paused' ? 'Resuming is only available for paused machines.' : 'Service is not ready. Wait until it reports healthy.');
			return;
		}
		if (service.service_type === 'terminal') {
			const checkout = checkoutId ? checkouts.find((c) => c.id === checkoutId) : undefined;
			dock.open({
				slug,
				machineId: machine.id,
				machineName: machine.name,
				checkoutId: checkoutId,
				checkoutLabel: checkout ? `${checkout.repository_full_name} - ${checkout.working_branch}` : undefined
			});
			return;
		}
		const busyKey = `${service.service_key}:${checkoutId ?? 'root'}`;
		launchBusy[busyKey] = true;
		launchBusy = { ...launchBusy };
		const popup = window.open('about:blank', '_blank');
		if (popup) popup.opener = null;
		try {
			const result = await launchMachineServiceWithResume(slug, machineId, service.service_key, checkoutId, {
				onStatus: (status) => appToast.info(status === 'resuming' ? 'Resuming paused Dev Machine…' : 'Waiting for Dev Machine…')
			});
			if (popup && result.launch_url) popup.location.replace(result.launch_url);
			else appToast.warning('Pop-up blocked. Please allow pop-ups for this site.');
			await refreshAll();
		} catch (error) {
			popup?.close();
			appToast.apiError(error, `Failed to open ${service.service_type}`);
		} finally {
			launchBusy[busyKey] = false;
			launchBusy = { ...launchBusy };
		}
	}

	async function teardown() {
		teardownConfirm = false;
		actionBusy = true;
		try {
			await teardownDevMachine(slug, machineId);
			appToast.success('Machine teardown queued');
			await refreshAll();
		} catch (error) {
			appToast.apiError(error, 'Failed to teardown machine');
		} finally {
			actionBusy = false;
		}
	}

	async function removeMachine() {
		deleteConfirm = false;
		actionBusy = true;
		try {
			await deleteDevMachine(slug, machineId);
			appToast.success('Dev Machine deletion requested');
			await goto(`/${slug}/machines`);
		} catch (error) {
			appToast.apiError(error, 'Failed to permanently delete machine');
		} finally {
			actionBusy = false;
		}
	}

	async function toggleKeepRunning(checked: boolean) {
		if (!machine) return;
		const previous = machine.keep_running;
		machine.keep_running = checked;
		try {
			machine = await updateDevMachine(slug, machineId, { keep_running: checked });
		} catch (error) {
			machine.keep_running = previous;
			appToast.apiError(error, 'Failed to update inactivity behavior');
		}
	}

	async function saveSnapshot() {
		if (!snapshotName.trim()) return;
		snapshotBusy = true;
		try {
			await snapshotDevMachineEnvironment(slug, { name: snapshotName.trim(), source_machine_id: machineId });
			appToast.success('Development environment snapshot queued');
			snapshotOpen = false;
		} catch (error) {
			appToast.apiError(error, 'Failed to save development environment');
		} finally {
			snapshotBusy = false;
		}
	}

	async function doCancelAgentRun(runId: string) {
		cancelBusy[runId] = true;
		cancelBusy = { ...cancelBusy };
		try {
			await cancelAgentRun(slug, runId);
			appToast.success('Run cancelled');
			await refreshAll();
		} catch (error) {
			appToast.apiError(error, 'Failed to cancel run');
		} finally {
			cancelBusy[runId] = false;
			cancelBusy = { ...cancelBusy };
		}
	}

	async function loadMoreRuns() {
		const nextPage = runsPage + 1;
		try {
			const response = await listMachineAgentRuns(slug, machineId, nextPage);
			const existing = new Set(runs.map((run) => run.id));
			runs = [...runs, ...(response.data ?? []).filter((run) => !existing.has(run.id))];
			runsPage = nextPage;
			runsHasMore = response.has_more;
		} catch (error) {
			appToast.apiError(error, 'Failed to load older runs');
		}
	}

	function bytes(value = 0) {
		if (value < 1024 * 1024) return `${Math.round(value / 1024)} KB`;
		if (value < 1024 * 1024 * 1024) return `${(value / 1024 / 1024).toFixed(1)} MB`;
		return `${(value / 1024 / 1024 / 1024).toFixed(1)} GB`;
	}

	function serviceHealthy(service: DevMachineService): boolean {
		return service.status === 'running' && ['healthy', 'running'].includes(service.health_status);
	}

	function machineCanAutoLaunch(): boolean {
		return !!machine && (machine.status === 'running' || machine.status === 'paused' || (['queued', 'spawning'].includes(machine.status) && machine.desired_status === 'running')) && !['destroyed', 'expired', 'stopped', 'failed'].includes(machine.status);
	}

	function serviceActionAvailable(service: DevMachineService): boolean {
		if (!['ide', 'terminal', 'browser'].includes(service.service_type)) return false;
		if (!machineCanAutoLaunch() || (isPending && machine?.status !== 'paused')) return false;
		return machine?.status === 'paused' || serviceHealthy(service);
	}

	function launchBusyFor(service: DevMachineService, checkoutId?: string) {
		return !!launchBusy[`${service.service_key}:${checkoutId ?? 'root'}`];
	}
</script>

{#if loading}
	<LoadingState />
{:else if failed || !machine}
	<ErrorState message="Unable to load Dev Machine" onretry={load} />
{:else}
	<div class="flex h-full flex-col">
		<header class="flex min-h-[49px] items-center justify-between gap-3 border-b border-[var(--app-border)] px-4">
			<div class="flex min-w-0 items-center gap-2">
				<a href="/{slug}/machines" class="rounded p-1 hover:bg-[var(--color-bg-hover)]"><ArrowLeft size={16} /></a>
				<Server size={15} />
				<span class="truncate text-sm font-medium">{machine.name}</span>
				<MachineStatusBadge status={machine.status} />
				{#if machine.environment_builder}<span class="text-[10px] font-semibold uppercase text-[var(--app-accent)]">Environment Builder</span>{/if}
				{#if isPending}
					<span class="inline-flex items-center gap-1 rounded-full border border-blue-500/30 bg-blue-500/10 px-2 py-0.5 text-[10px] font-semibold uppercase text-blue-400">
						<Loader size={10} class="animate-spin" /> Transitioning
					</span>
				{/if}
			</div>
			<div class="flex items-center gap-1">
				{#if machine.environment_builder && (machine.status === 'paused' || machine.status === 'stopped')}
					<Button variant="ghost" size="icon-sm" disabled={actionBusy} onclick={() => { snapshotName = `${machine?.name ?? 'Development'} environment`; snapshotOpen = true; }} title="Save development environment"><Save size={15} /></Button>
				{/if}
				{#if machine.status === 'running' && !isPending}
					<Button variant="ghost" size="icon-sm" disabled={controlsDisabled || actionBusy} onclick={() => lifecycle('pause')} title="Pause"><Pause size={15} /></Button>
					<Button variant="ghost" size="icon-sm" disabled={controlsDisabled || actionBusy} onclick={() => lifecycle('stop')} title="Stop"><Square size={15} /></Button>
				{/if}
				{#if (machine.status === 'paused' || machine.status === 'stopped' || machine.status === 'failed') && !isPending}
					<Button variant="ghost" size="icon-sm" disabled={controlsDisabled || actionBusy} onclick={() => lifecycle('start')} title="Start"><Play size={15} /></Button>
				{/if}
				<Button variant="ghost" size="icon-sm" disabled={actionBusy || machine.status === 'destroyed'} onclick={() => (teardownConfirm = true)} title="Teardown runtime"><ServerOff size={15} /></Button>
				{#if canAdminDevMachines}
					<Button variant="destructive" size="icon-sm" disabled={actionBusy} onclick={() => (deleteConfirm = true)} title="Delete permanently"><Trash2 size={15} /></Button>
				{/if}
			</div>
		</header>

		<div class="min-h-0 flex-1 overflow-y-auto p-4 sm:p-6">
			<div class="mx-auto max-w-6xl space-y-5">
				<section class="grid gap-3 md:grid-cols-4">
					<div class="rounded-xl border border-[var(--app-border)] bg-[var(--color-bg-secondary)] p-4 md:col-span-2"><p class="text-[10px] uppercase tracking-wider text-[var(--color-text-tertiary)]">Repository affinity</p>{#if machine.repo_owner && machine.repo_name}<p class="mt-2 text-sm font-medium">{machine.repo_owner}/{machine.repo_name}</p><p class="mt-1 flex items-center gap-1 text-xs text-[var(--color-text-tertiary)]"><GitBranch size={12} />{checkouts.length} issue {checkouts.length === 1 ? 'branch' : 'branches'}</p>{:else}<p class="mt-2 text-sm text-[var(--color-text-tertiary)]">No repository attached. Open this machine from an issue to prepare a branch.</p>{/if}</div>
					<div class="rounded-xl border border-[var(--app-border)] bg-[var(--color-bg-secondary)] p-4"><p class="text-[10px] uppercase tracking-wider text-[var(--color-text-tertiary)]">CPU / memory</p><p class="mt-2 text-sm font-medium">{latestUsage?.cpu_percent.toFixed(1) ?? '0.0'}% · {bytes(latestUsage?.memory_bytes)}</p><p class="mt-1 text-xs text-[var(--color-text-tertiary)]">Limit {machine.cpu_millis / 1000} CPU / {machine.memory_mb / 1024} GB</p></div>
					<div class="rounded-xl border border-[var(--app-border)] bg-[var(--color-bg-secondary)] p-4"><p class="text-[10px] uppercase tracking-wider text-[var(--color-text-tertiary)]">Workspace disk</p><p class="mt-2 text-sm font-medium">{bytes(latestUsage?.disk_bytes)}</p><p class="mt-1 text-xs text-[var(--color-text-tertiary)]">Soft limit {machine.disk_gb} GB</p></div>
				</section>

				<Card.Root>
					<Card.Header>
						<Card.Title class="text-sm">Developer environment</Card.Title>
						<Card.Description>Open code-server or a native terminal into the same workspace. Paused machines resume automatically; stopped machines must be started explicitly.</Card.Description>
					</Card.Header>
					<Card.Content>
						<div class="grid gap-3 sm:grid-cols-2">
							<Button variant="outline" class="h-auto justify-start gap-3 p-4" disabled={!ideService || !machineCanAutoLaunch() || (!!ideService && launchBusyFor(ideService))} onclick={() => ideService && launch(ideService)}>
								<Code2 class="size-5" />
								<span class="text-left"><span class="block text-sm font-medium">Code Editor</span><span class="block text-xs text-muted-foreground">Generic workspace at /workspace/tasks</span></span>
							</Button>
							<Button variant="outline" class="h-auto justify-start gap-3 p-4" disabled={!terminalService || !machineCanAutoLaunch()} onclick={() => terminalService && launch(terminalService)}>
								<SquareTerminal class="size-5" />
								<span class="text-left"><span class="block text-sm font-medium">Native Terminal</span><span class="block text-xs text-muted-foreground">In-app ttyd session at /workspace/tasks</span></span>
							</Button>
						</div>
					</Card.Content>
				</Card.Root>

				<section class="rounded-xl border border-[var(--app-border)] bg-[var(--color-bg-secondary)] p-4">
					<div class="flex items-center justify-between gap-4"><div><h2 class="text-sm font-semibold">Inactivity</h2><p class="text-xs text-[var(--color-text-tertiary)]">Pause automatically after workspace inactivity unless kept running.</p></div><label class="flex items-center gap-2 text-sm"><span>Keep running</span><Switch aria-label="Keep running" checked={machine.keep_running} onCheckedChange={toggleKeepRunning} /></label></div>
				</section>

				<section class="rounded-xl border border-[var(--app-border)] bg-[var(--color-bg-secondary)] p-4">
					<div class="mb-3"><h2 class="text-sm font-semibold">Issue worktrees</h2><p class="text-xs text-[var(--color-text-tertiary)]">Each issue branch has an independent checkout on this machine.</p></div>
					<div class="space-y-2">
						{#if checkouts.length === 0}<p class="text-xs text-[var(--color-text-tertiary)]">No issue branches prepared yet.</p>{/if}
						{#each checkouts as checkout}
							<div class="flex flex-col justify-between gap-3 rounded-lg border border-[var(--app-border)] p-3 sm:flex-row sm:items-center">
								<div class="min-w-0"><p class="truncate text-xs font-medium">{checkout.repository_full_name}</p><p class="mt-1 truncate text-[10px] text-[var(--color-text-tertiary)]">{checkout.working_branch} · {checkout.status}</p>{#if checkout.last_error}<p class="mt-1 text-[10px] text-red-400">{checkout.last_error}</p>{/if}</div>
								{#if checkout.status === 'ready' && machineCanAutoLaunch()}
									<div class="flex gap-2"><Button size="sm" variant="outline" disabled={!ideService || (!!ideService && launchBusyFor(ideService, checkout.id))} onclick={() => { if (ideService) launch(ideService, checkout.id); }}><Code2 size={13} />Code Editor</Button><Button size="sm" variant="outline" disabled={!terminalService} onclick={() => { if (terminalService) launch(terminalService, checkout.id); }}><SquareTerminal size={13} />Terminal</Button></div>
								{/if}
							</div>
						{/each}
					</div>
				</section>

				<section class="rounded-xl border border-[var(--app-border)] bg-[var(--color-bg-secondary)] p-4">
					<div class="mb-3 flex items-center justify-between">
						<div><h2 class="text-sm font-semibold">Services</h2><p class="text-xs text-[var(--color-text-tertiary)]">All public access is authenticated by the machine gateway.</p></div>
						{#if machine.status === 'running' && !isPending && readyCheckouts.length > 0}
							<Button size="sm" onclick={() => (runOpen = true)}><Bot size={13} />Run agent</Button>
						{/if}
					</div>
					<div class="grid gap-2 md:grid-cols-2">
						{#each services as service}
							<div class="flex items-center justify-between rounded-lg border border-[var(--app-border)] p-3">
								<div>
									<p class="text-xs font-medium capitalize">{service.service_type.replace('_', ' ')}</p>
									<p class="mt-1 text-[10px] text-[var(--color-text-tertiary)]">{service.status} · {service.health_status}{#if service.health_message && service.health_status !== 'healthy'} · {service.health_message}{/if}</p>
								</div>
								{#if serviceActionAvailable(service) && service.service_type !== 'ide' && service.service_type !== 'terminal'}
									<Button variant="ghost" size="icon-sm" disabled={launchBusyFor(service)} onclick={() => launch(service)} title="Open service"><ExternalLink size={14} /></Button>
								{:else if serviceActionAvailable(service) && (machine.environment_builder || checkouts.length === 0) && (service.service_type === 'ide' || service.service_type === 'terminal')}
									<Button variant="ghost" size="icon-sm" disabled={launchBusyFor(service)} onclick={() => launch(service)} title={service.service_type === 'ide' ? 'Open Code Editor' : 'Open Terminal'}>{#if service.service_type === 'ide'}<Code2 size={14} />{:else}<SquareTerminal size={14} />{/if}</Button>
								{/if}
							</div>
						{/each}
					</div>
				</section>

				<section id="agent-runs" class="rounded-xl border border-[var(--app-border)] bg-[var(--color-bg-secondary)] p-4 scroll-mt-4">
					<h2 class="text-sm font-semibold">Agent runs</h2>
					<div class="mt-3 space-y-2">
						{#if runs.length === 0}<p class="text-xs text-[var(--color-text-tertiary)]">No agent runs yet.</p>{/if}
						{#each runs as run}
							<article id={`agent-run-${run.id}`} class="rounded-lg border border-[var(--app-border)] p-3 transition-colors hover:bg-[var(--color-bg-hover)] scroll-mt-4">
								<div class="flex items-start gap-2">
									<button type="button" class="min-w-0 flex-1 text-left" onclick={() => openTrace(run.id)} aria-label={`View ${run.provider_id} agent run activity`}>
										<span class="flex items-center justify-between gap-2">
											<span><span class="text-xs font-medium">{run.provider_id}</span><span class="ml-2 text-[10px] uppercase text-[var(--color-text-tertiary)]">{run.mode}</span></span>
											<span class="text-[10px] font-semibold uppercase">{run.status}</span>
										</span>
										{#if run.summary}<span class="mt-2 block whitespace-pre-wrap text-xs text-[var(--color-text-secondary)]">{run.summary}</span>{/if}
									</button>
									{#if ['queued', 'starting', 'running', 'waiting_input'].includes(run.status)}
										<Button variant="ghost" size="xs" disabled={cancelBusy[run.id]} onclick={() => doCancelAgentRun(run.id)} class="shrink-0 text-red-400">
											{cancelBusy[run.id] ? 'Cancelling...' : 'Cancel'}
										</Button>
									{/if}
								</div>
								{#if run.pull_request_url}<a href={run.pull_request_url} target="_blank" rel="noreferrer" class="mt-2 inline-flex items-center gap-1 text-xs text-[var(--app-accent)]">Pull request <ExternalLink size={11} /></a>{/if}
								{#if run.risk_notes?.length}<div class="mt-2 text-xs text-amber-400">{run.risk_notes.join(' · ')}</div>{/if}
							</article>
						{/each}
						{#if runsHasMore}<Button variant="outline" size="sm" onclick={loadMoreRuns}>Load older runs</Button>{/if}
					</div>
				</section>

				<section id="activity" class="grid gap-5 lg:grid-cols-2 scroll-mt-4">
					<div class="rounded-xl border border-[var(--app-border)] bg-[var(--color-bg-secondary)] p-4"><h2 class="text-sm font-semibold">Activity</h2><div class="mt-3 max-h-96 space-y-3 overflow-y-auto">{#if events.length === 0}<p class="text-xs text-[var(--color-text-tertiary)]">No activity yet.</p>{/if}{#each events as event}
						{#if event.agent_run_id}
							<button type="button" class="block w-full border-l border-[var(--app-border)] pl-3 text-left transition-colors hover:border-[var(--app-accent)]" onclick={() => openTrace(event.agent_run_id!)} aria-label={`View agent activity for ${event.event_type}`}>
								<span class="block text-xs font-medium">{event.event_type.replaceAll('_', ' ')}</span>
								<span class="block text-[10px] text-[var(--color-text-tertiary)]">{event.source} · {new Date(event.occurred_at).toLocaleString()}</span>
							</button>
						{:else}
							<div class="border-l border-[var(--app-border)] pl-3">
								<p class="text-xs font-medium">{event.event_type.replaceAll('_', ' ')}</p>
								<p class="text-[10px] text-[var(--color-text-tertiary)]">{event.source} · {new Date(event.occurred_at).toLocaleString()}</p>
							</div>
						{/if}
					{/each}</div></div>
					<div class="rounded-xl border border-[var(--app-border)] bg-[var(--color-bg-secondary)] p-4"><h2 class="text-sm font-semibold">Logs</h2><pre class="mt-3 max-h-96 overflow-auto whitespace-pre-wrap rounded-lg bg-black/30 p-3 text-[11px] leading-relaxed text-zinc-300">{logs.length ? logs.map((chunk) => `[${chunk.stream}] ${chunk.content}`).join('\n') : 'No logs yet.'}</pre></div>
				</section>
			</div>
		</div>
	</div>

	<AgentRunDialog bind:open={runOpen} {slug} {machine} checkoutId={readyCheckouts[0]?.id} oncreated={(run) => { refreshAll(); openTrace(run.id); }} />

	<AgentRunTraceSheet bind:open={traceOpen} {slug} runId={traceRunId} onclose={closeTrace} />

	<AlertDialog.Root bind:open={teardownConfirm}><AlertDialog.Content><AlertDialog.Header><AlertDialog.Title>Teardown Dev Machine?</AlertDialog.Title><AlertDialog.Description>This removes containers, the isolated network, workspace volume, and active access sessions while retaining the machine history.</AlertDialog.Description></AlertDialog.Header><AlertDialog.Footer><AlertDialog.Cancel>Cancel</AlertDialog.Cancel><AlertDialog.Action onclick={teardown}>Teardown</AlertDialog.Action></AlertDialog.Footer></AlertDialog.Content></AlertDialog.Root>
	<AlertDialog.Root bind:open={deleteConfirm}><AlertDialog.Content><AlertDialog.Header><AlertDialog.Title>Delete Dev Machine permanently?</AlertDialog.Title><AlertDialog.Description>This tears down any running resources, then permanently removes machine history, logs, agent runs, issue worktrees, and volumes. This cannot be undone.</AlertDialog.Description></AlertDialog.Header><AlertDialog.Footer><AlertDialog.Cancel>Cancel</AlertDialog.Cancel><AlertDialog.Action variant="destructive" onclick={removeMachine} disabled={actionBusy}>{actionBusy ? 'Deleting...' : 'Delete permanently'}</AlertDialog.Action></AlertDialog.Footer></AlertDialog.Content></AlertDialog.Root>
	<Dialog.Root bind:open={snapshotOpen}><Dialog.Content class="sm:max-w-md"><Dialog.Header><Dialog.Title>Save development environment</Dialog.Title><Dialog.Description>Capture the paused developer container as an immutable local image. Repositories, volumes, and secrets are excluded.</Dialog.Description></Dialog.Header><label class="space-y-1.5"><Label for="snapshot-name">Environment name</Label><Input id="snapshot-name" bind:value={snapshotName} /></label><Dialog.Footer><Button variant="outline" onclick={() => (snapshotOpen = false)}>Cancel</Button><Button onclick={saveSnapshot} disabled={snapshotBusy || !snapshotName.trim()}>{snapshotBusy ? 'Queuing...' : 'Save snapshot'}</Button></Dialog.Footer></Dialog.Content></Dialog.Root>
{/if}
