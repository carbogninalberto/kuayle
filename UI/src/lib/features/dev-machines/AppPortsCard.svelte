<script lang="ts">
	import { tick } from 'svelte';
	import {
		Bot,
		Check,
		ExternalLink,
		ListTodo,
		LoaderCircle,
		Pencil,
		Plug,
		Plus,
		Trash2,
		User,
		X
	} from '@lucide/svelte';
	import * as Card from '$lib/components/ui/card/index.js';
	import * as AlertDialog from '$lib/components/ui/alert-dialog/index.js';
	import * as Tooltip from '$lib/components/ui/tooltip/index.js';
	import { Badge } from '$lib/components/ui/badge/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import { exposeMachinePort, listMachinePorts, removeMachinePort, renameMachinePort } from '$lib/api/dev-machines';
	import type { AppPort } from '$lib/types/dev-machine';
	import { appToast, getErrorMessage } from '$lib/features/toast/toast';
	import { m } from '$lib/paraglide/messages.js';
	import { detectedPorts, portName, validAppPort } from './app-ports';
	import { openAppPort } from './open-app-port';

	let {
		slug,
		machineId,
		running,
		canManage
	}: {
		slug: string;
		machineId: string;
		running: boolean;
		// Only the machine creator manages and opens ports; admins may read them.
		canManage: boolean;
	} = $props();

	const POLL_MS = 10_000;
	const LABEL = /^[A-Za-z0-9 ._-]{0,40}$/;
	const titleId = $props.id();

	let ports = $state<AppPort[]>([]);
	let listening = $state<number[]>([]);
	let loaded = $state(false);
	let card = $state<HTMLElement | null>(null);
	let visible = $state(true);
	let newPort = $state<number | null>(null);
	let newLabel = $state('');
	let formError = $state('');
	let exposing = $state(false);
	let opening = $state<string | null>(null);
	let editing = $state<string | null>(null);
	let draft = $state('');
	let removing = $state<AppPort | null>(null);
	let removeOpen = $state(false);
	let request = 0;

	const detected = $derived(detectedPorts(listening, ports));
	const portValue = $derived(newPort ?? NaN);
	const draftValid = $derived(LABEL.test(draft.trim()));
	const newLabelValid = $derived(LABEL.test(newLabel.trim()));

	async function load(target: string) {
		const id = ++request;
		try {
			const list = await listMachinePorts(slug, target);
			if (id !== request || target !== machineId) return;
			ports = list.ports ?? [];
			listening = list.listening ?? [];
		} catch {
			// The next poll retries; a transient failure keeps the last list.
		} finally {
			if (id === request) loaded = true;
		}
	}

	$effect(() => {
		const target = machineId;
		ports = [];
		listening = [];
		loaded = false;
		void load(target);
	});

	// Poll only while the card is on screen and the tab is visible.
	$effect(() => {
		if (!card || typeof IntersectionObserver === 'undefined') return;
		const observer = new IntersectionObserver(([entry]) => (visible = entry.isIntersecting));
		observer.observe(card);
		return () => observer.disconnect();
	});
	$effect(() => {
		if (!visible) return;
		const target = machineId;
		const timer = setInterval(() => {
			if (document.visibilityState === 'visible') void load(target);
		}, POLL_MS);
		return () => clearInterval(timer);
	});

	function refusal(error: unknown, port: number): string {
		const code = (error as { error?: { code?: string } })?.error?.code;
		if (code === 'PORT_EXISTS') return m['machines.port_exists']({ port });
		if (code === 'PORT_RESERVED') return m['machines.port_reserved']({ port });
		return getErrorMessage(error, m['machines.port_error']());
	}

	async function expose(port: number, label?: string) {
		if (exposing || !validAppPort(port)) return;
		exposing = true;
		formError = '';
		try {
			const created = await exposeMachinePort(slug, machineId, label ? { port, label } : { port });
			ports = [...ports.filter((p) => p.id !== created.id), created].sort((a, b) => a.port - b.port);
			if (port === newPort) {
				newPort = null;
				newLabel = '';
			}
		} catch (error) {
			formError = refusal(error, port);
		} finally {
			exposing = false;
		}
	}

	function submit(event: SubmitEvent) {
		event.preventDefault();
		if (!newLabelValid) return;
		void expose(portValue, newLabel.trim() || undefined);
	}

	async function open(port: AppPort) {
		if (opening) return;
		opening = port.id;
		let announced = false;
		try {
			await openAppPort(slug, machineId, port.id, () => {
				if (!announced) appToast.info(m['machines.port_starting']());
				announced = true;
			});
		} catch (error) {
			appToast.apiError(error, m['machines.port_not_ready']());
		} finally {
			opening = null;
		}
	}

	async function startEdit(port: AppPort) {
		editing = port.id;
		draft = port.label ?? '';
		await tick();
		document.getElementById(`${titleId}-edit`)?.focus();
	}

	async function saveLabel(port: AppPort) {
		if (!draftValid) return;
		const label = draft.trim() || null;
		editing = null;
		if (label === port.label) return;
		try {
			const updated = await renameMachinePort(slug, machineId, port.id, label);
			ports = ports.map((p) => (p.id === updated.id ? updated : p));
		} catch (error) {
			appToast.apiError(error, m['machines.port_error']());
		}
	}

	function editKey(event: KeyboardEvent, port: AppPort) {
		if (event.key === 'Enter') {
			event.preventDefault();
			void saveLabel(port);
		} else if (event.key === 'Escape') {
			event.preventDefault();
			editing = null;
		}
	}

	function askRemove(port: AppPort) {
		removing = port;
		removeOpen = true;
	}

	async function remove() {
		const port = removing;
		removeOpen = false;
		if (!port) return;
		try {
			await removeMachinePort(slug, machineId, port.id);
			ports = ports.filter((p) => p.id !== port.id);
		} catch (error) {
			appToast.apiError(error, m['machines.port_error']());
		}
	}
</script>

{#snippet tip(label: string, button: import('svelte').Snippet<[Record<string, unknown>]>)}
	<Tooltip.Root>
		<Tooltip.Trigger>
			{#snippet child({ props })}{@render button(props)}{/snippet}
		</Tooltip.Trigger>
		<Tooltip.Content>{label}</Tooltip.Content>
	</Tooltip.Root>
{/snippet}

<Card.Root bind:ref={card} role="region" aria-labelledby={titleId}>
	<Card.Header>
		<Card.Title id={titleId} class="flex items-center gap-2 text-sm"
			><Plug class="size-4 text-muted-foreground" />{m['machines.ports']()}<Badge variant="secondary"
				>{ports.length}</Badge
			></Card.Title
		>
		<Card.Description>{m['machines.ports_desc']()}</Card.Description>
	</Card.Header>
	<Card.Content class="min-w-0 space-y-3">
		{#if ports.length}
			<ul class="divide-y rounded-lg border">
				{#each ports as port (port.id)}
					{@const name = portName(port)}
					<li class="flex min-w-0 items-center gap-2 px-3 py-2">
						<span
							class="size-2 shrink-0 rounded-full {port.listening
								? 'bg-[var(--status-success)]'
								: 'bg-muted-foreground/40'}"
							title={port.listening ? m['machines.port_listening']() : m['machines.port_not_listening']()}
							><span class="sr-only"
								>{port.listening ? m['machines.port_listening']() : m['machines.port_not_listening']()}</span
							></span
						>
						{#if editing === port.id}
							<Input
								id={`${titleId}-edit`}
								class="h-7 min-w-0 flex-1"
								aria-label={m['machines.port_label_for']({ name: `:${port.port}` })}
								aria-invalid={!draftValid}
								maxlength={40}
								placeholder={`:${port.port}`}
								bind:value={draft}
								onkeydown={(event) => editKey(event, port)}
							/>
							<Button
								variant="ghost"
								size="icon-sm"
								aria-label={m['machines.port_save_label']()}
								disabled={!draftValid}
								onclick={() => void saveLabel(port)}><Check /></Button
							>
							<Button variant="ghost" size="icon-sm" aria-label={m['common.cancel']()} onclick={() => (editing = null)}
								><X /></Button
							>
						{:else}
							<div class="flex min-w-0 flex-1 items-center gap-2">
								<span class="truncate font-medium">{name}</span>
								{#if port.label}<span class="shrink-0 font-mono text-xs text-muted-foreground">:{port.port}</span>{/if}
								{#if port.task_identifier}
									<!-- A task port is labelled with its issue: the badge then shows only its icon. -->
									<Badge
										variant="outline"
										class="shrink-0"
										href={`/${slug}/issue/${port.task_identifier}`}
										aria-label={port.task_identifier}
										title={port.task_identifier}
										>{#if port.source === 'agent'}<Bot />{:else}<ListTodo
											/>{/if}{#if name !== port.task_identifier}{port.task_identifier}{/if}</Badge
									>
								{:else if port.source === 'agent'}
									<Badge variant="outline" class="shrink-0"><Bot />{m['machines.port_source_agent']()}</Badge>
								{:else if port.source === 'user'}
									<Badge variant="outline" class="shrink-0"><User />{m['machines.port_source_user']()}</Badge>
								{/if}
							</div>
							{#if canManage}
								<div class="flex shrink-0 items-center gap-0.5">
									<Button
										size="sm"
										variant="outline"
										aria-label={m['machines.port_open_named']({ name })}
										disabled={!running || opening !== null}
										onclick={() => void open(port)}
										>{#if opening === port.id}<LoaderCircle class="animate-spin" />{:else}<ExternalLink />{/if}<span
											class="hidden sm:inline">{m['machines.port_open']()}</span
										></Button
									>
									{#snippet renameButton(props: Record<string, unknown>)}
										<Button
											{...props}
											variant="ghost"
											size="icon-sm"
											aria-label={m['machines.port_rename']({ name })}
											onclick={() => void startEdit(port)}><Pencil /></Button
										>
									{/snippet}
									{#snippet removeButton(props: Record<string, unknown>)}
										<Button
											{...props}
											variant="ghost"
											size="icon-sm"
											class="text-destructive hover:text-destructive"
											aria-label={m['machines.port_remove']({ name })}
											onclick={() => askRemove(port)}><Trash2 /></Button
										>
									{/snippet}
									{@render tip(m['machines.port_rename']({ name }), renameButton)}
									{@render tip(m['machines.port_remove']({ name }), removeButton)}
								</div>
							{/if}
						{/if}
					</li>
				{/each}
			</ul>
		{:else if loaded}
			<p class="text-xs text-muted-foreground">{m['machines.ports_empty']()}</p>
		{/if}

		{#if canManage && detected.length}
			<div class="flex flex-wrap items-center gap-1.5" role="group" aria-label={m['machines.ports_detected']()}>
				<span class="text-xs text-muted-foreground">{m['machines.ports_detected_short']()}</span>
				{#each detected as port (port)}
					<Button
						variant="outline"
						size="xs"
						class="font-mono"
						aria-label={m['machines.port_expose_detected']({ port })}
						disabled={exposing}
						onclick={() => void expose(port)}><Plus />{port}</Button
					>
				{/each}
			</div>
		{/if}

		{#if canManage}
			<form class="flex min-w-0 items-center gap-2" onsubmit={submit}>
				<Input
					class="w-20 shrink-0 font-mono"
					type="number"
					inputmode="numeric"
					min="1024"
					max="65535"
					placeholder="3000"
					aria-label={m['machines.port_number']()}
					bind:value={newPort}
				/>
				<Input
					class="min-w-0 flex-1"
					maxlength={40}
					placeholder={m['machines.port_label_placeholder']()}
					aria-label={m['machines.port_label']()}
					aria-invalid={!newLabelValid}
					bind:value={newLabel}
				/>
				<Button type="submit" variant="outline" disabled={exposing || !validAppPort(portValue) || !newLabelValid}
					><Plus />{m['machines.port_expose']()}</Button
				>
			</form>
			{#if !newLabelValid || (editing && !draftValid)}<p class="text-xs text-destructive">
					{m['machines.port_label_invalid']()}
				</p>{/if}
			{#if formError}<p role="alert" class="text-xs text-destructive">{formError}</p>{/if}
		{/if}
	</Card.Content>
</Card.Root>

<AlertDialog.Root bind:open={removeOpen}>
	<AlertDialog.Content>
		<AlertDialog.Header>
			<AlertDialog.Title
				>{m['machines.port_remove_title']({ name: removing ? portName(removing) : '' })}</AlertDialog.Title
			>
			<AlertDialog.Description>{m['machines.port_remove_desc']()}</AlertDialog.Description>
		</AlertDialog.Header>
		<AlertDialog.Footer>
			<AlertDialog.Cancel>{m['common.cancel']()}</AlertDialog.Cancel>
			<AlertDialog.Action variant="destructive" onclick={() => void remove()}>{m['common.remove']()}</AlertDialog.Action
			>
		</AlertDialog.Footer>
	</AlertDialog.Content>
</AlertDialog.Root>
