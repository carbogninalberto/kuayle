<script lang="ts">
	import { Wrench, CircleAlert, LoaderCircle, ExternalLink, Bot, CircleCheck, CircleX } from '@lucide/svelte';
	import * as Trace from '$lib/components/ui/trace/index.js';
	import * as CodeBlock from '$lib/components/ui/code-block/index.js';
	import * as Item from '$lib/components/ui/item/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import type { ConversationApproval, ConversationToolCall } from '$lib/api/conversations';
	import { m } from '$lib/paraglide/messages.js';
	let {
		calls,
		slug,
		approvals = {},
		onresolve
	}: {
		calls: ConversationToolCall[];
		slug: string;
		approvals?: Record<string, ConversationApproval>;
		onresolve?: (approval: ConversationApproval, decision: 'approve' | 'deny') => Promise<void>;
	} = $props();
	let deciding = $state<string | null>(null),
		decisionError = $state<string | null>(null);
	async function decide(approval: ConversationApproval, decision: 'approve' | 'deny') {
		if (!onresolve || deciding) return;
		deciding = approval.id;
		decisionError = null;
		try {
			await onresolve(approval, decision);
		} catch (error) {
			// An expiry is reported through the conversation stream as well.
			if ((error as { status?: number }).status !== 410) decisionError = approval.id;
		} finally {
			deciding = null;
		}
	}
	const names: Record<string, () => string> = {
		list_teams: m['assistant.tool_list_teams'],
		search_issues: m['assistant.tool_search_issues'],
		get_issue: m['assistant.tool_get_issue'],
		create_issue: m['assistant.tool_create_issue'],
		delegate_issue: m['assistant.tool_delegate_issue']
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
			{@const approval = approvals[call.id]}
			{#if approval}
				{@const identifier = approval.payload.identifier}
				{#if approval.status === 'pending'}
					<!-- Official AI Elements Confirmation composition: Item prompt with Deny/Approve. -->
					<Item.Root variant="outline" size="sm" class="items-start">
						<Item.Media variant="icon"><Bot /></Item.Media>
						<Item.Content class="min-w-0">
							<Item.Title>{m['assistant.approval_title']({ identifier })}</Item.Title>
							<Item.Description class="line-clamp-none"
								>{m['assistant.approval_description']({ title: approval.payload.title })}</Item.Description
							>
							<CodeBlock.Root size="sm"
								><CodeBlock.Content
									code={approval.payload.instructions}
									wrap
									class="max-h-48 text-xs"
								/></CodeBlock.Root
							>
							{#if decisionError === approval.id}<p role="alert" class="text-xs text-destructive">
									{m['assistant.approval_error']()}
								</p>{/if}
						</Item.Content>
						<Item.Actions>
							<Button
								size="sm"
								variant="outline"
								disabled={deciding !== null || !onresolve}
								onclick={() => void decide(approval, 'deny')}>{m['assistant.approval_deny']()}</Button
							>
							<Button
								size="sm"
								disabled={deciding !== null || !onresolve}
								onclick={() => void decide(approval, 'approve')}>{m['assistant.approval_approve']()}</Button
							>
						</Item.Actions>
					</Item.Root>
				{:else}
					<Trace.Line
						status={approval.status === 'approving' ? 'running' : approval.status === 'failed' ? 'error' : undefined}
					>
						<Trace.Icon
							>{#if approval.status === 'approving'}<LoaderCircle
									class="animate-spin"
								/>{:else if approval.status === 'approved'}<CircleCheck
								/>{:else if approval.status === 'failed'}<CircleAlert />{:else}<CircleX />{/if}</Trace.Icon
						>
						<Trace.Title class="whitespace-normal break-words"
							>{approval.status === 'approving'
								? m['assistant.approval_approving']({ identifier })
								: approval.status === 'approved'
									? m['assistant.approval_approved']({ identifier })
									: approval.status === 'failed'
										? m['assistant.approval_failed']({ identifier, error: approval.error ?? '' })
										: approval.status === 'expired'
											? m['assistant.approval_expired']({ identifier })
											: m['assistant.approval_denied']({ identifier })}</Trace.Title
						>
					</Trace.Line>
					{#if approval.status === 'approved'}<a
							class="inline-flex items-center gap-1 text-xs underline underline-offset-2"
							href="/{slug}/issue/{encodeURIComponent(identifier)}"><ExternalLink size={12} />{identifier}</a
						>{/if}
				{/if}
			{:else if call.status === 'running'}
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
