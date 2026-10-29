<script lang="ts">
	import { untrack } from 'svelte';
	import * as Dialog from '$lib/components/ui/dialog';
	import { Button } from '$lib/components/ui/button';
	import MachineResourceSliders from '$lib/features/dev-machines/MachineResourceSliders.svelte';
	import { resolveMachineSizeOptions } from '$lib/features/dev-machines/machine-size';
	import { resizeSizeOptions } from '$lib/features/dev-machines/machine-resize';
	import { updateDevMachine } from '$lib/api/dev-machines';
	import type { DevMachine, DevMachinePolicy } from '$lib/types/dev-machine';
	import { appToast, getErrorMessage } from '$lib/features/toast/toast';
	import { m } from '$lib/paraglide/messages.js';

	let {
		open = $bindable(false),
		slug,
		machine,
		policy,
		onresized
	}: {
		open: boolean;
		slug: string;
		machine: DevMachine;
		policy: DevMachinePolicy | null;
		onresized: (machine: DevMachine) => void;
	} = $props();

	let cpuMillis = $state(0);
	let memoryMb = $state(0);
	let diskGb = $state(0);
	let saving = $state(false);
	let error = $state('');

	const diskResizable = $derived(policy?.disk_quota_mode === 'monitored');
	const sizeOptions = $derived(
		resizeSizeOptions(
			resolveMachineSizeOptions(policy?.size_options, policy?.max_disk_gb ?? machine.disk_gb),
			policy?.max_disk_gb ?? machine.disk_gb,
			{ cpu_millis: machine.cpu_millis, memory_mb: machine.memory_mb, disk_gb: machine.disk_gb },
			diskResizable
		)
	);
	const unchanged = $derived(
		cpuMillis === machine.cpu_millis && memoryMb === machine.memory_mb && diskGb === machine.disk_gb
	);

	// Each opening starts from the machine's applied size; background refreshes
	// of the machine while the dialog is open keep the user's choice.
	$effect(() => {
		if (!open) return;
		untrack(() => {
			cpuMillis = machine.cpu_millis;
			memoryMb = machine.memory_mb;
			diskGb = machine.disk_gb;
			error = '';
		});
	});

	async function apply() {
		saving = true;
		error = '';
		try {
			const resized = await updateDevMachine(slug, machine.id, {
				cpu_millis: cpuMillis,
				memory_mb: memoryMb,
				disk_gb: diskGb
			});
			appToast.success(m['machines.resize_queued']());
			onresized(resized);
			open = false;
		} catch (cause) {
			error = getErrorMessage(cause, m['machines.resize_failed']());
		} finally {
			saving = false;
		}
	}
</script>

<Dialog.Root bind:open>
	<Dialog.Content class="max-h-[90dvh] overflow-y-auto sm:max-w-lg">
		<Dialog.Header>
			<Dialog.Title>{m['machines.resize_title']()}</Dialog.Title>
			<Dialog.Description>{m['machines.resize_desc']()}</Dialog.Description>
		</Dialog.Header>
		<MachineResourceSliders {sizeOptions} bind:cpu_millis={cpuMillis} bind:memory_mb={memoryMb} bind:disk_gb={diskGb} />
		<p class="text-xs text-muted-foreground">
			{diskResizable ? m['machines.resize_disk_monitored']() : m['machines.resize_disk_fixed']()}
		</p>
		{#if error}<p role="alert" class="text-sm text-destructive">{error}</p>{/if}
		<Dialog.Footer>
			<Button variant="outline" onclick={() => (open = false)}>{m['common.cancel']()}</Button>
			<Button onclick={apply} disabled={saving || unchanged}>
				{saving ? m['machines.resize_applying']() : m['machines.resize_apply']()}
			</Button>
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>
