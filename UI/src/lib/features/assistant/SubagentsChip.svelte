<script lang="ts">
	import { ArrowUpRight, CircleAlert, CircleCheck } from '@lucide/svelte';
	import * as Trace from '$lib/components/ui/trace/index.js';
	import type { ToolChipRendererProps } from './tool-chips';
	import { m } from '$lib/features/i18n/messages';
	import { httpURL, subagentsOutput, formatMs } from './research-output';

	let { call }: ToolChipRendererProps = $props();
	const output = $derived(subagentsOutput(call.output));
</script>

<!-- One nested row per subagent: title, model, duration; a failed one shows its error and does not hide the others. -->
{#if call.status === 'failed'}
	<p class="text-xs text-muted-foreground">{call.error}</p>
{:else if output}
	<Trace.Root class="min-w-0">
		<Trace.Line>
			<Trace.Title class="whitespace-normal break-words text-muted-foreground"
				>{m['assistant.research_subagents_summary']({
					completed: output.completed,
					failed: output.failed,
					duration: formatMs(output.duration_ms)
				})}</Trace.Title
			>
		</Trace.Line>
		{#each output.subagents as agent (agent.index)}
			{@const ok = agent.status === 'completed'}
			<Trace.Collapsible>
				<Trace.CollapsibleTrigger>
					<Trace.Line status={ok ? 'success' : 'error'}>
						<Trace.Icon
							>{#if ok}<CircleCheck />{:else}<CircleAlert />{/if}</Trace.Icon
						>
						<Trace.Title class="truncate">{agent.title}</Trace.Title>
						<Trace.Description class="shrink-0 tabular-nums"
							>{[agent.model, formatMs(agent.duration_ms)].filter(Boolean).join(' · ')}</Trace.Description
						>
					</Trace.Line>
				</Trace.CollapsibleTrigger>
				<Trace.CollapsibleContent class="space-y-1 px-2 text-xs">
					{#if agent.error}<p class="text-muted-foreground">{agent.error}</p>{/if}
					{#if agent.summary}<p class="whitespace-pre-wrap break-words">{agent.summary}</p>{/if}
					{#if agent.files.length}
						<ul class="font-mono text-[11px]">
							{#each agent.files as file, index (index)}<li class="truncate" title={file}>{file}</li>{/each}
						</ul>
					{/if}
					{#each agent.citations as citation, index (index)}
						{@const url = httpURL(citation.url)}
						{#if url}<a
								href={url}
								target="_blank"
								rel="noopener noreferrer nofollow"
								class="flex min-w-0 items-center gap-1 underline-offset-2 hover:underline"
								><span class="truncate">{citation.title || url}</span><ArrowUpRight
									size={12}
									aria-hidden="true"
									class="shrink-0"
								/></a
							>{/if}
					{/each}
				</Trace.CollapsibleContent>
			</Trace.Collapsible>
		{/each}
	</Trace.Root>
{/if}
