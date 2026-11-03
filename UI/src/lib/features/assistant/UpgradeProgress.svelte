<script lang="ts">
	import { CircleAlert, CircleCheck, LoaderCircle } from '@lucide/svelte';
	import * as Trace from '$lib/components/ui/trace/index.js';
	import { getSystemUpdateStatus, type SystemUpdateStatus } from '$lib/api/system';
	import { m } from '$lib/paraglide/messages.js';

	// Follows an approved upgrade through the same status route as Settings →
	// Version. The instance restarts during the run, so failed reads while the
	// state is unknown mean "waiting", not "failed".
	let { pollMs = 3000, attempts = 200 }: { pollMs?: number; attempts?: number } = $props();

	let status = $state<SystemUpdateStatus | null>(null),
		unreachable = $state(false);

	$effect(() => {
		let stopped = false,
			timer: ReturnType<typeof setTimeout> | undefined;
		const poll = async (left: number) => {
			try {
				status = await getSystemUpdateStatus();
				unreachable = false;
			} catch {
				unreachable = true;
			}
			if (stopped || left <= 0 || status?.state === 'succeeded' || status?.state === 'failed') return;
			timer = setTimeout(() => void poll(left - 1), pollMs);
		};
		void poll(attempts);
		return () => {
			stopped = true;
			clearTimeout(timer);
		};
	});

	const phase = $derived(status?.state);
</script>

{#if phase === 'succeeded'}
	<Trace.Line status="success">
		<Trace.Icon><CircleCheck /></Trace.Icon>
		<Trace.Title class="whitespace-normal break-words">{m['assistant.upgrade_progress_succeeded']()}</Trace.Title>
	</Trace.Line>
{:else if phase === 'failed'}
	<Trace.Line status="error">
		<Trace.Icon><CircleAlert /></Trace.Icon>
		<Trace.Title class="whitespace-normal break-words"
			>{m['assistant.upgrade_progress_failed']({ error: status?.error ?? '' })}</Trace.Title
		>
	</Trace.Line>
{:else}
	<Trace.Line status="running">
		<Trace.Icon><LoaderCircle class="animate-spin" /></Trace.Icon>
		<Trace.Title class="whitespace-normal break-words"
			>{unreachable ? m['assistant.upgrade_progress_waiting']() : m['assistant.upgrade_progress_running']()}</Trace.Title
		>
	</Trace.Line>
{/if}
