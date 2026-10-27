<script lang="ts">
	import { FlaskConical, CircleCheck, CircleAlert, CircleDashed, LoaderCircle, Download } from '@lucide/svelte';
	import { Button } from '$lib/components/ui/button/index.js';
	import { agentTasks, type ValidationArtifact } from '$lib/api/agent-tasks';
	import * as Trace from '$lib/components/ui/trace/index.js';
	import * as CodeBlock from '$lib/components/ui/code-block/index.js';
	import type { ValidationStep } from '$lib/api/agent-tasks';
	import { m } from '$lib/paraglide/messages.js';
	let { step, output, slug, taskId }: { step: ValidationStep | null; output: string; slug?: string; taskId?: string } =
		$props();
	let artifacts = $state<ValidationArtifact[]>([]);
	// Reports, screenshots and traces the check left behind, once it finished.
	$effect(() => {
		const finished = step && (step.status === 'succeeded' || step.status === 'failed');
		const run = step?.agent_run_id;
		if (!finished || !run || !slug || !taskId) {
			artifacts = [];
			return;
		}
		void agentTasks
			.artifacts(slug, taskId, run)
			.then((list) => {
				if (step?.agent_run_id === run) artifacts = list;
			})
			.catch(() => (artifacts = []));
	});
	// Always a file download: report HTML is untrusted and never rendered here.
	async function download(artifact: ValidationArtifact) {
		if (!slug || !taskId || !step) return;
		const { blob, filename } = await agentTasks.artifact(slug, taskId, step.agent_run_id, artifact.id);
		const url = URL.createObjectURL(new Blob([blob], { type: 'application/octet-stream' }));
		const link = document.createElement('a');
		link.href = url;
		link.download = filename ?? artifact.path.split('/').pop() ?? 'artifact';
		link.click();
		setTimeout(() => URL.revokeObjectURL(url), 1000);
	}
	const size = (bytes: number) =>
		bytes < 1024
			? `${bytes} B`
			: bytes < 1048576
				? `${Math.round(bytes / 1024)} KB`
				: `${(bytes / 1048576).toFixed(1)} MB`;
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
			{#if artifacts.length}
				<div class="space-y-1" aria-label={m['tasks.check_artifacts']()}>
					<p class="text-xs font-medium text-muted-foreground">{m['tasks.check_artifacts']()}</p>
					{#each artifacts as artifact (artifact.id)}
						<Button variant="ghost" size="sm" class="h-7 w-full justify-start" onclick={() => void download(artifact)}
							><Download /><span class="truncate">{artifact.path}</span><span
								class="ml-auto shrink-0 text-muted-foreground">{size(artifact.size)}</span
							></Button
						>
					{/each}
				</div>
			{/if}
		</Trace.CollapsibleContent>
	</Trace.Collapsible>
</section>
