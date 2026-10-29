<script lang="ts" module>
	import type { AssistantToolParameters } from '$lib/api/assistant-tools';

	export interface ProposedTool {
		tool_id: string;
		name: string;
		description: string;
		parameters: AssistantToolParameters;
		script: string;
		interpreter: string;
		read_only: boolean;
		version: number;
		/** The tool's own status: `active` when the draft is an edit of an active tool. */
		tool_status: 'draft' | 'active';
	}

	const uuidPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

	/** The stored propose_tool result, or undefined when it is not a usable card. */
	export function proposedTool(output: unknown): ProposedTool | undefined {
		const card = output as Partial<Record<keyof ProposedTool, unknown>> | null | undefined;
		const parameters = card?.parameters as AssistantToolParameters | undefined;
		if (
			!card ||
			typeof card.tool_id !== 'string' ||
			!uuidPattern.test(card.tool_id) ||
			typeof card.name !== 'string' ||
			typeof card.description !== 'string' ||
			typeof card.script !== 'string' ||
			typeof card.version !== 'number' ||
			!parameters ||
			typeof parameters !== 'object' ||
			typeof parameters.properties !== 'object' ||
			parameters.properties === null
		)
			return undefined;
		return {
			tool_id: card.tool_id,
			name: card.name,
			description: card.description,
			parameters,
			script: card.script,
			interpreter: typeof card.interpreter === 'string' ? card.interpreter : 'sh',
			read_only: card.read_only === true,
			version: card.version,
			tool_status: card.tool_status === 'active' ? 'active' : 'draft'
		};
	}
</script>

<script lang="ts">
	import { Settings, Wrench } from '@lucide/svelte';
	import { Badge } from '$lib/components/ui/badge/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import * as CodeBlock from '$lib/components/ui/code-block/index.js';
	import * as Item from '$lib/components/ui/item/index.js';
	import { m } from '$lib/paraglide/messages.js';
	import { schemaSummary } from './tool-schema';

	// The assistant's proposed tool is a draft: the card shows its full script
	// and schema as escaped text and links to Settings, where an owner or admin
	// reviews and activates it.
	let { tool, slug, id }: { tool: ProposedTool; slug: string; id: string } = $props();

	const version = $derived(String(tool.version));
	const summary = $derived(
		tool.tool_status === 'active'
			? m['assistant.tool_draft_update']({ version })
			: m['assistant.tool_draft_new']({ version })
	);
	const parameters = $derived(schemaSummary(tool.parameters));
	const href = $derived(
		`/${encodeURIComponent(slug)}/settings/assistant?tab=custom-tools&tool=${encodeURIComponent(tool.tool_id)}`
	);
</script>

<article aria-labelledby="proposed-tool-{id}" class="min-w-0">
	<Item.Root variant="outline" size="sm" class="items-start">
		<Item.Media variant="icon"><Wrench /></Item.Media>
		<Item.Content class="min-w-0 gap-1.5">
			<Item.Title class="flex flex-wrap items-center gap-2">
				<span id="proposed-tool-{id}" class="break-all">{m['assistant.tool_proposed_title']({ name: tool.name })}</span>
				<Badge variant="outline" class="border-[var(--status-warning)]/40 font-normal text-[var(--status-warning)]"
					>{m['assistant.tool_proposed_badge']()}</Badge
				>
			</Item.Title>
			<Item.Description class="line-clamp-none break-words">{tool.description}</Item.Description>
			<p class="text-xs text-muted-foreground">{summary}</p>
			<p class="text-xs break-words">
				<span class="text-muted-foreground">{m['assistant.tool_parameters']()}</span>
				<code class="font-mono">{parameters || m['assistant_custom_tools.no_parameters']()}</code>
			</p>
			{#if tool.read_only}<p class="text-xs text-muted-foreground">{m['assistant.tool_claims_read_only']()}</p>{/if}
			<CodeBlock.Root size="sm"
				><CodeBlock.Header
					><CodeBlock.Title>{m['assistant_custom_tools.script_title']({ interpreter: tool.interpreter })}</CodeBlock.Title
					></CodeBlock.Header
				><CodeBlock.Content code={tool.script} wrap class="max-h-72 text-xs" /></CodeBlock.Root
			>
			<div>
				<Button size="sm" variant="outline" {href}><Settings />{m['assistant.tool_review_link']()}</Button>
			</div>
		</Item.Content>
	</Item.Root>
</article>
