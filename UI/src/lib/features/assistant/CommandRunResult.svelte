<script lang="ts">
	import { onDestroy, untrack } from 'svelte';
	import { CircleAlert, CircleCheck, LoaderCircle, Server } from '@lucide/svelte';
	import * as Trace from '$lib/components/ui/trace/index.js';
	import { conversations, type CommandRunResult } from '$lib/api/conversations';
	import { m } from '$lib/features/i18n/messages';
	import ToolRunView from './ToolRunView.svelte';
	import { commandMachineSize, commandRunPending } from './command-machine';

	// The command an approved run_command card started, polled until it ends.
	// It names the machine, its size and whether it was reused. The output is
	// untrusted text, shown only as escaped Code Block text.
	let {
		slug,
		tool,
		conversationId,
		approvalId
	}: { slug: string; tool: string; conversationId: string; approvalId: string } = $props();

	const pollMs = 2000;
	// A command stops after 60 seconds; polling gives up well after that.
	const lookups = 60;
	let run = $state<CommandRunResult | null>(null);
	let timer: ReturnType<typeof setTimeout> | undefined;
	let stopped = false;

	const status = $derived(run?.status ?? 'queued');
	const size = $derived(run ? commandMachineSize(run) : null);
	const line = $derived(
		status === 'succeeded'
			? m['assistant.custom_run_succeeded']({ tool })
			: status === 'failed'
				? m['assistant.custom_run_failed_line']({ tool })
				: status === 'running'
					? m['assistant.custom_run_running']({ tool })
					: m['assistant.custom_run_queued']({ tool })
	);

	async function tick(left: number) {
		if (stopped) return;
		try {
			run = await conversations.commandRun(slug, conversationId, approvalId);
		} catch {
			// Not queued yet, or a failed read: try again on the next tick.
		}
		if (stopped || !commandRunPending(run?.status)) return;
		if (left > 1) timer = setTimeout(() => void tick(left - 1), pollMs);
	}

	$effect(() => {
		untrack(() => void tick(lookups));
		return () => clearTimeout(timer);
	});
	onDestroy(() => {
		stopped = true;
		clearTimeout(timer);
	});
</script>

<div class="min-w-0 space-y-1.5 pl-6" data-testid="command-run-result">
	<Trace.Line status={status === 'succeeded' ? 'success' : status === 'failed' ? 'error' : 'running'}>
		<Trace.Icon
			>{#if status === 'succeeded'}<CircleCheck />{:else if status === 'failed'}<CircleAlert />{:else}<LoaderCircle
					class="animate-spin"
				/>{/if}</Trace.Icon
		>
		<Trace.Title class="break-words whitespace-normal">{line}</Trace.Title>
	</Trace.Line>
	{#if run && size}
		<p class="flex min-w-0 items-center gap-1 text-xs text-muted-foreground">
			<Server size={12} class="shrink-0" /><span class="break-words"
				>{m['assistant.command_machine']({ machine: run.machine, ...size })} · {run.reused
					? m['assistant.command_reused']()
					: run.temporary
						? m['assistant.command_temporary']()
						: m['assistant.command_own_machine']()}</span
			>
		</p>
		{#if !commandRunPending(run.status) || run.output_tail}
			<ToolRunView
				status={run.status}
				exitCode={run.exit_code}
				output={run.output_tail}
				truncated={run.output_truncated}
				error={run.error ?? null}
				durationMs={run.duration_ms}
			/>
		{/if}
	{/if}
</div>
