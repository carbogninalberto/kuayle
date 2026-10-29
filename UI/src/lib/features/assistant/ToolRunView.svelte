<script lang="ts" module>
	import { m } from '$lib/paraglide/messages.js';
	import type { AssistantToolRunStatus } from '$lib/api/assistant-tools';

	export const runStatusLabel = (status: AssistantToolRunStatus | string) =>
		status === 'queued'
			? m['assistant_custom_tools.run_queued']()
			: status === 'running'
				? m['assistant_custom_tools.run_running']()
				: status === 'succeeded'
					? m['assistant_custom_tools.run_succeeded']()
					: m['assistant_custom_tools.run_failed']();
</script>

<script lang="ts">
	import { Badge } from '$lib/components/ui/badge/index.js';
	import * as CodeBlock from '$lib/components/ui/code-block/index.js';

	// One custom tool run: status, exit code and output. Inputs and output are
	// untrusted text and are only rendered as escaped Code Block text.
	let {
		status,
		exitCode = null,
		output = '',
		truncated = false,
		error = null,
		durationMs = null,
		inputs = undefined
	}: {
		status: AssistantToolRunStatus | string;
		exitCode?: number | null;
		output?: string;
		truncated?: boolean;
		error?: string | null;
		durationMs?: number | null;
		inputs?: Record<string, unknown>;
	} = $props();

	const tone = $derived(
		status === 'succeeded'
			? 'border-[var(--status-success)]/40 text-[var(--status-success)]'
			: status === 'failed'
				? 'border-destructive/40 text-destructive'
				: 'text-muted-foreground'
	);
</script>

<div class="min-w-0 space-y-2">
	<div class="flex flex-wrap items-center gap-1.5 text-xs" role="status">
		<Badge variant="outline" class="font-normal {tone}">{runStatusLabel(status)}</Badge>
		{#if exitCode !== null}<Badge variant="outline" class="font-normal tabular-nums"
				>{m['assistant_custom_tools.exit_code']({ code: String(exitCode) })}</Badge
			>{/if}
		{#if durationMs !== null}<span class="text-muted-foreground tabular-nums"
				>{m['assistant_custom_tools.duration']({ seconds: (durationMs / 1000).toFixed(1) })}</span
			>{/if}
	</div>
	{#if error}<p class="text-xs break-words text-destructive">{error}</p>{/if}
	{#if inputs}
		<CodeBlock.Root size="sm"
			><CodeBlock.Header><CodeBlock.Title>{m['assistant_custom_tools.inputs']()}</CodeBlock.Title></CodeBlock.Header
			><CodeBlock.Content code={JSON.stringify(inputs, null, 2)} wrap class="max-h-40 text-xs" /></CodeBlock.Root
		>
	{/if}
	<CodeBlock.Root size="sm"
		><CodeBlock.Header><CodeBlock.Title>{m['assistant_custom_tools.output']()}</CodeBlock.Title></CodeBlock.Header
		><CodeBlock.Content
			code={output || m['assistant_custom_tools.output_empty']()}
			wrap
			class="max-h-64 text-xs"
		/></CodeBlock.Root
	>
	{#if truncated}<p class="text-xs text-muted-foreground">{m['assistant_custom_tools.output_truncated']()}</p>{/if}
</div>
