<script lang="ts">
	import { ExternalLink, LoaderCircle, RefreshCw } from '@lucide/svelte';
	import { Button } from '$lib/components/ui/button/index.js';
	import { listMachinePorts } from '$lib/api/dev-machines';
	import type { AppPort } from '$lib/types/dev-machine';
	import { appToast } from '$lib/features/toast/toast';
	import { openAppPort } from '$lib/features/dev-machines/open-app-port';
	import { portName } from '$lib/features/dev-machines/app-ports';
	import { m } from '$lib/paraglide/messages.js';

	// Open buttons for a machine's app ports in the chat. The ports are read
	// from the API, never from tool output, and each opens through a one-time
	// launch ticket; the API decides who may list and open them.
	let {
		slug,
		machineId,
		port,
		taskId,
		wait = false
	}: {
		slug: string;
		machineId: string;
		// Only this port number, or only the port Kuayle started for this task's preview.
		port?: number;
		taskId?: string;
		// Keep checking while the port does not exist yet, as a starting preview's.
		wait?: boolean;
	} = $props();

	const pollMs = 3000,
		pollAttempts = 40;
	let ports = $state<AppPort[] | null>(null),
		failed = $state(false),
		polling = $state(false),
		opening = $state<string | null>(null),
		generation = $state(0);
	const wanted = (p: AppPort, number?: number, task?: string) =>
		(number === undefined || p.port === number) &&
		(task === undefined || (p.agent_task_id === task && p.source === 'task'));
	const shown = $derived((ports ?? []).filter((p) => wanted(p, port, taskId)));

	$effect(() => {
		const current = { slug, machineId, port, taskId, wait, generation };
		let stopped = false,
			timer: ReturnType<typeof setTimeout> | undefined;
		const matches = (list: AppPort[]) => list.some((p) => wanted(p, current.port, current.taskId));
		const attempt = async (left: number) => {
			try {
				const list = await listMachinePorts(current.slug, current.machineId);
				if (stopped) return;
				ports = list.ports;
				failed = false;
				if (current.wait && left > 0 && !matches(list.ports)) {
					polling = true;
					timer = setTimeout(() => void attempt(left - 1), pollMs);
					return;
				}
			} catch {
				if (stopped) return;
				failed = true;
			}
			polling = false;
		};
		void attempt(current.wait ? pollAttempts : 0);
		return () => {
			stopped = true;
			clearTimeout(timer);
		};
	});

	async function open(target: AppPort) {
		if (opening) return;
		opening = target.id;
		let announced = false;
		try {
			await openAppPort(slug, machineId, target.id, () => {
				if (!announced) appToast.info(m['machines.port_starting']());
				announced = true;
			});
		} catch (error) {
			appToast.apiError(error, m['machines.port_not_ready']());
		} finally {
			opening = null;
		}
	}
</script>

<div class="flex flex-wrap items-center gap-1.5 text-xs" role="group" aria-label={m['assistant.port_links']()}>
	{#each shown as target (target.id)}
		<Button
			size="sm"
			variant="outline"
			aria-label={m['machines.port_open_named']({ name: portName(target) })}
			disabled={opening !== null}
			onclick={() => void open(target)}
			>{#if opening === target.id}<LoaderCircle class="animate-spin" />{:else}<ExternalLink />{/if}{portName(
				target
			)}</Button
		>
		<span class="min-w-0 truncate text-muted-foreground">{target.host}</span>
	{:else}
		{#if ports === null && !failed}
			<LoaderCircle class="size-3 animate-spin text-muted-foreground" />
		{:else if polling}
			<span class="flex items-center gap-1 text-muted-foreground"
				><LoaderCircle class="size-3 animate-spin" />{m['assistant.port_waiting']()}</span
			>
		{:else}
			<span class={failed ? 'text-destructive' : 'text-muted-foreground'}
				>{failed
					? m['assistant.port_load_failed']()
					: port === undefined && taskId === undefined
						? m['assistant.port_none']()
						: m['assistant.port_missing']()}</span
			>
			<Button size="sm" variant="ghost" onclick={() => generation++}
				><RefreshCw />{m['assistant.port_check_again']()}</Button
			>
		{/if}
	{/each}
</div>
