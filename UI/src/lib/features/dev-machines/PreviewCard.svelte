<script lang="ts">
	import { ExternalLink, Trash2, Save } from '@lucide/svelte';
	import * as Card from '$lib/components/ui/card';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import {
		deleteMachinePreview,
		getMachinePreview,
		launchMachinePreview,
		setMachinePreview,
		type DevMachinePreview
	} from '$lib/api/dev-machines';
	import { appToast } from '$lib/features/toast/toast';
	import { m } from '$lib/paraglide/messages.js';

	let { slug, machineId, running }: { slug: string; machineId: string; running: boolean } = $props();
	let preview = $state<DevMachinePreview | null>(null);
	let port = $state('');
	let busy = $state(false);

	$effect(() => {
		const target = machineId;
		void getMachinePreview(slug, target)
			.then((value) => {
				if (target !== machineId) return;
				preview = value;
				port = value ? String(value.port) : '';
			})
			.catch(() => (preview = null));
	});

	const portValue = $derived(Number(port));
	const validPort = $derived(Number.isInteger(portValue) && portValue >= 1024 && portValue <= 65535);

	async function save() {
		if (!validPort || busy) return;
		busy = true;
		try {
			preview = await setMachinePreview(slug, machineId, portValue);
			appToast.success(m['machines.preview_saved']());
		} catch (error) {
			appToast.apiError(error, m['machines.preview']());
		} finally {
			busy = false;
		}
	}

	async function open() {
		if (busy) return;
		busy = true;
		// Open synchronously to keep the user gesture; the launch URL carries a
		// one-time ticket bound to the preview host, never a reusable credential.
		const popup = window.open('about:blank', '_blank');
		if (popup) popup.opener = null;
		try {
			const launch = await launchMachinePreview(slug, machineId);
			if (launch.status !== 'ready' || !launch.launch_url) {
				popup?.close();
				appToast.warning(m['machines.preview_not_ready']());
				return;
			}
			if (popup) popup.location.replace(launch.launch_url);
		} catch (error) {
			popup?.close();
			appToast.apiError(error, m['machines.preview_open']());
		} finally {
			busy = false;
		}
	}

	async function remove() {
		if (busy) return;
		busy = true;
		try {
			await deleteMachinePreview(slug, machineId);
			preview = null;
			port = '';
		} catch (error) {
			appToast.apiError(error, m['machines.preview_remove']());
		} finally {
			busy = false;
		}
	}
</script>

<Card.Root>
	<Card.Header>
		<Card.Title class="text-sm">{m['machines.preview']()}</Card.Title>
		<Card.Description>{m['machines.preview_desc']()}</Card.Description>
	</Card.Header>
	<Card.Content class="flex flex-wrap items-end gap-3">
		<div class="grid gap-1.5">
			<Label for="machine-preview-port">{m['machines.preview_port']()}</Label>
			<Input
				id="machine-preview-port"
				class="w-32"
				type="number"
				inputmode="numeric"
				min="1024"
				max="65535"
				placeholder="5173"
				bind:value={port}
			/>
		</div>
		<Button variant="outline" disabled={busy || !validPort || preview?.port === portValue} onclick={() => void save()}
			><Save />{m['machines.preview_save']()}</Button
		>
		{#if preview}
			<Button disabled={busy || !running} onclick={() => void open()}
				><ExternalLink />{m['machines.preview_open']()}</Button
			>
			<Button variant="ghost" disabled={busy} onclick={() => void remove()}
				><Trash2 />{m['machines.preview_remove']()}</Button
			>
		{/if}
	</Card.Content>
</Card.Root>
