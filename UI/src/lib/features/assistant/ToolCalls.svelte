<script lang="ts">
	import { Wrench, CircleAlert, LoaderCircle, ExternalLink } from '@lucide/svelte';
	import * as Trace from '$lib/components/ui/trace/index.js';
	import * as CodeBlock from '$lib/components/ui/code-block/index.js';
	import type { ConversationToolCall } from '$lib/api/conversations';
	import { m } from '$lib/paraglide/messages.js';
	let { calls, slug }: { calls: ConversationToolCall[]; slug: string } = $props();
	const names: Record<string, () => string> = {
		list_teams: m['assistant.tool_list_teams'],
		search_issues: m['assistant.tool_search_issues'],
		get_issue: m['assistant.tool_get_issue'],
		create_issue: m['assistant.tool_create_issue']
	};
	const label = (call: ConversationToolCall) => (names[call.name] ?? (() => call.name))();
	function created(call: ConversationToolCall) {
		const identifier = (call.output as { identifier?: unknown } | undefined)?.identifier;
		return call.name === 'create_issue' && call.status === 'completed' && typeof identifier === 'string'
			? identifier
			: undefined;
	}
	// Tool data is untrusted: it is rendered only as escaped Code Block text.
	const pretty = (value: unknown) => JSON.stringify(value ?? null, null, 2);
</script>

<!-- Official AI Elements Tool composition: Trace rows, expandable Code Block output. -->
{#if calls.length}
	<Trace.Root class="min-w-0">
		{#each calls as call (call.id)}
			{#if call.status === 'running'}
				<Trace.Line status="running">
					<Trace.Icon><LoaderCircle class="animate-spin" /></Trace.Icon>
					<Trace.Title>{m['assistant.tool_running']({ tool: label(call) })}</Trace.Title>
				</Trace.Line>
			{:else}
				{@const failed = call.status === 'failed'}
				{@const identifier = created(call)}
				<Trace.Collapsible>
					<Trace.CollapsibleTrigger>
						<Trace.Line status={failed ? 'error' : undefined}>
							<Trace.Icon
								>{#if failed}<CircleAlert />{:else}<Wrench />{/if}</Trace.Icon
							>
							<Trace.Title class="whitespace-normal break-words"
								>{identifier
									? m['assistant.tool_created_issue']({ identifier })
									: failed
										? m['assistant.tool_failed']({ tool: label(call) })
										: m['assistant.tool_completed']({ tool: label(call) })}</Trace.Title
							>
						</Trace.Line>
					</Trace.CollapsibleTrigger>
					<Trace.CollapsibleContent class="space-y-2">
						<CodeBlock.Root size="sm"
							><CodeBlock.Header><CodeBlock.Title>{m['assistant.tool_input']()}</CodeBlock.Title></CodeBlock.Header
							><CodeBlock.Content code={pretty(call.input)} wrap class="max-h-48 text-xs" /></CodeBlock.Root
						>
						<CodeBlock.Root size="sm"
							><CodeBlock.Header><CodeBlock.Title>{m['assistant.tool_output']()}</CodeBlock.Title></CodeBlock.Header
							><CodeBlock.Content
								code={failed ? (call.error ?? '') : pretty(call.output)}
								wrap
								class="max-h-64 text-xs"
							/></CodeBlock.Root
						>
					</Trace.CollapsibleContent>
				</Trace.Collapsible>
				{#if identifier}<a
						class="inline-flex items-center gap-1 text-xs underline underline-offset-2"
						href="/{slug}/issue/{encodeURIComponent(identifier)}"><ExternalLink size={12} />{identifier}</a
					>{/if}
			{/if}
		{/each}
	</Trace.Root>
{/if}
