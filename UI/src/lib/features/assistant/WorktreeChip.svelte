<script lang="ts">
	import * as Trace from '$lib/components/ui/trace/index.js';
	import type { ToolChipRendererProps } from './tool-chips';
	import { m } from '$lib/features/i18n/messages';
	import { machineWorktreesOutput, taskWorktreeOutput } from './research-output';

	let { call }: ToolChipRendererProps = $props();
	const status = $derived(taskWorktreeOutput(call.output));
	const worktrees = $derived(machineWorktreesOutput(call.output));
</script>

<!-- A task worktree's branch and state; a machine lists only how many it holds. -->
{#if call.status === 'failed'}
	<p class="text-xs text-muted-foreground">{call.error}</p>
{:else if status}
	<Trace.Root class="min-w-0">
		<Trace.Line>
			<Trace.Title class="whitespace-normal break-words text-muted-foreground"
				>{status.branch || '—'} · {status.dirty
					? m['assistant.research_worktree_dirty']()
					: m['assistant.research_worktree_clean']()} · {m['assistant.research_worktree_changed']({
					count: status.changed
				})}</Trace.Title
			>
		</Trace.Line>
	</Trace.Root>
{:else if worktrees}
	<Trace.Root class="min-w-0">
		<Trace.Line>
			<Trace.Title class="whitespace-normal break-words text-muted-foreground"
				>{m['assistant.research_worktrees_count']({ count: worktrees.count })}</Trace.Title
			>
		</Trace.Line>
	</Trace.Root>
{/if}
