<script lang="ts">
	import { page } from '$app/state';
	import { tick } from 'svelte';
	import { Activity, AppWindow, ArrowLeft, CalendarPlus, CircleAlert, Clock, Code2, Ellipsis, Cpu, ExternalLink, Gauge, GitBranch, LayoutDashboard, Pause, Play, Save, ScrollText, Server, ServerOff, Square, SquareTerminal, Trash2, Loader } from '@lucide/svelte';
	import * as Tabs from '$lib/components/ui/tabs/index.js';
	import { goto, replaceState } from '$app/navigation';
	import * as Alert from '$lib/components/ui/alert';
	import * as AlertDialog from '$lib/components/ui/alert-dialog';
	import * as Card from '$lib/components/ui/card';
	import * as Dialog from '$lib/components/ui/dialog';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import {
		getDevMachine, launchMachineServiceWithResume, listMachineServices, listMachineEvents, listMachineLogs,
		listMachineTasks, listResourceUsage, pauseDevMachine, startDevMachine, stopDevMachine, teardownDevMachine,
		checkoutIssue, deleteDevMachine, getDevMachinePolicy, listMachineCheckouts, snapshotDevMachineEnvironment
	} from '$lib/api/dev-machines';
	import type { ResourceUsageWindow } from '$lib/api/dev-machines';
	import { getWorkspace } from '$lib/api/workspaces';
	import type { AgentQueueItem } from '$lib/api/agent-tasks';
	import type { DevMachine, DevMachineCheckout, DevMachineEvent, DevMachineLogChunk, DevMachinePolicy, DevMachineService, ResourceSample } from '$lib/types/dev-machine';
	import LoadingState from '$lib/components/shared/LoadingState.svelte';
	import ErrorState from '$lib/components/shared/ErrorState.svelte';
	import MachineStatusBadge from '$lib/features/dev-machines/MachineStatusBadge.svelte';
	import MachineTasksCard from '$lib/features/dev-machines/MachineTasksCard.svelte';
	import AppPortsCard from '$lib/features/dev-machines/AppPortsCard.svelte';
	import MachineResizeDialog from '$lib/features/dev-machines/MachineResizeDialog.svelte';
	import MachineUsageCharts from '$lib/features/dev-machines/MachineUsageCharts.svelte';
	import RecentList from '$lib/features/dev-machines/RecentList.svelte';
	import { canResizeMachine, pendingResize } from '$lib/features/dev-machines/machine-resize';
	import { formatMachineResource, memoryGb } from '$lib/features/dev-machines/machine-size';
	import { authState } from '$lib/features/auth/auth.state.svelte';
	import { appendRecentTelemetry, DEV_MACHINE_EVENT_RETENTION, DEV_MACHINE_LOG_RETENTION } from '$lib/features/dev-machines/telemetry-retention';
	import { useTerminalDock } from '$lib/features/dev-machines/terminal-dock-context.svelte';
	import { appToast } from '$lib/features/toast/toast';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { m } from '$lib/paraglide/messages.js';
	import { getLocale, setLocale } from '$lib/paraglide/runtime.js';

	const slug = $derived(page.params.workspaceSlug ?? '');
	const machineId = $derived(page.params.machineId ?? '');
	let machine = $state<DevMachine | null>(null);
	let policy = $state<DevMachinePolicy | null>(null);
	// Plain (non-reactive) cache: reading reactive state inside refreshAll would make the route effect
	// depend on it and re-run on every assignment. The policy only labels the disk limit, so a failed
	// fetch falls back to the hard-limit label instead of blocking the page.
	let policyRequest: Promise<DevMachinePolicy | null> | null = null;
	let services = $state<DevMachineService[]>([]);
	let checkouts = $state<DevMachineCheckout[]>([]);
	let events = $state<DevMachineEvent[]>([]);
	let eventsLimited = $state(false);
	let eventsAfterId = 0;
	let logs = $state<DevMachineLogChunk[]>([]);
	let logsLimited = $state(false);
	let logsAfterId = 0;
	let tasks = $state<AgentQueueItem[]>([]);
	let tasksFailed = $state(false);
	// Resource history of the selected window, oldest first; refreshed every 30 s, not on every poll.
	let usage = $state<ResourceSample[]>([]);
	let usageLoaded = $state(false);
	let usageWindow = $state<ResourceUsageWindow>('1h');
	type MachineTab = 'overview' | 'work' | 'apps' | 'activity';
	let tab = $state<MachineTab>('overview');
	let loading = $state(true);
	let failed = $state(false);
	let actionBusy = $state(false);
	let teardownConfirm = $state(false);
	let deleteConfirm = $state(false);
	let snapshotOpen = $state(false);
	let resizeOpen = $state(false);
	let snapshotName = $state('');
	let snapshotBusy = $state(false);
	let checkoutAttempted = false;
	let workspaceRole = $state('');
	let launchBusy = $state<Record<string, boolean>>({});
	let terminalQueryConsumed = false;
	let routeGeneration = 0;
	let refreshSequence = 0;
	let pollGeneration = 0;
	let fragmentScrollVersion = 0;
	const dock = useTerminalDock();

	// Treat desired_status != status as pending ("transitioning")
	const isPending = $derived(machine ? machine.desired_status !== machine.status : false);

	const latestUsage = $derived(usage.at(-1));
	const diskUsageNearLimit = $derived(
		machine !== null &&
			latestUsage !== undefined &&
			machine.disk_gb > 0 &&
			latestUsage.disk_bytes >= machine.disk_gb * 1024 ** 3 * 0.9
	);
	const canAdminDevMachines = $derived(workspaceRole === 'owner' || workspaceRole === 'admin');
	// Resizing, like the other machine changes, is for the machine's creator.
	const canManageMachine = $derived(!!machine && (!machine.created_by_user_id || machine.created_by_user_id === authState.user?.id));
	const resizable = $derived(!!machine && canResizeMachine(machine));
	const canSnapshot = $derived(!!machine && machine.environment_builder && (machine.status === 'paused' || machine.status === 'stopped'));
	const pendingSize = $derived(machine ? pendingResize(machine) : null);
	const pendingSizeLabel = $derived(
		pendingSize
			? m['machines.resource_summary']({
					cpu: formatMachineResource(pendingSize.cpu_millis / 1000, m['machines.cpu_unit']()),
					memory: formatMachineResource(memoryGb(pendingSize.memory_mb), m['machines.memory_summary_unit']()),
					disk: formatMachineResource(pendingSize.disk_gb, m['machines.disk_summary_unit']())
				})
			: ''
	);
	const sizeLabel = $derived(
		machine
			? m['machines.resource_summary']({
					cpu: formatMachineResource(machine.cpu_millis / 1000, m['machines.cpu_unit']()),
					memory: formatMachineResource(memoryGb(machine.memory_mb), m['machines.memory_summary_unit']()),
					disk: formatMachineResource(machine.disk_gb, m['machines.disk_summary_unit']())
				})
			: ''
	);
	// Events arrive oldest first; the activity list shows the newest first.
	const newestEvents = $derived(events.slice().reverse());
	const dateTime = (value: string) =>
		new Date(value).toLocaleString(getLocale(), { dateStyle: 'medium', timeStyle: 'short' });
	const ideService = $derived(services.find((item) => item.service_type === 'ide'));
	const terminalService = $derived(services.find((item) => item.service_type === 'terminal'));

	$effect(() => {
		const targetSlug = slug;
		const targetMachineId = machineId;
		const generation = ++routeGeneration;
		resetMachineState();
		if (!targetSlug || !targetMachineId) return;
		void load(targetSlug, targetMachineId, generation);
		const pollTimer = setInterval(() => void poll(targetSlug, targetMachineId, generation), 4000);
		return () => {
			if (routeGeneration === generation) routeGeneration++;
			clearInterval(pollTimer);
		};
	});

	$effect(() => {
		const hash = page.url.hash;
		const targetSlug = slug;
		const targetMachineId = machineId;
		const currentLoading = loading;
		if (currentLoading || (hash !== '#tasks' && hash !== '#activity')) return;
		// Deep links open the tab holding their section, then scroll to it.
		tab = hash === '#tasks' ? 'work' : 'activity';
		const version = ++fragmentScrollVersion;
		void scrollToFragment(hash.slice(1), targetSlug, targetMachineId, version);
		return () => {
			if (fragmentScrollVersion === version) fragmentScrollVersion++;
		};
	});

	$effect(() => {
		const targetSlug = slug;
		const targetMachineId = machineId;
		const window = usageWindow;
		if (!targetSlug || !targetMachineId) return;
		let current = true;
		const refreshUsage = async () => {
			try {
				const samples = await listResourceUsage(targetSlug, targetMachineId, window);
				if (current) usage = samples ?? [];
			} catch {
				// Keep the last good history on a failed background refresh.
			} finally {
				if (current) usageLoaded = true;
			}
		};
		void refreshUsage();
		// Samples arrive every 30 seconds; a 10 second refresh shows each soon.
		const usageTimer = setInterval(() => void refreshUsage(), 10_000);
		return () => {
			current = false;
			clearInterval(usageTimer);
		};
	});

	function resetMachineState() {
		machine = null;
		policy = null;
		policyRequest = null;
		services = [];
		checkouts = [];
		events = [];
		eventsLimited = false;
		eventsAfterId = 0;
		logs = [];
		logsLimited = false;
		logsAfterId = 0;
		tasks = [];
		tasksFailed = false;
		usage = [];
		usageLoaded = false;
		tab = 'overview';
		loading = true;
		failed = false;
		actionBusy = false;
		teardownConfirm = false;
		deleteConfirm = false;
		snapshotOpen = false;
		resizeOpen = false;
		snapshotName = '';
		snapshotBusy = false;
		checkoutAttempted = false;
		workspaceRole = '';
		launchBusy = {};
		terminalQueryConsumed = false;
		refreshSequence++;
		fragmentScrollVersion++;
	}

	async function scrollToFragment(targetId: string, targetSlug: string, targetMachineId: string, version: number) {
		for (let attempt = 0; attempt < 8; attempt++) {
			await tick();
			if (fragmentScrollVersion !== version || slug !== targetSlug || machineId !== targetMachineId || page.url.hash !== `#${targetId}`) return;
			const target = document.getElementById(targetId);
			if (target) {
				target.scrollIntoView({ block: 'start' });
				return;
			}
			await new Promise((resolve) => setTimeout(resolve, 50));
		}
	}

	function isCurrentRoute(targetSlug: string, targetMachineId: string, generation: number) {
		return routeGeneration === generation && slug === targetSlug && machineId === targetMachineId;
	}

	function isCurrentRefresh(targetSlug: string, targetMachineId: string, generation: number, sequence: number) {
		return isCurrentRoute(targetSlug, targetMachineId, generation) && refreshSequence === sequence;
	}

	async function load(targetSlug = slug, targetMachineId = machineId, generation = routeGeneration) {
		const sequence = ++refreshSequence;
		loading = true;
		failed = false;
		try {
			await refreshAll(targetSlug, targetMachineId, generation, sequence);
		} catch (error) {
			if (!isCurrentRefresh(targetSlug, targetMachineId, generation, sequence)) return;
			failed = true;
			appToast.apiError(error, 'Failed to load Dev Machine');
		} finally {
			if (isCurrentRefresh(targetSlug, targetMachineId, generation, sequence)) loading = false;
		}
	}

	async function refreshAll(targetSlug = slug, targetMachineId = machineId, generation = routeGeneration, sequence = ++refreshSequence) {
		policyRequest ??= getDevMachinePolicy(targetSlug).catch(() => null);
		const [nextMachine, nextPolicy] = await Promise.all([getDevMachine(targetSlug, targetMachineId), policyRequest]);
		if (!isCurrentRefresh(targetSlug, targetMachineId, generation, sequence)) return;
		machine = nextMachine;
		policy = nextPolicy;
		const [serviceResult, checkoutResult, tasksResult, workspaceResult] = await Promise.allSettled([
			listMachineServices(targetSlug, targetMachineId),
			listMachineCheckouts(targetSlug, targetMachineId),
			listMachineTasks(targetSlug, targetMachineId),
			workspaceRole ? Promise.resolve(null) : getWorkspace(targetSlug)
		]);
		if (!isCurrentRefresh(targetSlug, targetMachineId, generation, sequence)) return;
		if (serviceResult.status === 'fulfilled') services = serviceResult.value ?? [];
		if (checkoutResult.status === 'fulfilled') checkouts = checkoutResult.value ?? [];
		// Keep the last good list on a failed background refresh.
		if (tasksResult.status === 'fulfilled') {
			tasks = tasksResult.value ?? [];
			tasksFailed = false;
		} else tasksFailed = true;
		if (workspaceResult.status === 'fulfilled' && workspaceResult.value) workspaceRole = workspaceResult.value.current_user_role;
		if (!checkoutAttempted && nextMachine.status === 'running' && nextMachine.issue_id && checkouts.length === 0) {
			checkoutAttempted = true;
			try {
				const checkout = await checkoutIssue(targetSlug, targetMachineId, nextMachine.issue_id);
				if (!isCurrentRefresh(targetSlug, targetMachineId, generation, sequence)) return;
				checkouts = [checkout];
			} catch (error) {
				if (!isCurrentRefresh(targetSlug, targetMachineId, generation, sequence)) return;
				appToast.apiError(error, 'Set a development repository before preparing this issue');
			}
		}
		const eventCursor = eventsAfterId;
		const logCursor = logsAfterId;
		const [eventResult, logResult] = await Promise.allSettled([
			listMachineEvents(targetSlug, targetMachineId, eventCursor),
			listMachineLogs(targetSlug, targetMachineId, logCursor)
		]);
		if (!isCurrentRefresh(targetSlug, targetMachineId, generation, sequence)) return;
		const newEvents = eventResult.status === 'fulfilled' ? (eventResult.value ?? []) : [];
		const newLogs = logResult.status === 'fulfilled' ? (logResult.value ?? []) : [];
		if (newEvents.length > 0) {
			const retained = appendRecentTelemetry(events, newEvents, DEV_MACHINE_EVENT_RETENTION);
			events = retained.items;
			eventsLimited ||= retained.dropped > 0;
			eventsAfterId = Math.max(eventsAfterId, ...newEvents.map((event) => event.id));
		}
		if (newLogs.length > 0) {
			const retained = appendRecentTelemetry(logs, newLogs, DEV_MACHINE_LOG_RETENTION);
			logs = retained.items;
			logsLimited ||= retained.dropped > 0;
			logsAfterId = Math.max(logsAfterId, ...newLogs.map((log) => log.id));
		}
		const terminalParam = page.url.searchParams.get('terminal');
		if (terminalParam === '1' && !terminalQueryConsumed) {
			terminalQueryConsumed = true;
			const checkoutId = page.url.searchParams.get('checkout_id') ?? undefined;
			const checkout = checkoutId ? checkouts.find((c) => c.id === checkoutId) : undefined;
			dock.open({
				slug: targetSlug,
				machineId: nextMachine.id,
				machineName: nextMachine.name,
				checkoutId,
				checkoutLabel: checkout ? `${checkout.repository_full_name} - ${checkout.working_branch}` : undefined
			});
			const url = new URL(page.url);
			url.searchParams.delete('terminal');
			url.searchParams.delete('checkout_id');
			replaceState(url, page.state);
		}
	}

	async function poll(targetSlug = slug, targetMachineId = machineId, generation = routeGeneration) {
		if (pollGeneration === generation || !isCurrentRoute(targetSlug, targetMachineId, generation) || loading || actionBusy) return;
		pollGeneration = generation;
		try {
			await refreshAll(targetSlug, targetMachineId, generation);
		} catch {
			// Background poll failures are silent, keep stale data
		} finally {
			if (pollGeneration === generation) pollGeneration = 0;
		}
	}

	async function lifecycle(action: 'start' | 'pause' | 'stop') {
		const targetSlug = slug;
		const targetMachineId = machineId;
		const generation = routeGeneration;
		refreshSequence++;
		actionBusy = true;
		try {
			if (action === 'start') await startDevMachine(targetSlug, targetMachineId);
			if (action === 'pause') await pauseDevMachine(targetSlug, targetMachineId);
			if (action === 'stop') await stopDevMachine(targetSlug, targetMachineId);
			if (!isCurrentRoute(targetSlug, targetMachineId, generation)) return;
			appToast.success(`Machine ${action} queued`);
			await refreshAll(targetSlug, targetMachineId, generation);
		} catch (error) {
			if (!isCurrentRoute(targetSlug, targetMachineId, generation)) return;
			appToast.apiError(error, `Failed to ${action} machine`);
		} finally {
			if (isCurrentRoute(targetSlug, targetMachineId, generation)) actionBusy = false;
		}
	}

	async function launch(service: DevMachineService, checkoutId?: string) {
		if (!machine) return;
		const targetSlug = slug;
		const targetMachineId = machineId;
		const targetMachine = machine;
		const generation = routeGeneration;
		if (!serviceActionAvailable(service)) {
			appToast.warning(machine.status === 'paused' ? 'Resuming is only available for paused machines.' : 'Service is not ready. Wait until it reports healthy.');
			return;
		}
		if (service.service_type === 'terminal') {
			const checkout = checkoutId ? checkouts.find((c) => c.id === checkoutId) : undefined;
			dock.open({
				slug: targetSlug,
				machineId: targetMachine.id,
				machineName: targetMachine.name,
				checkoutId: checkoutId,
				checkoutLabel: checkout ? `${checkout.repository_full_name} - ${checkout.working_branch}` : undefined
			});
			return;
		}
		const busyKey = `${service.service_key}:${checkoutId ?? 'root'}`;
		const resumeToastId = `dev-machine-resume:${targetSlug}:${targetMachineId}:${busyKey}`;
		let resumeToastVisible = false;
		launchBusy[busyKey] = true;
		launchBusy = { ...launchBusy };
		const popup = window.open('about:blank', '_blank');
		if (popup) popup.opener = null;
		try {
			const result = await launchMachineServiceWithResume(targetSlug, targetMachineId, service.service_key, checkoutId, {
				onStatus: (status) => {
					if (isCurrentRoute(targetSlug, targetMachineId, generation)) {
						resumeToastVisible = true;
						appToast.info(status === 'resuming' ? 'Resuming paused Dev Machine…' : 'Waiting for Dev Machine…', { id: resumeToastId, duration: Number.POSITIVE_INFINITY });
					} else if (resumeToastVisible) {
						appToast.dismiss(resumeToastId);
					}
				}
			});
			if (!isCurrentRoute(targetSlug, targetMachineId, generation)) {
				popup?.close();
				if (resumeToastVisible) appToast.dismiss(resumeToastId);
				return;
			}
			if (popup && result.launch_url) {
				popup.location.replace(result.launch_url);
				if (resumeToastVisible) appToast.success('Dev Machine is ready', { id: resumeToastId });
			} else {
				appToast.warning('Pop-up blocked. Please allow pop-ups for this site.', resumeToastVisible ? { id: resumeToastId } : undefined);
			}
			await refreshAll(targetSlug, targetMachineId, generation);
		} catch (error) {
			popup?.close();
			if (!isCurrentRoute(targetSlug, targetMachineId, generation)) {
				if (resumeToastVisible) appToast.dismiss(resumeToastId);
				return;
			}
			appToast.apiError(error, `Failed to open ${service.service_type}`, { id: resumeToastId });
		} finally {
			if (isCurrentRoute(targetSlug, targetMachineId, generation)) {
				launchBusy[busyKey] = false;
				launchBusy = { ...launchBusy };
			}
		}
	}

	async function teardown() {
		const targetSlug = slug;
		const targetMachineId = machineId;
		const generation = routeGeneration;
		refreshSequence++;
		teardownConfirm = false;
		actionBusy = true;
		try {
			await teardownDevMachine(targetSlug, targetMachineId);
			if (!isCurrentRoute(targetSlug, targetMachineId, generation)) return;
			appToast.success('Machine teardown queued');
			await refreshAll(targetSlug, targetMachineId, generation);
		} catch (error) {
			if (!isCurrentRoute(targetSlug, targetMachineId, generation)) return;
			appToast.apiError(error, 'Failed to teardown machine');
		} finally {
			if (isCurrentRoute(targetSlug, targetMachineId, generation)) actionBusy = false;
		}
	}

	async function removeMachine() {
		const targetSlug = slug;
		const targetMachineId = machineId;
		const generation = routeGeneration;
		refreshSequence++;
		deleteConfirm = false;
		actionBusy = true;
		try {
			await deleteDevMachine(targetSlug, targetMachineId);
			if (!isCurrentRoute(targetSlug, targetMachineId, generation)) return;
			appToast.success('Dev Machine deletion requested');
			await goto(`/${targetSlug}/settings/dev-machines`);
		} catch (error) {
			if (!isCurrentRoute(targetSlug, targetMachineId, generation)) return;
			appToast.apiError(error, 'Failed to permanently delete machine');
		} finally {
			if (isCurrentRoute(targetSlug, targetMachineId, generation)) actionBusy = false;
		}
	}

	function openSnapshot() {
		snapshotName = `${machine?.name ?? 'Development'} environment`;
		snapshotOpen = true;
	}

	async function saveSnapshot() {
		if (!snapshotName.trim()) return;
		const targetSlug = slug;
		const targetMachineId = machineId;
		const generation = routeGeneration;
		const name = snapshotName.trim();
		snapshotBusy = true;
		try {
			await snapshotDevMachineEnvironment(targetSlug, { name, source_machine_id: targetMachineId });
			if (!isCurrentRoute(targetSlug, targetMachineId, generation)) return;
			appToast.success('Development environment snapshot queued');
			snapshotOpen = false;
		} catch (error) {
			if (!isCurrentRoute(targetSlug, targetMachineId, generation)) return;
			appToast.apiError(error, 'Failed to save development environment');
		} finally {
			if (isCurrentRoute(targetSlug, targetMachineId, generation)) snapshotBusy = false;
		}
	}

	function serviceHealthy(service: DevMachineService): boolean {
		return service.status === 'running' && ['healthy', 'running'].includes(service.health_status);
	}

	function machineCanAutoLaunch(): boolean {
		return !!machine && (machine.status === 'running' || machine.status === 'paused' || (['queued', 'spawning'].includes(machine.status) && machine.desired_status === 'running'));
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
	<ErrorState message={m['machines.failed_load_detail']()} onretry={load} />
{:else}
	<div class="flex h-full flex-col">
		<header class="flex min-h-[49px] items-center justify-between gap-3 border-b border-[var(--app-border)] px-4">
			<div class="flex min-w-0 items-center gap-2">
				<a href="/{slug}/settings/dev-machines" class="rounded p-1 hover:bg-[var(--color-bg-hover)]" aria-label={m['machines.back_to_machines']()}><ArrowLeft size={16} /></a>
				<Server size={15} class="hidden shrink-0 sm:block" />
				<span class="min-w-16 truncate text-sm font-medium">{machine.name}</span>
				<MachineStatusBadge status={machine.status} />
				{#if machine.profile === 'headless'}<span class="hidden rounded-full border border-[var(--app-border)] px-2 py-0.5 text-[10px] font-semibold uppercase text-[var(--color-text-tertiary)] sm:inline">{m['machines.profile_headless_short']()}</span>{/if}
				{#if machine.environment_builder}<span class="text-[10px] font-semibold uppercase text-[var(--app-accent)]">{m['machines.environment_builder']()}</span>{/if}
				{#if isPending}
					<span class="inline-flex items-center gap-1 rounded-full border border-blue-500/30 bg-blue-500/10 px-2 py-0.5 text-[10px] font-semibold uppercase text-blue-400">
						<Loader size={10} class="animate-spin" /> {m['machines.transitioning']()}
					</span>
				{/if}
				{#if pendingSize}
					<span data-testid="machine-resize-pending" class="hidden items-center gap-1 rounded-full border border-blue-500/30 bg-blue-500/10 px-2 py-0.5 text-[10px] font-semibold text-blue-400 sm:inline-flex">
						<Loader size={10} class="animate-spin" /> {m['machines.resize_pending']({ size: pendingSizeLabel })}
					</span>
				{/if}
			</div>
			<div class="flex items-center gap-1">
				{#if machine.status === 'running' && !isPending}
					<Button variant="ghost" size="icon-sm" disabled={actionBusy} onclick={() => lifecycle('pause')} title={m['machines.pause']()}><Pause size={15} /></Button>
					<Button variant="ghost" size="icon-sm" disabled={actionBusy} onclick={() => lifecycle('stop')} title={m['machines.stop']()}><Square size={15} /></Button>
				{/if}
				{#if (machine.status === 'paused' || machine.status === 'stopped' || machine.status === 'failed') && !isPending}
					<Button variant="ghost" size="icon-sm" disabled={actionBusy} onclick={() => lifecycle('start')} title={m['machines.start']()}><Play size={15} /></Button>
				{/if}
				<!-- Phones keep lifecycle actions visible and the rest in a menu, so the name stays readable. -->
				<div class="hidden items-center gap-1 sm:flex">
					{#if canSnapshot}
						<Button variant="ghost" size="icon-sm" disabled={actionBusy} onclick={openSnapshot} title={m['machines.save_environment']()}><Save size={15} /></Button>
					{/if}
					{#if canManageMachine}
						<Button variant="ghost" size="icon-sm" disabled={actionBusy || !resizable} onclick={() => (resizeOpen = true)} title={resizable ? m['machines.resize']() : m['machines.resize_unavailable']()} aria-label={m['machines.resize']()}><Gauge size={15} /></Button>
					{/if}
					<Button variant="ghost" size="icon-sm" disabled={actionBusy || machine.status === 'destroyed'} onclick={() => (teardownConfirm = true)} title={m['machines.teardown_runtime']()}><ServerOff size={15} /></Button>
					{#if canAdminDevMachines}
						<Button variant="destructive" size="icon-sm" disabled={actionBusy} onclick={() => (deleteConfirm = true)} title={m['machines.delete_permanently']()}><Trash2 size={15} /></Button>
					{/if}
				</div>
				<DropdownMenu.Root>
					<DropdownMenu.Trigger>
						{#snippet child({ props })}
							<Button {...props} variant="ghost" size="icon-sm" class="sm:hidden" disabled={actionBusy} aria-label={m['tasks.more_actions']()}><Ellipsis size={15} /></Button>
						{/snippet}
					</DropdownMenu.Trigger>
					<DropdownMenu.Content align="end">
						{#if canSnapshot}<DropdownMenu.Item onclick={openSnapshot}><Save />{m['machines.save_environment']()}</DropdownMenu.Item>{/if}
						{#if canManageMachine}<DropdownMenu.Item disabled={!resizable} onclick={() => (resizeOpen = true)}><Gauge />{m['machines.resize']()}</DropdownMenu.Item>{/if}
						<DropdownMenu.Item disabled={machine.status === 'destroyed'} onclick={() => (teardownConfirm = true)}><ServerOff />{m['machines.teardown_runtime']()}</DropdownMenu.Item>
						{#if canAdminDevMachines}<DropdownMenu.Item variant="destructive" onclick={() => (deleteConfirm = true)}><Trash2 />{m['machines.delete_permanently']()}</DropdownMenu.Item>{/if}
					</DropdownMenu.Content>
				</DropdownMenu.Root>
			</div>
		</header>

		<div class="min-h-0 flex-1 overflow-y-auto p-4 sm:p-6">
			<div class="w-full space-y-5">
				<section aria-label={m['machines.summary']()} class="rounded-xl border border-[var(--app-border)] bg-[var(--color-bg-secondary)] p-4">
					<dl class="grid grid-cols-2 gap-x-6 gap-y-3 lg:grid-cols-4" data-testid="machine-summary">
						<div class="min-w-0">
							<dt class="flex items-center gap-1.5 text-[10px] uppercase tracking-wider text-[var(--color-text-tertiary)]"><Cpu size={12} aria-hidden="true" />{m['machines.size']()}</dt>
							<dd class="mt-1 break-words text-sm font-medium">{sizeLabel}</dd>
						</div>
						<div class="min-w-0">
							<dt class="flex items-center gap-1.5 text-[10px] uppercase tracking-wider text-[var(--color-text-tertiary)]"><GitBranch size={12} aria-hidden="true" />{m['machines.repository_affinity']()}</dt>
							{#if machine.repo_owner && machine.repo_name}
								<dd class="mt-1 truncate text-sm font-medium">{machine.repo_owner}/{machine.repo_name}</dd>
								<dd class="mt-0.5 flex items-center gap-1 text-xs text-[var(--color-text-tertiary)]"><GitBranch size={12} />{checkouts.length} issue {checkouts.length === 1 ? 'branch' : 'branches'}</dd>
							{:else}
								<dd class="mt-1 text-sm text-[var(--color-text-tertiary)]">{m['machines.no_repository_attached']()}</dd>
							{/if}
						</div>
						<div class="min-w-0">
							<dt class="flex items-center gap-1.5 text-[10px] uppercase tracking-wider text-[var(--color-text-tertiary)]"><CalendarPlus size={12} aria-hidden="true" />{m['machines.created']()}</dt>
							<dd class="mt-1 break-words text-sm font-medium">{dateTime(machine.created_at)}</dd>
						</div>
						<div class="min-w-0">
							<dt class="flex items-center gap-1.5 text-[10px] uppercase tracking-wider text-[var(--color-text-tertiary)]"><Clock size={12} aria-hidden="true" />{m['machines.last_activity']()}</dt>
							<dd class="mt-1 break-words text-sm font-medium">{machine.last_activity_at ? dateTime(machine.last_activity_at) : m['machines.no_recent_activity']()}</dd>
						</div>
					</dl>
				</section>
				{#if pendingSize}
					<p class="text-xs text-blue-400 sm:hidden">{m['machines.resize_pending']({ size: pendingSizeLabel })}</p>
				{/if}
				{#if machine.resource_limit_kind && machine.resource_limit_message}
					<Alert.Root variant="destructive" data-testid="machine-resource-limit">
						<CircleAlert />
						<Alert.Title>{machine.resource_limit_kind === 'disk' ? m['machines.resource_limit_disk']() : m['machines.resource_limit_memory']()}</Alert.Title>
						<Alert.Description>
							<p>{machine.resource_limit_message}</p>
							{#if machine.resource_limit_at}<p class="text-xs">{m['machines.resource_limit_at']({ time: new Date(machine.resource_limit_at).toLocaleString() })}</p>{/if}
							{#if canManageMachine && resizable}<Button size="sm" variant="outline" class="mt-2" onclick={() => (resizeOpen = true)}><Gauge size={13} />{m['machines.resize']()}</Button>{/if}
						</Alert.Description>
					</Alert.Root>
				{/if}
				{#if machine.resize_error}
					<Alert.Root variant="destructive" data-testid="machine-resize-error">
						<CircleAlert />
						<Alert.Title>{m['machines.resize_rejected']()}</Alert.Title>
						<Alert.Description>{machine.resize_error}</Alert.Description>
					</Alert.Root>
				{/if}

				<Tabs.Root bind:value={tab} class="gap-4">
					<Tabs.List class="grid w-full grid-cols-2 group-data-horizontal/tabs:h-auto sm:inline-flex sm:w-fit sm:group-data-horizontal/tabs:h-8">
						<Tabs.Trigger value="overview"><LayoutDashboard aria-hidden="true" />{m['machines.tab_overview']()}</Tabs.Trigger>
						<Tabs.Trigger value="work"><GitBranch aria-hidden="true" />{m['machines.tab_work']()}{#if checkouts.length + tasks.length > 0}<span class="text-muted-foreground tabular-nums">{checkouts.length + tasks.length}</span>{/if}</Tabs.Trigger>
						<Tabs.Trigger value="apps"><AppWindow aria-hidden="true" />{m['machines.tab_apps']()}</Tabs.Trigger>
						<Tabs.Trigger value="activity"><Activity aria-hidden="true" />{m['machines.activity']()}</Tabs.Trigger>
					</Tabs.List>

					<Tabs.Content value="overview" class="space-y-5">
						<MachineUsageCharts
							{machine}
							samples={usage}
							bind:window={usageWindow}
							loaded={usageLoaded}
							diskLimitLabel={policy?.disk_quota_mode === 'monitored' ? m['machines.monitored_limit']({ disk: machine.disk_gb }) : m['machines.hard_limit']({ disk: machine.disk_gb })}
							diskNearLimit={diskUsageNearLimit}
						/>
						<Card.Root>
							<Card.Header>
								<Card.Title class="flex items-center gap-2 text-sm"><SquareTerminal class="size-4 text-muted-foreground" aria-hidden="true" />{m['machines.developer_environment']()}</Card.Title>
								<Card.Description>{machine.profile === 'headless' ? m['machines.developer_environment_headless_desc']() : m['machines.developer_environment_desc']()}</Card.Description>
							</Card.Header>
							<Card.Content>
								<div class="grid gap-3 sm:grid-cols-2">
									{#if machine.profile !== 'headless'}
										<Button variant="outline" class="h-auto justify-start gap-3 p-4" disabled={!ideService || !machineCanAutoLaunch() || (!!ideService && launchBusyFor(ideService))} onclick={() => ideService && launch(ideService)}>
											<Code2 class="size-5" />
											<span class="text-left"><span class="block text-sm font-medium">{m['machines.code_editor']()}</span><span class="block text-xs text-muted-foreground">{m['machines.code_editor_desc']()}</span></span>
										</Button>
									{/if}
									<Button variant="outline" class="h-auto justify-start gap-3 p-4" disabled={!terminalService || !machineCanAutoLaunch()} onclick={() => terminalService && launch(terminalService)}>
										<SquareTerminal class="size-5" />
										<span class="text-left"><span class="block text-sm font-medium">{m['machines.native_terminal']()}</span><span class="block text-xs text-muted-foreground">{m['machines.native_terminal_desc']()}</span></span>
									</Button>
								</div>
							</Card.Content>
						</Card.Root>
					</Tabs.Content>

					<Tabs.Content value="work" class="space-y-5">
						<section class="rounded-xl border border-[var(--app-border)] bg-[var(--color-bg-secondary)] p-4">
							<div class="mb-3"><h2 class="flex items-center gap-2 text-sm font-semibold"><GitBranch class="size-4 text-muted-foreground" aria-hidden="true" />{m['machines.issue_worktrees']()}</h2><p class="text-xs text-[var(--color-text-tertiary)]">{m['machines.issue_worktrees_desc']()}</p></div>
							<RecentList items={checkouts} title={m['machines.issue_worktrees']()} description={m['machines.issue_worktrees_desc']()}>
								{#snippet empty()}<p class="text-xs text-[var(--color-text-tertiary)]">{m['machines.no_issue_branches']()}</p>{/snippet}
								{#snippet content(shown)}
									<div class="space-y-2">
										{#each shown as checkout (checkout.id)}
											<div class="flex flex-col justify-between gap-3 rounded-lg border border-[var(--app-border)] p-3 sm:flex-row sm:items-center">
												<div class="min-w-0"><p class="truncate text-xs font-medium">{checkout.repository_full_name}</p><p class="mt-1 truncate text-[10px] text-[var(--color-text-tertiary)]">{checkout.working_branch} · {checkout.status}</p>{#if checkout.last_error}<p class="mt-1 text-[10px] text-red-400">{checkout.last_error}</p>{/if}</div>
												{#if checkout.status === 'ready' && machineCanAutoLaunch()}
													<div class="flex gap-2">{#if machine?.profile !== 'headless'}<Button size="sm" variant="outline" disabled={!ideService || (!!ideService && launchBusyFor(ideService, checkout.id))} onclick={() => { if (ideService) launch(ideService, checkout.id); }}><Code2 size={13} />{m['machines.code_editor']()}</Button>{/if}<Button size="sm" variant="outline" disabled={!terminalService} onclick={() => { if (terminalService) launch(terminalService, checkout.id); }}><SquareTerminal size={13} />{m['machines.terminal']()}</Button></div>
												{/if}
											</div>
										{/each}
									</div>
								{/snippet}
							</RecentList>
						</section>

						<MachineTasksCard {slug} {tasks} failed={tasksFailed} />
					</Tabs.Content>

					<Tabs.Content value="apps" class="space-y-5">
		<AppPortsCard {slug} {machineId} running={machine.status === 'running'} canManage={!machine.created_by_user_id || machine.created_by_user_id === authState.user?.id} />

						<section class="rounded-xl border border-[var(--app-border)] bg-[var(--color-bg-secondary)] p-4">
							<div class="mb-3"><h2 class="flex items-center gap-2 text-sm font-semibold"><Server class="size-4 text-muted-foreground" aria-hidden="true" />{m['machines.services']()}</h2><p class="text-xs text-[var(--color-text-tertiary)]">{m['machines.services_desc']()}</p></div>
							<RecentList items={services} limit={6} title={m['machines.services']()} description={m['machines.services_desc']()}>
								{#snippet content(shown, all)}
									<div class="grid gap-2 {all ? '' : 'md:grid-cols-2'}">
										{#each shown as service (service.id)}
											<div class="flex items-center justify-between rounded-lg border border-[var(--app-border)] p-3">
												<div>
													<p class="text-xs font-medium capitalize">{service.service_type.replace('_', ' ')}</p>
													<p class="mt-1 text-[10px] text-[var(--color-text-tertiary)]">{service.status} · {service.health_status}{#if service.health_message && service.health_status !== 'healthy'} · {service.health_message}{/if}</p>
												</div>
												{#if serviceActionAvailable(service) && service.service_type !== 'ide' && service.service_type !== 'terminal'}
													<Button variant="ghost" size="icon-sm" disabled={launchBusyFor(service)} onclick={() => launch(service)} title={m['machines.open_service']()}><ExternalLink size={14} /></Button>
												{:else if serviceActionAvailable(service) && (machine?.environment_builder || checkouts.length === 0) && (service.service_type === 'ide' || service.service_type === 'terminal') && !(machine?.profile === 'headless' && service.service_type === 'ide')}
													<Button variant="ghost" size="icon-sm" disabled={launchBusyFor(service)} onclick={() => launch(service)} title={service.service_type === 'ide' ? m['machines.open_code_editor']() : m['machines.open_terminal']()}>{#if service.service_type === 'ide'}<Code2 size={14} />{:else}<SquareTerminal size={14} />{/if}</Button>
												{/if}
											</div>
										{/each}
									</div>
								{/snippet}
							</RecentList>
						</section>
					</Tabs.Content>

					<Tabs.Content value="activity">
						<section id="activity" class="grid gap-5 lg:grid-cols-2 scroll-mt-4">
							<div class="rounded-xl border border-[var(--app-border)] bg-[var(--color-bg-secondary)] p-4">
								<h2 class="flex items-center gap-2 text-sm font-semibold"><Activity class="size-4 text-muted-foreground" aria-hidden="true" />{m['machines.activity']()} ({events.length})</h2>
								{#if eventsLimited}<p class="mt-1 text-[10px] text-[var(--color-text-tertiary)]" data-testid="machine-events-retention">{m['machines.events_retention']({ count: DEV_MACHINE_EVENT_RETENTION })}</p>{/if}
								<div class="mt-3">
									<RecentList items={newestEvents} limit={8} title={`${m['machines.activity']()} (${events.length})`}>
										{#snippet empty()}<p class="text-xs text-[var(--color-text-tertiary)]">{m['machines.no_activity']()}</p>{/snippet}
										{#snippet content(shown)}
											<div class="space-y-3">
												{#each shown as event (event.id)}
													<div class="border-l border-[var(--app-border)] pl-3">
														<p class="text-xs font-medium">{event.event_type.replaceAll('_', ' ')}</p>
														<p class="text-[10px] text-[var(--color-text-tertiary)]">{event.source} · {new Date(event.occurred_at).toLocaleString()}</p>
													</div>
												{/each}
											</div>
										{/snippet}
									</RecentList>
								</div>
							</div>
							<div class="rounded-xl border border-[var(--app-border)] bg-[var(--color-bg-secondary)] p-4">
								<h2 class="flex items-center gap-2 text-sm font-semibold"><ScrollText class="size-4 text-muted-foreground" aria-hidden="true" />{m['machines.logs']()} ({logs.length})</h2>
								{#if logsLimited}<p class="mt-1 text-[10px] text-[var(--color-text-tertiary)]" data-testid="machine-logs-retention">{m['machines.logs_retention']({ count: DEV_MACHINE_LOG_RETENTION })}</p>{/if}
								<div class="mt-3">
									<RecentList items={logs} limit={20} take="last" title={`${m['machines.logs']()} (${logs.length})`}>
										{#snippet empty()}<pre class="rounded-lg bg-black/30 p-3 text-[11px] leading-relaxed text-zinc-300">{m['machines.no_logs']()}</pre>{/snippet}
										{#snippet content(shown, all)}
											<pre class="overflow-x-auto whitespace-pre-wrap rounded-lg bg-black/30 p-3 text-[11px] leading-relaxed text-zinc-300 {all ? '' : 'max-h-96 overflow-y-auto'}">{shown.map((chunk) => `[${chunk.stream}] ${chunk.content}`).join('\n')}</pre>
										{/snippet}
									</RecentList>
								</div>
							</div>
						</section>
					</Tabs.Content>
				</Tabs.Root>
			</div>
		</div>
	</div>

	<AlertDialog.Root bind:open={teardownConfirm}><AlertDialog.Content><AlertDialog.Header><AlertDialog.Title>{m['machines.teardown_title']()}</AlertDialog.Title><AlertDialog.Description>{m['machines.teardown_desc']()}</AlertDialog.Description></AlertDialog.Header><AlertDialog.Footer><AlertDialog.Cancel>{m['common.cancel']()}</AlertDialog.Cancel><AlertDialog.Action onclick={teardown}>{m['machines.teardown_action']()}</AlertDialog.Action></AlertDialog.Footer></AlertDialog.Content></AlertDialog.Root>
	<AlertDialog.Root bind:open={deleteConfirm}><AlertDialog.Content><AlertDialog.Header><AlertDialog.Title>{m['machines.delete_title']()}</AlertDialog.Title><AlertDialog.Description>{m['machines.delete_desc']()}</AlertDialog.Description></AlertDialog.Header><AlertDialog.Footer><AlertDialog.Cancel>{m['common.cancel']()}</AlertDialog.Cancel><AlertDialog.Action variant="destructive" onclick={removeMachine} disabled={actionBusy}>{actionBusy ? m['machines.deleting']() : m['machines.delete_action']()}</AlertDialog.Action></AlertDialog.Footer></AlertDialog.Content></AlertDialog.Root>
	<MachineResizeDialog bind:open={resizeOpen} {slug} {machine} {policy} onresized={(updated) => (machine = updated)} />
	<Dialog.Root bind:open={snapshotOpen}><Dialog.Content class="sm:max-w-md"><Dialog.Header><Dialog.Title>{m['machines.snapshot_title']()}</Dialog.Title><Dialog.Description>{m['machines.snapshot_desc']()}</Dialog.Description></Dialog.Header><label class="space-y-1.5"><Label for="snapshot-name">{m['machines.environment_name']()}</Label><Input id="snapshot-name" bind:value={snapshotName} /></label><Dialog.Footer><Button variant="outline" onclick={() => (snapshotOpen = false)}>{m['common.cancel']()}</Button><Button onclick={saveSnapshot} disabled={snapshotBusy || !snapshotName.trim()}>{snapshotBusy ? m['machines.queuing']() : m['machines.save_snapshot']()}</Button></Dialog.Footer></Dialog.Content></Dialog.Root>
{/if}
