<script lang="ts">
	import * as Trace from '$lib/components/ui/trace/index.js';
	import type { ToolChipRendererProps } from './tool-chips';
	import { m } from '$lib/features/i18n/messages';
	import { repoScanOutput } from './research-output';

	let { call }: ToolChipRendererProps = $props();
	const scan = $derived(repoScanOutput(call.name, call.input, call.output));
</script>

<!-- What was scanned is shown so the step can be audited: repository, commit and the path or pattern used. -->
{#if call.status === 'failed'}
	<p class="text-xs text-muted-foreground">{call.error}</p>
{:else if scan}
	<Trace.Root class="min-w-0">
		<Trace.Line>
			<Trace.Title class="whitespace-normal break-words text-muted-foreground"
				>{m['assistant.research_scanned']({ repository: scan.repository, commit: scan.commit })}{scan.query
					? ` · ${scan.query}`
					: ''}</Trace.Title
			>
		</Trace.Line>
		{#if scan.source === 'task' && scan.branch}
			<Trace.Line>
				<Trace.Title class="whitespace-normal break-words text-muted-foreground"
					>{m['assistant.research_live_worktree']({ branch: scan.branch })}</Trace.Title
				>
			</Trace.Line>
		{/if}
		{#if scan.lines.length}
			<ul class="max-h-64 space-y-0.5 overflow-y-auto px-2 font-mono text-[11px]">
				{#each scan.lines as line, index (index)}
					<li class="truncate" title={line}>{line}</li>
				{/each}
			</ul>
		{/if}
		{#if scan.summary}<p class="px-2 text-[11px] text-muted-foreground">{scan.summary}</p>{/if}
	</Trace.Root>
{/if}
