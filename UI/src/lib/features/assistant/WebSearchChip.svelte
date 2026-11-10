<script lang="ts">
	import { ArrowUpRight } from '@lucide/svelte';
	import * as Trace from '$lib/components/ui/trace/index.js';
	import type { ToolChipRendererProps } from './tool-chips';
	import { m } from '$lib/features/i18n/messages';
	import { httpURL, webSearchOutput } from './research-output';

	let { call }: ToolChipRendererProps = $props();
	const output = $derived(webSearchOutput(call.output));
</script>

<!-- Search results are untrusted: shown as text, linked only when the URL is http(s). -->
{#if call.status === 'failed'}
	<p class="text-xs text-muted-foreground">{call.error}</p>
{:else if output}
	<Trace.Root class="min-w-0">
		<Trace.Line>
			<Trace.Title class="whitespace-normal break-words text-muted-foreground"
				>{m['assistant.research_web_results']({ count: output.results.length, query: output.query })}</Trace.Title
			>
		</Trace.Line>
		{#each output.results as result, index (index)}
			{@const url = httpURL(result.url)}
			<div class="min-w-0 rounded-md px-2 py-1">
				{#if url}
					<a
						href={url}
						target="_blank"
						rel="noopener noreferrer nofollow"
						class="flex min-w-0 items-center gap-1 text-xs font-medium underline-offset-2 hover:underline"
					>
						<span class="truncate">{result.title || url}</span><ArrowUpRight
							size={12}
							aria-hidden="true"
							class="shrink-0"
						/>
					</a>
					<p class="truncate text-[11px] text-muted-foreground">{url}</p>
				{:else}
					<p class="truncate text-xs font-medium">{result.title}</p>
				{/if}
				{#if result.excerpt}<p class="line-clamp-2 text-xs text-muted-foreground">{result.excerpt}</p>{/if}
			</div>
		{/each}
		{#if output.truncated}<p class="px-2 text-[11px] text-muted-foreground">
				{m['assistant.research_truncated']()}
			</p>{/if}
	</Trace.Root>
{/if}
