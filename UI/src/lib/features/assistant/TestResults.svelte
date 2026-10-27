<script lang="ts">
	import { FlaskConical, CircleCheck, CircleAlert, CircleDashed, LoaderCircle } from '@lucide/svelte';
	import * as Trace from '$lib/components/ui/trace/index.js';
	import * as CodeBlock from '$lib/components/ui/code-block/index.js';
	import type { ValidationStep } from '$lib/api/agent-tasks';
	import { m } from '$lib/paraglide/messages.js';
	let { step, output }: { step: ValidationStep | null; output: string } = $props();
	const running = $derived(step?.status === 'queued' || step?.status === 'running');
	const failed = $derived(step?.status === 'failed' || step?.status === 'cancelled');
	const label = $derived(
		!step
			? m['tasks.check_unavailable']()
			: {
					queued: m['tasks.check_queued'](),
					running: m['tasks.check_running'](),
					succeeded: m['tasks.check_passed'](),
					failed: m['tasks.check_failed'](),
					cancelled: m['tasks.check_cancelled'](),
					skipped: m['tasks.check_skipped']()
				}[step.status]
	);
</script>

<!-- Official AI Elements Test Results composition: Trace + Code Block.
     A configured command is one check; no suite counts are inferred from text. -->
<section aria-label={m['tasks.checks']()} class="min-w-0">
	<Trace.Collapsible>
		<Trace.CollapsibleTrigger>
			<Trace.Line status={running ? 'running' : failed ? 'error' : undefined}>
				<Trace.Icon
					>{#if running}<LoaderCircle class="animate-spin" />{:else if failed}<CircleAlert
						/>{:else if step?.status === 'succeeded'}<CircleCheck />{:else}<CircleDashed />{/if}</Trace.Icon
				>
				<Trace.Title class="whitespace-normal break-words">{label}</Trace.Title>
			</Trace.Line>
		</Trace.CollapsibleTrigger>
		<Trace.CollapsibleContent class="space-y-2">
			{#if step}
				<CodeBlock.Root size="sm"
					><CodeBlock.Header
						><CodeBlock.Title><FlaskConical class="size-4" />{m['tasks.checks']()}</CodeBlock.Title></CodeBlock.Header
					><CodeBlock.Content code={JSON.stringify(step.command_argv)} wrap /></CodeBlock.Root
				>
				{#if step.exit_code != null}<p class="text-xs">{m['tasks.check_exit']({ code: step.exit_code })}</p>{/if}
				{#if step.started_at}<p class="text-xs text-muted-foreground">
						{m['tasks.check_started']({ time: new Date(step.started_at).toLocaleString() })}
					</p>{/if}
				{#if step.completed_at}<p class="text-xs text-muted-foreground">
						{m['tasks.check_finished']({ time: new Date(step.completed_at).toLocaleString() })}
					</p>{/if}
			{/if}
			{#if output}<CodeBlock.Root size="sm"
					><CodeBlock.Content code={output} wrap class="max-h-64 text-xs" /></CodeBlock.Root
				>{/if}
		</Trace.CollapsibleContent>
	</Trace.Collapsible>
</section>
