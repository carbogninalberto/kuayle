<script module lang="ts">
	export type IssueMachineIntent = 'ide' | 'terminal' | 'agent';
</script>

<script lang="ts">
	import { ExternalLink, LoaderCircle, Plus, Settings2, SquareTerminal } from 'lucide-svelte';
	import * as Dialog from '$lib/components/ui/dialog';
	import { Button } from '$lib/components/ui/button';
	import {
		ensureIssueCheckoutReady,
		launchMachineServiceWithResume,
		listDevMachines,
		resumePausedMachine
	} from '$lib/api/dev-machines';
	import type { DevMachine } from '$lib/types/dev-machine';
	import type { Issue } from '$lib/types/issue';
	import MachineStatusBadge from './MachineStatusBadge.svelte';
	import { useTerminalDock } from './terminal-dock-context.svelte';

	let {
		open = $bindable(false),
		slug,
		issue,
		intent,
		oncreate,
		onrepository,
		onagent
	}: {
		open: boolean;
		slug: string;
		issue: Issue;
		intent: IssueMachineIntent;
		oncreate?: () => void;
		onrepository?: () => void;
		onagent?: (machine: DevMachine, checkoutId: string) => void;
	} = $props();

	const dock = useTerminalDock();
	let machines = $state<DevMachine[]>([]);
	let selectedMachineId = $state('');
	let loading = $state(false);
	let submitting = $state(false);
	let loadKey = '';
	let errorMessage = $state('');

	const selectedMachine = $derived(machines.find((machine) => machine.id === selectedMachineId));
	const actionLabel = $derived(intent === 'ide' ? 'Open Code Editor' : intent === 'terminal' ? 'Open Terminal' : 'Continue to Agent');

	$effect(() => {
		if (!open) {
			loadKey = '';
			return;
		}
		const key = `${slug}:${issue.id}:${intent}`;
		if (loadKey === key) return;
		loadKey = key;
		void loadMachines();
	});

	async function loadMachines() {
		loading = true;
		errorMessage = '';
		try {
			const response = await listDevMachines(slug, undefined, 1, 100);
			machines = (response.data ?? []).filter((machine) => !machine.delete_requested_at && !['destroyed', 'expired', 'tearing_down'].includes(machine.status));
			const preferred = machines.find((machine) => !disabledReason(machine));
			selectedMachineId = preferred?.id ?? '';
		} catch (error) {
			machines = [];
			errorMessage = messageFromError(error, 'Unable to load Dev Machines');
		} finally {
			loading = false;
		}
	}

	function disabledReason(machine: DevMachine): string {
		if (intent === 'agent' && (machine.status !== 'running' || machine.desired_status !== 'running')) {
			return 'Agent runs require a running machine';
		}
		if (machine.status === 'running' && machine.desired_status === 'running') return '';
		if (machine.status === 'paused' && (machine.desired_status === 'paused' || machine.desired_status === 'running')) return '';
		if (machine.status === 'stopped') return 'Start this machine first';
		if (machine.status === 'failed') return 'Machine cleanup or recovery is required';
		return 'Machine is still transitioning';
	}

	async function confirmSelection() {
		if (!selectedMachine || disabledReason(selectedMachine)) return;
		submitting = true;
		errorMessage = '';
		const popup = intent === 'ide' ? window.open('about:blank', '_blank') : null;
		if (popup) popup.opener = null;
		try {
			let machine = selectedMachine;
			if (machine.status === 'paused') {
				machine = await resumePausedMachine(slug, machine.id);
			}
			const checkout = await ensureIssueCheckoutReady(slug, machine.id, issue.id);
			if (intent === 'terminal') {
				dock.open({
					slug,
					machineId: machine.id,
					machineName: machine.name,
					checkoutId: checkout.id,
					checkoutLabel: `${checkout.repository_full_name} - ${checkout.working_branch}`
				});
			} else if (intent === 'agent') {
				onagent?.(machine, checkout.id);
			} else {
				const launch = await launchMachineServiceWithResume(slug, machine.id, 'ide', checkout.id);
				if (popup && launch.launch_url) popup.location.replace(launch.launch_url);
				else throw new Error('Pop-up blocked. Allow pop-ups for this site and try again.');
			}
			open = false;
		} catch (error) {
			popup?.close();
			errorMessage = messageFromError(error, 'Unable to prepare this Dev Machine');
		} finally {
			submitting = false;
		}
	}

	function openCreate() {
		open = false;
		oncreate?.();
	}

	function openRepositorySettings() {
		open = false;
		onrepository?.();
	}

	function messageFromError(error: unknown, fallback: string): string {
		if (error instanceof Error) return error.message;
		if (error && typeof error === 'object' && 'error' in error) {
			const payload = (error as { error?: { message?: unknown } }).error;
			if (typeof payload?.message === 'string') return payload.message;
		}
		return fallback;
	}
</script>

<Dialog.Root bind:open>
	<Dialog.Content class="sm:max-w-xl">
		<Dialog.Header>
			<Dialog.Title>Choose Dev Machine</Dialog.Title>
			<Dialog.Description>Select where to prepare {issue.identifier}. Checkout creation starts only after confirmation.</Dialog.Description>
		</Dialog.Header>

		{#if loading}
			<div class="flex min-h-40 items-center justify-center text-sm text-[var(--color-text-tertiary)]"><LoaderCircle class="mr-2 size-4 animate-spin" />Loading Dev Machines...</div>
		{:else}
			<div class="max-h-[min(50vh,420px)] space-y-2 overflow-y-auto pr-1" role="radiogroup" aria-label="Existing Dev Machines">
				{#each machines as machine (machine.id)}
					{@const reason = disabledReason(machine)}
					<button
						type="button"
						role="radio"
						aria-checked={selectedMachineId === machine.id}
						disabled={!!reason}
						class="flex w-full items-start justify-between gap-4 rounded-lg border p-3 text-left transition-colors {selectedMachineId === machine.id ? 'border-[var(--app-accent)] bg-[var(--app-accent)]/5' : 'border-[var(--app-border)] hover:bg-[var(--color-bg-hover)]'} disabled:cursor-not-allowed disabled:opacity-50"
						onclick={() => (selectedMachineId = machine.id)}
					>
						<span class="min-w-0">
							<span class="block truncate text-sm font-medium">{machine.name}</span>
							<span class="mt-1 block truncate text-xs text-[var(--color-text-tertiary)]">
								{machine.repo_owner && machine.repo_name ? `${machine.repo_owner}/${machine.repo_name}` : 'Available for a repository checkout'}
							</span>
							{#if reason}<span class="mt-1 block text-xs text-amber-500">{reason}</span>{/if}
						</span>
						<MachineStatusBadge status={machine.status} />
					</button>
				{/each}
				{#if machines.length === 0 && !errorMessage}
					<div class="rounded-lg border border-dashed border-[var(--app-border)] p-6 text-center text-sm text-[var(--color-text-tertiary)]">No reusable Dev Machines are available.</div>
				{/if}
			</div>
		{/if}

		{#if errorMessage}
			<div class="rounded-md border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-300">
				<p>{errorMessage}</p>
				{#if errorMessage.toLowerCase().includes('development repository')}
					<Button class="mt-2" size="sm" variant="outline" onclick={openRepositorySettings}><Settings2 class="size-3.5" />Set Development Defaults</Button>
				{/if}
			</div>
		{/if}

		<Dialog.Footer class="gap-2 sm:justify-between">
			<div class="flex gap-2">
				<Button variant="outline" onclick={openCreate}><Plus class="size-3.5" />New machine</Button>
				<Button variant="ghost" onclick={openRepositorySettings}><Settings2 class="size-3.5" />Defaults</Button>
			</div>
			<Button onclick={confirmSelection} disabled={!selectedMachine || !!disabledReason(selectedMachine) || submitting || loading}>
				{#if submitting}<LoaderCircle class="size-3.5 animate-spin" />{:else if intent === 'terminal'}<SquareTerminal class="size-3.5" />{:else}<ExternalLink class="size-3.5" />{/if}
				{submitting ? 'Preparing...' : actionLabel}
			</Button>
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>
