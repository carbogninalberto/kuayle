<script lang="ts">
	import { CircleCheck } from '@lucide/svelte';
	import * as Trace from '$lib/components/ui/trace/index.js';
	import { m } from '$lib/paraglide/messages.js';
	let { summaries }: { summaries: string[] } = $props();
</script>

<!-- Official AI Elements Reasoning composition. Only provider-supplied
     summaries are shown, collapsed by default; nothing is synthesized. -->
<Trace.Root class="min-w-0">
	{#each summaries as summary, index (index)}
		<Trace.Collapsible>
			<Trace.CollapsibleTrigger>
				<Trace.Line>
					<Trace.Icon><CircleCheck /></Trace.Icon>
					<Trace.Title>{m['tasks.reasoning']()}</Trace.Title>
				</Trace.Line>
			</Trace.CollapsibleTrigger>
			<Trace.CollapsibleContent class="whitespace-pre-wrap break-words text-xs">{summary}</Trace.CollapsibleContent>
		</Trace.Collapsible>
	{/each}
</Trace.Root>
