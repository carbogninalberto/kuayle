<script lang="ts">
	import { ArrowUpRight, ListTree } from '@lucide/svelte';
	import * as Trace from '$lib/components/ui/trace/index.js';
	import type { ConversationToolCall } from '$lib/api/conversations';
	import { m } from '$lib/paraglide/messages.js';
	let { calls, slug }: { calls: ConversationToolCall[]; slug: string } = $props();
	type Source = { identifier: string; title: string };
	const identifierPattern = /^[A-Z][A-Z0-9]*-\d+$/;
	// Only issues that tools actually returned to this reply are listed.
	const sources = $derived.by(() => {
		const found = new Map<string, Source>();
		const add = (value: unknown) => {
			const issue = value as { identifier?: unknown; title?: unknown } | null;
			if (
				typeof issue?.identifier === 'string' &&
				identifierPattern.test(issue.identifier) &&
				!found.has(issue.identifier)
			)
				found.set(issue.identifier, {
					identifier: issue.identifier,
					title: typeof issue.title === 'string' ? issue.title : ''
				});
		};
		for (const call of calls) {
			if (call.status !== 'completed') continue;
			const output = call.output as { issues?: unknown } | undefined;
			if (Array.isArray(output?.issues)) output.issues.slice(0, 20).forEach(add);
			else add(call.output);
		}
		return [...found.values()];
	});
</script>

<!-- Official AI Elements Sources composition. -->
{#if sources.length}
	<Trace.Root class="min-w-0">
		<Trace.Collapsible>
			<Trace.CollapsibleTrigger>
				<Trace.Line>
					<Trace.Icon><ListTree /></Trace.Icon>
					<Trace.Title>{m['assistant.sources']({ count: sources.length })}</Trace.Title>
				</Trace.Line>
			</Trace.CollapsibleTrigger>
			<Trace.CollapsibleContent>
				<Trace.Root>
					{#each sources as source (source.identifier)}
						<a href="/{slug}/issue/{encodeURIComponent(source.identifier)}" class="block rounded-md hover:bg-muted">
							<Trace.Line>
								<Trace.Title class="truncate">{source.identifier}{source.title ? ` · ${source.title}` : ''}</Trace.Title
								>
								<Trace.Icon><ArrowUpRight aria-hidden="true" /></Trace.Icon>
							</Trace.Line>
						</a>
					{/each}
				</Trace.Root>
			</Trace.CollapsibleContent>
		</Trace.Collapsible>
	</Trace.Root>
{/if}
