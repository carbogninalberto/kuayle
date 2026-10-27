<script lang="ts">
	import { Wrench, CircleAlert, LoaderCircle, ExternalLink, Bot, CircleCheck, CircleX } from '@lucide/svelte';
	import * as Trace from '$lib/components/ui/trace/index.js';
	import * as CodeBlock from '$lib/components/ui/code-block/index.js';
	import * as Item from '$lib/components/ui/item/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import type { ConversationApproval, ConversationToolCall } from '$lib/api/conversations';
	import { m } from '$lib/paraglide/messages.js';
	import AgentSubagent from './AgentSubagent.svelte';
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
		update_issue: m['assistant.tool_update_issue'],
		delegate_issue: m['assistant.tool_delegate_issue'],
		plan_issues: m['assistant.tool_plan_issues'],
		link_issues: m['assistant.tool_link_issues'],
		get_agent_work: m['assistant.tool_get_agent_work'],
		merge_pull_request: m['assistant.tool_merge_pull_request']
	};
	// Outcome lines per approval action.
	function outcome(approval: ConversationApproval) {
		const identifier = approval.payload.identifier;
		const merge = approval.action === 'merge_pull_request';
		switch (approval.status) {
			case 'approving':
				return merge
					? m['assistant.merge_approving']({ identifier })
					: m['assistant.approval_approving']({ identifier });
			case 'approved':
				return merge ? m['assistant.merge_approved']({ identifier }) : m['assistant.approval_approved']({ identifier });
			case 'failed':
				return merge
					? m['assistant.merge_failed']({ identifier, error: approval.error ?? '' })
					: m['assistant.approval_failed']({ identifier, error: approval.error ?? '' });
			case 'expired':
				return m['assistant.approval_expired']({ identifier });
			default:
				return merge ? m['assistant.merge_denied']({ identifier }) : m['assistant.approval_denied']({ identifier });
		}
	}
	const label = (call: ConversationToolCall) => (names[call.name] ?? (() => call.name))();
	function created(call: ConversationToolCall) {
		const identifier = (call.output as { identifier?: unknown } | undefined)?.identifier;
		return (call.name === 'create_issue' || call.name === 'update_issue') &&
			call.status === 'completed' &&
			typeof identifier === 'string'
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
					<!-- A pending approval is the one thing waiting on the user: give it the warning tone. -->
					<Item.Root
						variant="outline"
						size="sm"
						class="items-start border-[var(--status-warning)]/40 bg-[var(--status-warning)]/5"
					>
						<Item.Media variant="icon" class="text-[var(--status-warning)]"><Bot /></Item.Media>
						<Item.Content class="min-w-0">
							{#if approval.action === 'merge_pull_request'}
								<Item.Title
									>{m['assistant.merge_title']({ identifier, number: approval.payload.pull_request })}</Item.Title
								>
								<Item.Description class="line-clamp-none"
									>{m['assistant.merge_description']({
										title: approval.payload.title,
										commit: approval.payload.commit.slice(0, 7),
										repository: approval.payload.repository
									})}</Item.Description
								>
								<a
									class="inline-flex items-center gap-1 text-xs underline underline-offset-2"
									href={approval.payload.pull_request_url}
									target="_blank"
									rel="noopener noreferrer"><ExternalLink size={12} />{m['tasks.open_pull_request']()}</a
								>
							{:else}
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
							{/if}
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
						status={approval.status === 'approving'
							? 'running'
							: approval.status === 'failed'
								? 'error'
								: approval.status === 'approved'
									? 'success'
									: 'warning'}
					>
						<Trace.Icon
							>{#if approval.status === 'approving'}<LoaderCircle
									class="animate-spin"
								/>{:else if approval.status === 'approved'}<CircleCheck
								/>{:else if approval.status === 'failed'}<CircleAlert />{:else}<CircleX />{/if}</Trace.Icon
						>
						<Trace.Title class="whitespace-normal break-words">{outcome(approval)}</Trace.Title>
					</Trace.Line>
					{#if approval.action === 'delegate_issue' && approval.status === 'approved' && approval.task_id}<AgentSubagent
							{slug}
							taskId={approval.task_id}
							{identifier}
						/>{/if}
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
						<Trace.Line status={failed ? 'error' : identifier ? 'success' : undefined}>
							<Trace.Icon
								>{#if failed}<CircleAlert />{:else}<Wrench />{/if}</Trace.Icon
							>
							<Trace.Title class="whitespace-normal break-words"
								>{identifier
									? call.name === 'update_issue'
										? m['assistant.tool_updated_issue']({ identifier })
										: m['assistant.tool_created_issue']({ identifier })
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
