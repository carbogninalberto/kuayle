<script lang="ts" module>
	/** The run summary a custom tool call or get_tool_run stores. */
	export interface ToolRunSummary {
		run_id?: string;
		tool: string;
		version?: number;
		status: string;
		exit_code: number | null;
		output_tail: string;
		output_truncated: boolean;
		error?: string;
		duration_ms: number | null;
	}

	/** A stored run result, or undefined when the output is not one. */
	export function toolRunSummary(output: unknown): ToolRunSummary | undefined {
		const run = output as Record<string, unknown> | null | undefined;
		if (!run || typeof run.status !== 'string' || typeof run.tool !== 'string') return undefined;
		return {
			run_id: typeof run.run_id === 'string' ? run.run_id : undefined,
			tool: run.tool,
			version: typeof run.version === 'number' ? run.version : undefined,
			status: run.status,
			exit_code: typeof run.exit_code === 'number' ? run.exit_code : null,
			output_tail: typeof run.output_tail === 'string' ? run.output_tail : '',
			output_truncated: run.output_truncated === true,
			error: typeof run.error === 'string' ? run.error : undefined,
			duration_ms: typeof run.duration_ms === 'number' ? run.duration_ms : null
		};
	}
</script>

<script lang="ts">
	import { onDestroy, untrack } from 'svelte';
	import { CircleAlert, CircleCheck, LoaderCircle } from '@lucide/svelte';
	import * as Trace from '$lib/components/ui/trace/index.js';
	import { assistantTools, type AssistantToolRun } from '$lib/api/assistant-tools';
	import { m } from '$lib/paraglide/messages.js';
	import ToolRunView from './ToolRunView.svelte';

	// A custom tool run in the chat. It starts from the stored result (an auto
	// run) or finds the run an approval started, and polls it until it ends.
	// The output is untrusted text, shown only as escaped Code Block text.
	let {
		slug,
		name,
		initial = undefined,
		toolId = undefined,
		approvalId = undefined
	}: {
		slug: string;
		/** The name the chat shows, custom_<name>. */
		name: string;
		initial?: ToolRunSummary;
		toolId?: string;
		approvalId?: string;
	} = $props();

	const pollMs = 2000;
	// Polling gives up after about a minute without a successful read.
	const lookups = 30;
	let run = $state<AssistantToolRun | null>(null);
	let timer: ReturnType<typeof setTimeout> | undefined;
	let stopped = false;

	const status = $derived(run?.status ?? initial?.status ?? 'queued');
	const finished = $derived(status === 'succeeded' || status === 'failed');
	const view = $derived(
		run
			? {
					exitCode: run.exit_code,
					output: run.output,
					truncated: run.output_truncated,
					error: run.error,
					durationMs: run.duration_ms
				}
			: initial
				? {
						exitCode: initial.exit_code,
						output: initial.output_tail,
						truncated: initial.output_truncated,
						error: initial.error ?? null,
						durationMs: initial.duration_ms
					}
				: null
	);
	const line = $derived(
		status === 'succeeded'
			? m['assistant.custom_run_succeeded']({ tool: name })
			: status === 'failed'
				? m['assistant.custom_run_failed_line']({ tool: name })
				: status === 'running'
					? m['assistant.custom_run_running']({ tool: name })
					: m['assistant.custom_run_queued']({ tool: name })
	);

	// `left` counts failed or empty reads in a row; a successful read resets it.
	async function tick(left: number) {
		if (stopped) return;
		let found = false;
		try {
			if (run?.id || initial?.run_id) run = await assistantTools.run(slug, run?.id ?? initial!.run_id!);
			else if (toolId && approvalId)
				run = (await assistantTools.runs(slug, toolId)).runs.find((entry) => entry.approval_id === approvalId) ?? null;
			found = run !== null;
		} catch {
			// Try again on the next tick; the stored result stays on screen.
		}
		if (stopped || (run && run.status !== 'queued' && run.status !== 'running')) return;
		const next = found ? lookups : left - 1;
		if (next > 0) timer = setTimeout(() => void tick(next), pollMs);
	}

	$effect(() => {
		// Only unfinished runs are polled; a finished stored result is final.
		if (initial && initial.status !== 'queued' && initial.status !== 'running') return;
		if (!initial?.run_id && !(toolId && approvalId)) return;
		untrack(() => void tick(lookups));
		return () => clearTimeout(timer);
	});
	onDestroy(() => {
		stopped = true;
		clearTimeout(timer);
	});
</script>

<div class="min-w-0 space-y-1.5">
	<Trace.Line status={status === 'succeeded' ? 'success' : status === 'failed' ? 'error' : 'running'}>
		<Trace.Icon
			>{#if status === 'succeeded'}<CircleCheck />{:else if status === 'failed'}<CircleAlert />{:else}<LoaderCircle
					class="animate-spin"
				/>{/if}</Trace.Icon
		>
		<Trace.Title class="break-words whitespace-normal">{line}</Trace.Title>
	</Trace.Line>
	{#if view && (finished || view.output)}
		<ToolRunView {status} {...view} />
	{:else if !view && !finished}
		<p class="text-xs text-muted-foreground">{m['assistant.custom_run_waiting']()}</p>
	{/if}
</div>
