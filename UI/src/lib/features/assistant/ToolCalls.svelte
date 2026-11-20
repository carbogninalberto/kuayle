<script lang="ts">
	import {
		Wrench,
		CircleAlert,
		LoaderCircle,
		ExternalLink,
		CircleCheck,
		CircleX,
		BookOpenText,
		Hourglass,
		Workflow
	} from '@lucide/svelte';
	import * as Trace from '$lib/components/ui/trace/index.js';
	import * as CodeBlock from '$lib/components/ui/code-block/index.js';
	import type {
		ConversationApproval,
		ConversationToolCall,
		WikiApprovalPayload
	} from '$lib/api/conversations';
	import { isWikiApproval } from '$lib/api/conversations';
	import { m } from '$lib/features/i18n/messages';
	import AgentSubagent from './AgentSubagent.svelte';
	import AppPortLinks from './AppPortLinks.svelte';
	import AssistantProcessResult from './AssistantProcessResult.svelte';
	import UpgradeProgress from './UpgradeProgress.svelte';
	import { safePullRequestUrl } from './merged-work';
	import BulkIssueChanges from './BulkIssueChanges.svelte';
	import ShipSteps from './ShipSteps.svelte';
	import ProposedSkillCard, { proposedSkill } from './ProposedSkillCard.svelte';
	import ProposedToolCard, { proposedTool } from './ProposedToolCard.svelte';
	import CustomToolRunResult, { toolRunSummary } from './CustomToolRunResult.svelte';
	import CommandRunResult from './CommandRunResult.svelte';
	import { runStatusLabel } from './ToolRunView.svelte';
	import { agentInfo } from '$lib/features/agents/agents';
	import { toolChip, toolLabel } from './tool-chips';
	import './research-chips';
	import { elapsed, formatDuration, splitSteps } from './shell-rules';
	let {
		calls,
		slug,
		approvals = {},
		part = 'steps'
	}: {
		calls: ConversationToolCall[];
		slug: string;
		approvals?: Record<string, ConversationApproval>;
		// Render tool steps above the message and resolved decisions below it.
		part?: 'steps' | 'approvals';
	} = $props();
	function wikiSubject(payload: WikiApprovalPayload): string {
		return payload.title ? `${payload.scope_name} · ${payload.title}` : payload.scope_name;
	}
	// Outcome lines per approval action.
	function outcome(approval: ConversationApproval) {
		if (approval.action === 'bulk_update_issues') {
			const title = m['assistant.bulk_issues']({ count: approval.payload.changes.length });
			const status = {
				approved: m['assistant.completed'](), failed: approval.error ?? m['assistant.failed'](),
				denied: m['workflows.condition.approval_denied'](), expired: m['settings.tokens.expired'](),
				approving: m['tasks.state.running']()
			};
			return approval.status in status ? `${title} · ${status[approval.status as keyof typeof status]}` : title;
		}
		if (isWikiApproval(approval)) {
			const subject = wikiSubject(approval.payload);
			switch (approval.status) {
				case 'approving':
					return m['assistant.approval_approving']({ identifier: subject });
				case 'approved':
					return m['assistant.wiki_approved']({ subject });
				case 'failed':
					return m['assistant.wiki_failed']({ subject, error: approval.error ?? '' });
				case 'expired':
					return m['assistant.approval_expired']({ identifier: subject });
				default:
					return m['assistant.wiki_denied']({ subject });
			}
		}
		if (approval.action === 'create_release') {
			const tag = approval.payload.tag;
			switch (approval.status) {
				case 'approving':
					return m['assistant.release_approving']({ tag });
				case 'approved':
					return m['assistant.release_approved']({ tag });
				case 'failed':
					return m['assistant.release_failed']({ tag, error: approval.error ?? '' });
				case 'expired':
					return m['assistant.approval_expired']({ identifier: tag });
				default:
					return m['assistant.release_denied']({ tag });
			}
		}
		if (approval.action === 'ship_pull_request') {
			const { identifier } = approval.payload;
			switch (approval.status) {
				case 'approving':
					return m['assistant.ship_approving']({ identifier });
				case 'approved':
					return m['assistant.ship_approved']({ identifier, tag: approval.payload.release.tag });
				case 'failed':
					return m['assistant.ship_failed']({ identifier, error: approval.error ?? '' });
				case 'expired':
					return m['assistant.approval_expired']({ identifier });
				default:
					return m['assistant.ship_denied']({ identifier });
			}
		}
		if (approval.action === 'upgrade_instance') {
			const latest = approval.payload.latest_version;
			switch (approval.status) {
				case 'approving':
					return m['assistant.upgrade_approving']({ latest });
				case 'approved':
					return m['assistant.upgrade_approved']({ latest });
				case 'failed':
					return m['assistant.upgrade_failed']({ error: approval.error ?? '' });
				case 'expired':
					return m['assistant.approval_expired']({ identifier: latest });
				default:
					return m['assistant.upgrade_denied']({ latest });
			}
		}
		if (approval.action === 'start_temporary_machine') {
			switch (approval.status) {
				case 'approving':
					return m['assistant.temp_machine_approving']();
				case 'approved':
					return m['assistant.temp_machine_approved']();
				case 'failed':
					return m['assistant.temp_machine_failed']({ error: approval.error ?? '' });
				case 'expired':
					return m['assistant.approval_expired']({ identifier: 'start_temporary_machine' });
				default:
					return m['assistant.temp_machine_denied']();
			}
		}
		if (
			approval.action === 'run_command' ||
			approval.action === 'run_command_write' ||
			approval.action === 'run_migration'
		) {
			const identifier = approval.payload.machine,
				tool = approval.action;
			switch (approval.status) {
				case 'approving':
					return m['assistant.custom_run_approving']({ identifier, tool });
				case 'approved':
					return m['assistant.custom_run_approved']({ identifier, tool });
				case 'failed':
					return m['assistant.custom_run_failed']({ identifier, tool, error: approval.error ?? '' });
				case 'expired':
					return m['assistant.approval_expired']({ identifier });
				default:
					return m['assistant.custom_run_denied']({ identifier, tool });
			}
		}
		if (approval.action === 'start_process') {
			const machine = approval.payload.machine;
			switch (approval.status) {
				case 'approving':
					return m['assistant.process_approving']({ machine });
				case 'approved':
					return m['assistant.process_approved']({ machine });
				case 'failed':
					return m['assistant.process_approval_failed']({ machine, error: approval.error ?? '' });
				case 'expired':
					return m['assistant.approval_expired']({ identifier: machine });
				default:
					return m['assistant.process_denied']({ machine });
			}
		}
		const identifier = approval.payload.identifier;
		const merge = approval.action === 'merge_pull_request';
		if (
			approval.action === 'reply_to_review_comment' ||
			approval.action === 'resolve_review_thread' ||
			approval.action === 'update_pull_request_branch' ||
			approval.action === 'mark_pull_request_ready'
		) {
			switch (approval.status) {
				case 'approving':
					return m['assistant.pr_action_approving']({ identifier });
				case 'approved':
					return m['assistant.pr_action_approved']({ identifier });
				case 'failed':
					return m['assistant.pr_action_failed']({ identifier, error: approval.error ?? '' });
				case 'expired':
					return m['assistant.approval_expired']({ identifier });
				default:
					return m['assistant.pr_action_denied']({ identifier });
			}
		}
		if (approval.action === 'accept_agent_work') {
			switch (approval.status) {
				case 'approving':
					return m['assistant.accept_approving']({ identifier });
				case 'approved':
					return m['assistant.accept_approved']({ identifier });
				case 'failed':
					return m['assistant.accept_failed']({ identifier, error: approval.error ?? '' });
				case 'expired':
					return m['assistant.approval_expired']({ identifier });
				default:
					return m['assistant.accept_denied']({ identifier });
			}
		}
		if (approval.action === 'start_preview') {
			switch (approval.status) {
				case 'approving':
					return m['assistant.preview_approving']({ identifier });
				case 'approved':
					return m['assistant.preview_approved']({ identifier });
				case 'failed':
					return m['assistant.preview_failed']({ identifier, error: approval.error ?? '' });
				case 'expired':
					return m['assistant.approval_expired']({ identifier });
				default:
					return m['assistant.preview_denied']({ identifier });
			}
		}
		if (approval.action === 'expose_port') {
			const port = approval.payload.port;
			switch (approval.status) {
				case 'approving':
					return m['assistant.port_approving']({ identifier, port });
				case 'approved':
					return m['assistant.port_approved']({ identifier, port });
				case 'failed':
					return m['assistant.port_failed']({ identifier, port, error: approval.error ?? '' });
				case 'expired':
					return m['assistant.approval_expired']({ identifier });
				default:
					return m['assistant.port_denied']({ identifier, port });
			}
		}
		if (approval.action === 'run_check') {
			switch (approval.status) {
				case 'approving':
					return m['assistant.run_check_approving']({ identifier });
				case 'approved':
					return m['assistant.run_check_approved']({ identifier });
				case 'failed':
					return m['assistant.run_check_failed']({ identifier, error: approval.error ?? '' });
				case 'expired':
					return m['assistant.approval_expired']({ identifier });
				default:
					return m['assistant.run_check_denied']({ identifier });
			}
		}
		if (approval.action === 'run_custom_tool') {
			const tool = `custom_${approval.payload.tool_name}`;
			switch (approval.status) {
				case 'approving':
					return m['assistant.custom_run_approving']({ identifier, tool });
				case 'approved':
					return m['assistant.custom_run_approved']({ identifier, tool });
				case 'failed':
					return m['assistant.custom_run_failed']({ identifier, tool, error: approval.error ?? '' });
				case 'expired':
					return m['assistant.approval_expired']({ identifier });
				default:
					return m['assistant.custom_run_denied']({ identifier, tool });
			}
		}
		if (approval.action === 'publish_task_changes') {
			switch (approval.status) {
				case 'approving':
					return m['assistant.publish_task_approving']({ identifier });
				case 'approved':
					if (approval.payload.outcome === 'published') return m['assistant.publish_task_approved']({ identifier });
					if (approval.payload.outcome === 'empty') return m['assistant.publish_task_empty']({ identifier });
					return m['tasks.approved']();
				case 'failed':
					return approval.error ?? m['assistant.tool_failed']({ tool: m['assistant.tool_publish_task_changes']() });
				case 'expired':
					return m['assistant.approval_expired']({ identifier });
				default:
					return m['assistant.publish_task_denied']({ identifier });
			}
		}
		if (approval.action === 'message_task') {
			switch (approval.status) {
				case 'approving':
					return m['assistant.message_task_approving']({ identifier });
				case 'approved':
					return m['assistant.message_task_approved']({ identifier });
				case 'failed':
					return m['assistant.message_task_failed']({ identifier, error: approval.error ?? '' });
				case 'expired':
					return m['assistant.approval_expired']({ identifier });
				default:
					return m['assistant.message_task_denied']({ identifier });
			}
		}
		if (approval.action === 'request_agent_review' || approval.action === 'follow_up_task') {
			const review = approval.action === 'request_agent_review';
			const agent = review ? agentInfo(approval.payload.harness).name : '';
			switch (approval.status) {
				case 'approving':
					return review
						? m['assistant.review_approving']({ identifier, agent })
						: m['assistant.follow_up_approving']({ identifier });
				case 'approved':
					return review
						? m['assistant.review_approved']({ identifier, agent })
						: m['assistant.follow_up_approved']({ identifier });
				case 'failed':
					return review
						? m['assistant.review_failed']({ identifier, error: approval.error ?? '' })
						: m['assistant.follow_up_failed']({ identifier, error: approval.error ?? '' });
				case 'expired':
					return m['assistant.approval_expired']({ identifier });
				default:
					return review
						? m['assistant.review_denied']({ identifier })
						: m['assistant.follow_up_denied']({ identifier });
			}
		}
		switch (approval.status) {
			case 'approving':
				return merge
					? m['assistant.merge_approving']({ identifier })
					: m['assistant.approval_approving']({ identifier });
			case 'waiting_checks':
				// Approved: Kuayle updated the branch and merges once its checks pass (KUA-207).
				return m['assistant.merge_waiting']({ identifier });
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
	const label = (call: ConversationToolCall) => toolLabel(call.name);
	const duration = (call: ConversationToolCall) => {
		const ms = elapsed(call.created_at, call.completed_at);
		return ms === undefined ? '' : formatDuration(ms);
	};
	const shown = $derived(calls.filter((call) => part === 'approvals'
		? approvals[call.id] && approvals[call.id].status !== 'pending'
		: !approvals[call.id]));
	const steps = $derived(splitSteps(shown));
	function created(call: ConversationToolCall) {
		const identifier = (call.output as { identifier?: unknown } | undefined)?.identifier;
		return (call.name === 'create_issue' || call.name === 'update_issue') &&
			call.status === 'completed' &&
			typeof identifier === 'string'
			? identifier
			: undefined;
	}
	// list_ports names its machine; the ports themselves are read from the API.
	const uuidPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
	function listedMachine(call: ConversationToolCall) {
		const machine = (call.output as { machine_id?: unknown } | undefined)?.machine_id;
		return call.name === 'list_ports' &&
			call.status === 'completed' &&
			typeof machine === 'string' &&
			uuidPattern.test(machine)
			? machine
			: undefined;
	}
	// A message_task call recorded before messages needed approval was sent at
	// once, and its line says where it went. A newer call only requests an
	// approval, whose card and outcome say the rest.
	function sentTo(call: ConversationToolCall) {
		const output = call.output as { identifier?: unknown; delivery?: unknown; sent?: unknown } | undefined;
		return call.name === 'message_task' &&
			call.status === 'completed' &&
			output?.sent === true &&
			typeof output.identifier === 'string'
			? { identifier: output.identifier, live: output.delivery === 'live' }
			: undefined;
	}
	// load_skill stores only a summary; the line names the skill it loaded.
	function loadedSkill(call: ConversationToolCall) {
		const name = (call.output as { name?: unknown } | undefined)?.name ?? call.input?.name;
		return call.name === 'load_skill' && call.status === 'completed' && typeof name === 'string' ? name : undefined;
	}
	const proposal = (call: ConversationToolCall) =>
		call.name === 'propose_skill' && call.status === 'completed' ? proposedSkill(call.output) : undefined;
	const toolProposal = (call: ConversationToolCall) =>
		call.name === 'propose_tool' && call.status === 'completed' ? proposedTool(call.output) : undefined;
	// A custom tool call that ran (auto mode) stores the run's summary.
	const customRun = (call: ConversationToolCall) =>
		call.name.startsWith('custom_') && call.status === 'completed' ? toolRunSummary(call.output) : undefined;
	const readRun = (call: ConversationToolCall) =>
		call.name === 'get_tool_run' && call.status === 'completed' ? toolRunSummary(call.output) : undefined;
	// Tool data is untrusted: it is rendered only as escaped Code Block text.
	const pretty = (value: unknown) => JSON.stringify(value ?? null, null, 2);
</script>

<!-- Official AI Elements Tool composition: Trace rows (one-line step chips), expandable Code Block output. -->
{#snippet callView(call: ConversationToolCall)}
	{@const approval = approvals[call.id]}
	{#if approval}
		{@const identifier = 'identifier' in approval.payload ? approval.payload.identifier : ''}
		<Trace.Line
				status={approval.status === 'approving' || approval.status === 'waiting_checks'
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
						/>{:else if approval.status === 'waiting_checks'}<Hourglass
						/>{:else if approval.status === 'approved'}<CircleCheck
						/>{:else if approval.status === 'failed'}<CircleAlert />{:else}<CircleX />{/if}</Trace.Icon
				>
				<Trace.Title class="whitespace-normal break-words">{outcome(approval)}</Trace.Title>
			</Trace.Line>
			{#if approval.action === 'bulk_update_issues' && approval.payload.results?.length}
				<BulkIssueChanges payload={approval.payload} />
			{/if}
			{#if approval.action === 'merge_pull_request' && approval.status === 'waiting_checks' && approval.payload.wait?.waiting}<p
					data-testid="merge-waiting"
					class="pl-6 text-xs break-words text-muted-foreground"
				>
					{approval.payload.wait.waiting}
				</p>{/if}
			{#if approval.action === 'delegate_issue' && approval.status === 'approved' && approval.payload.workflow}<p
					class="flex min-w-0 items-center gap-1 pl-6 text-xs text-muted-foreground"
				>
					<Workflow size={12} class="shrink-0" /><span class="truncate"
						>{m['workflows.choose_label']()}: {approval.payload.workflow}</span
					>
				</p>{/if}
			{#if approval.action === 'delegate_issue' && approval.status === 'approved' && approval.task_id}<AgentSubagent
					{slug}
					taskId={approval.task_id}
					{identifier}
				/>{:else if approval.action === 'start_process' && approval.status === 'approved'}<AssistantProcessResult
					{slug}
					conversationId={approval.conversation_id}
					processId={approval.id}
				/>{:else if approval.action === 'start_preview' && approval.status === 'approved'}<AppPortLinks
					{slug}
					machineId={approval.payload.machine_id}
					taskId={approval.payload.task_id}
					wait
				/>{:else if approval.action === 'expose_port' && approval.status === 'approved'}<AppPortLinks
					{slug}
					machineId={approval.payload.machine_id}
					port={approval.payload.port}
					wait
				/>{:else if approval.action === 'upgrade_instance' && approval.status === 'approved'}<UpgradeProgress
				/>{:else if approval.action === 'publish_task_changes' && approval.status === 'approved'}
				{@const href = safePullRequestUrl(approval.payload.pull_request_url)}
				{#if href}<a
						class="inline-flex items-center gap-1 pl-6 text-xs underline underline-offset-2"
						{href}
						target="_blank"
						rel="noopener noreferrer"><ExternalLink size={12} />{m['assistant.publish_task_open']()}</a
					>{/if}
			{:else if approval.action === 'create_release' && approval.status === 'approved'}<a
					class="inline-flex items-center gap-1 pl-6 text-xs underline underline-offset-2"
					href={approval.payload.release_url}
					target="_blank"
					rel="noopener noreferrer"><ExternalLink size={12} />{m['assistant.release_open']()}</a
				>{:else if approval.action === 'ship_pull_request' && approval.status !== 'denied' && approval.status !== 'expired'}<div
					class="pl-6"
				>
					<ShipSteps payload={approval.payload} />
				</div>{:else if approval.action === 'run_custom_tool' && approval.status === 'approved'}<CustomToolRunResult
					{slug}
					name="custom_{approval.payload.tool_name}"
					toolId={approval.payload.tool_id}
					approvalId={approval.id}
				/>{:else if (approval.action === 'run_command' ||
					approval.action === 'run_command_write' ||
					approval.action === 'run_migration') &&
					approval.status === 'approved'}<CommandRunResult
					{slug}
					tool={approval.action}
					conversationId={approval.conversation_id}
					approvalId={approval.id}
				/>{/if}
	{:else if call.status === 'running'}
		<Trace.Line status="running">
			<Trace.Icon><LoaderCircle class="animate-spin" /></Trace.Icon>
			<Trace.Title>{m['assistant.tool_running']({ tool: label(call) })}</Trace.Title>
		</Trace.Line>
	{:else if proposal(call)}
		<ProposedSkillCard skill={proposal(call)!} {slug} id={call.id} />
	{:else if loadedSkill(call)}
		<Trace.Line status="success">
			<Trace.Icon><BookOpenText /></Trace.Icon>
			<Trace.Title class="whitespace-normal break-words"
				>{m['assistant.skill_used']({ name: loadedSkill(call)! })}</Trace.Title
			>
		</Trace.Line>
	{:else if toolProposal(call)}
		<ProposedToolCard tool={toolProposal(call)!} {slug} id={call.id} />
	{:else if customRun(call)}
		<CustomToolRunResult {slug} name={call.name} initial={customRun(call)} />
	{:else if readRun(call)}
		{@const read = readRun(call)!}
		<Trace.Line status={read.status === 'failed' ? 'error' : read.status === 'succeeded' ? 'success' : undefined}>
			<Trace.Icon><Wrench /></Trace.Icon>
			<Trace.Title class="break-words whitespace-normal"
				>{m['assistant.tool_run_read']({ tool: read.tool, status: runStatusLabel(read.status) })}{read.exit_code ===
				null
					? ''
					: ` · ${m['assistant_custom_tools.exit_code']({ code: String(read.exit_code) })}`}</Trace.Title
			>
		</Trace.Line>
	{:else}
		{@const failed = call.status === 'failed'}
		{@const identifier = created(call)}
		{@const Renderer = toolChip(call.name)?.renderer}
		{@const Icon = toolChip(call.name)?.icon ?? Wrench}
		{@const machine = listedMachine(call)}
		{@const sent = sentTo(call)}
		<Trace.Collapsible>
			<Trace.CollapsibleTrigger>
				<Trace.Line status={failed ? 'error' : identifier || sent ? 'success' : undefined}>
					<Trace.Icon
						>{#if failed}<CircleAlert />{:else}<Icon />{/if}</Trace.Icon
					>
					<Trace.Title class="whitespace-normal break-words"
						>{sent
							? sent.live
								? m['assistant.tool_message_sent_live']({ identifier: sent.identifier })
								: m['assistant.tool_message_sent_next']({ identifier: sent.identifier })
							: identifier
								? call.name === 'update_issue'
									? m['assistant.tool_updated_issue']({ identifier })
									: m['assistant.tool_created_issue']({ identifier })
								: failed
									? m['assistant.tool_failed']({ tool: label(call) })
									: m['assistant.tool_completed']({ tool: label(call) })}</Trace.Title
					>{#if duration(call)}<Trace.Description class="shrink-0 tabular-nums">{duration(call)}</Trace.Description
						>{/if}
				</Trace.Line>
			</Trace.CollapsibleTrigger>
			<Trace.CollapsibleContent class="space-y-2">
				{#if Renderer}<Renderer {call} {slug} />{:else}
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
				>{/if}
			</Trace.CollapsibleContent>
		</Trace.Collapsible>
		{#if identifier}<a
				class="inline-flex items-center gap-1 text-xs underline underline-offset-2"
				href="/{slug}/issue/{encodeURIComponent(identifier)}"><ExternalLink size={12} />{identifier}</a
			>{/if}
		{#if machine}<AppPortLinks {slug} machineId={machine} />{/if}
	{/if}
{/snippet}

{#if shown.length}
	<Trace.Root class="min-w-0 gap-1.5" data-testid={part === 'approvals' ? 'tool-approvals' : 'tool-steps'}>
		{#each steps.inline as call (call.id)}{@render callView(call)}{/each}
		{#if steps.overflow.length}
			<Trace.Collapsible>
				<Trace.CollapsibleTrigger>
					<Trace.Line>
						<Trace.Icon><Wrench /></Trace.Icon>
						<Trace.Title>{m['assistant.steps_more']({ count: steps.overflow.length })}</Trace.Title>
					</Trace.Line>
				</Trace.CollapsibleTrigger>
				<Trace.CollapsibleContent>
					<Trace.Root class="min-w-0 gap-1.5">
						{#each steps.overflow as call (call.id)}{@render callView(call)}{/each}
					</Trace.Root>
				</Trace.CollapsibleContent>
			</Trace.Collapsible>
		{/if}
	</Trace.Root>
{/if}
