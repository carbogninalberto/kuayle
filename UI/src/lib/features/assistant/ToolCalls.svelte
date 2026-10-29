<script lang="ts">
	import {
		Wrench,
		CircleAlert,
		LoaderCircle,
		ExternalLink,
		Bot,
		CircleCheck,
		CircleX,
		GitBranch,
		Lock,
		Server,
		BookOpenText,
		Eye
	} from '@lucide/svelte';
	import * as Trace from '$lib/components/ui/trace/index.js';
	import * as CodeBlock from '$lib/components/ui/code-block/index.js';
	import * as Item from '$lib/components/ui/item/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import type { ConversationApproval, ConversationToolCall } from '$lib/api/conversations';
	import { m } from '$lib/paraglide/messages.js';
	import AgentSubagent from './AgentSubagent.svelte';
	import AppPortLinks from './AppPortLinks.svelte';
	import ProposedSkillCard, { proposedSkill } from './ProposedSkillCard.svelte';
	import ProposedToolCard, { proposedTool } from './ProposedToolCard.svelte';
	import CustomToolRunResult, { toolRunSummary } from './CustomToolRunResult.svelte';
	import { runStatusLabel } from './ToolRunView.svelte';
	import AgentLogo from '$lib/features/agents/AgentLogo.svelte';
	import { agentInfo } from '$lib/features/agents/agents';
	import { repositorySourceLabel } from '$lib/features/dev-machines/repository-source';
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
		merge_pull_request: m['assistant.tool_merge_pull_request'],
		accept_agent_work: m['assistant.tool_accept_agent_work'],
		request_agent_review: m['assistant.tool_request_agent_review'],
		follow_up_task: m['assistant.tool_follow_up_task'],
		start_preview: m['assistant.tool_start_preview'],
		stop_preview: m['assistant.tool_stop_preview'],
		expose_port: m['assistant.tool_expose_port'],
		list_ports: m['assistant.tool_list_ports'],
		run_check: m['assistant.tool_run_check'],
		message_task: m['assistant.tool_message_task'],
		get_task_messages: m['assistant.tool_get_task_messages'],
		load_skill: m['assistant.tool_load_skill'],
		propose_skill: m['assistant.tool_propose_skill'],
		propose_tool: m['assistant.tool_propose_tool'],
		get_tool_run: m['assistant.tool_get_tool_run']
	};
	// Outcome lines per approval action.
	function outcome(approval: ConversationApproval) {
		const identifier = approval.payload.identifier;
		const merge = approval.action === 'merge_pull_request';
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
	// message_task only delivers text, so its card says where it went.
	function sentTo(call: ConversationToolCall) {
		const output = call.output as { identifier?: unknown; delivery?: unknown } | undefined;
		return call.name === 'message_task' && call.status === 'completed' && typeof output?.identifier === 'string'
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
							{:else if approval.action === 'accept_agent_work'}
								<Item.Title>{m['assistant.accept_title']({ identifier })}</Item.Title>
								<Item.Description class="line-clamp-none"
									>{m['assistant.accept_description']({
										title: approval.payload.title,
										attempt: approval.payload.generation,
										number: approval.payload.pull_request
									})}</Item.Description
								>
								<a
									class="inline-flex items-center gap-1 text-xs underline underline-offset-2"
									href={approval.payload.pull_request_url}
									target="_blank"
									rel="noopener noreferrer"><ExternalLink size={12} />{m['tasks.open_pull_request']()}</a
								>
							{:else if approval.action === 'request_agent_review'}
								<Item.Title class="flex items-center gap-2"
									><AgentLogo agent={approval.payload.harness} size="sm" />{m['assistant.review_title']({
										identifier,
										agent: agentInfo(approval.payload.harness).name
									})}</Item.Title
								>
								<Item.Description class="line-clamp-none"
									>{m['assistant.review_description']({
										number: approval.payload.pull_request,
										commit: approval.payload.commit.slice(0, 7)
									})}</Item.Description
								>
								{#if approval.payload.focus}<CodeBlock.Root size="sm"
										><CodeBlock.Content code={approval.payload.focus} wrap class="max-h-48 text-xs" /></CodeBlock.Root
									>{/if}
								<p class="text-xs text-muted-foreground">
									{approval.payload.post_to_pull_request
										? m['assistant.review_posts_comment']()
										: m['assistant.review_no_comment']()}
								</p>
								<a
									class="inline-flex items-center gap-1 text-xs underline underline-offset-2"
									href={approval.payload.pull_request_url}
									target="_blank"
									rel="noopener noreferrer"><ExternalLink size={12} />{m['tasks.open_pull_request']()}</a
								>
							{:else if approval.action === 'run_check'}
								<Item.Title>{m['assistant.run_check_title']({ identifier })}</Item.Title>
								<Item.Description class="line-clamp-none"
									>{m['assistant.run_check_description']({ title: approval.payload.title })}</Item.Description
								>
								<!-- The exact argv that runs, as recorded on the check. -->
								<CodeBlock.Root size="sm"
									><CodeBlock.Content
										code={approval.payload.command
											? JSON.stringify(approval.payload.command)
											: m['assistant.run_check_default']()}
										wrap
										class="max-h-48 text-xs"
									/></CodeBlock.Root
								>
							{:else if approval.action === 'run_custom_tool'}
								<Item.Title class="break-all"
									>{m['assistant.custom_run_title']({
										tool: `custom_${approval.payload.tool_name}`,
										version: String(approval.payload.version),
										identifier
									})}</Item.Title
								>
								<Item.Description class="line-clamp-none break-words"
									>{approval.payload.description}</Item.Description
								>
								<p class="text-xs break-words text-muted-foreground">
									{m['assistant.custom_run_where']({
										title: approval.payload.title,
										interpreter: approval.payload.interpreter
									})}
								</p>
								<!-- The exact validated inputs the run receives, as data. -->
								<CodeBlock.Root size="sm"
									><CodeBlock.Header
										><CodeBlock.Title>{m['assistant_custom_tools.inputs']()}</CodeBlock.Title></CodeBlock.Header
									><CodeBlock.Content
										code={pretty(approval.payload.inputs)}
										wrap
										class="max-h-48 text-xs"
									/></CodeBlock.Root
								>
								{#if approval.payload.read_only}<p class="flex items-center gap-1 text-xs text-muted-foreground">
										<Eye size={12} class="shrink-0" />{m['assistant.custom_run_read_only']()}
									</p>{/if}
							{:else if approval.action === 'follow_up_task'}
								<Item.Title>{m['assistant.follow_up_title']({ identifier })}</Item.Title>
								<Item.Description class="line-clamp-none"
									>{m['assistant.follow_up_description']({ title: approval.payload.title })}</Item.Description
								>
								<CodeBlock.Root size="sm"
									><CodeBlock.Content
										code={approval.payload.instructions}
										wrap
										class="max-h-48 text-xs"
									/></CodeBlock.Root
								>
							{:else if approval.action === 'start_preview'}
								<Item.Title>{m['assistant.preview_title']({ identifier })}</Item.Title>
								<Item.Description class="line-clamp-none"
									>{m['assistant.preview_description']({
										title: approval.payload.title,
										machine: approval.payload.machine
									})}</Item.Description
								>
								<CodeBlock.Root size="sm"
									><CodeBlock.Content
										code={approval.payload.command.join(' ')}
										wrap
										class="max-h-24 text-xs"
									/></CodeBlock.Root
								>
								{#if approval.payload.port}<p class="text-xs text-muted-foreground">
										{m['assistant.preview_port']({ port: approval.payload.port })}
									</p>{/if}
								<p class="flex items-center gap-1 text-xs text-muted-foreground">
									<Lock size={12} class="shrink-0" />{m['assistant.port_audience_owner']()}
								</p>
							{:else if approval.action === 'expose_port'}
								<Item.Title>{m['assistant.port_title']({ identifier, port: approval.payload.port })}</Item.Title>
								<Item.Description class="line-clamp-none"
									>{m['assistant.port_description']({
										port: approval.payload.port,
										machine: approval.payload.machine,
										host: approval.payload.host
									})}</Item.Description
								>
								{#if approval.payload.existing}<p class="flex items-center gap-1 text-xs text-muted-foreground">
										<Server size={12} class="shrink-0" />{m['assistant.port_existing']()}
									</p>{/if}
								<p class="flex items-center gap-1 text-xs text-muted-foreground">
									<Lock size={12} class="shrink-0" />{m['assistant.port_audience_owner']()}
								</p>
							{:else}
								<Item.Title>{m['assistant.approval_title']({ identifier })}</Item.Title>
								<Item.Description class="line-clamp-none"
									>{m['assistant.approval_description']({ title: approval.payload.title })}</Item.Description
								>
								{#if approval.payload.repository}<p
										class="flex min-w-0 items-center gap-1 text-xs text-muted-foreground"
									>
										<GitBranch size={12} class="shrink-0" /><span class="truncate"
											>{m['assistant.approval_repository']({
												repository: approval.payload.repository,
												branch: approval.payload.base_branch ?? '',
												source: repositorySourceLabel(approval.payload.repository_source ?? 'workspace')
											})}</span
										>
									</p>{/if}
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
						/>{:else if approval.action === 'run_custom_tool' && approval.status === 'approved'}<CustomToolRunResult
							{slug}
							name="custom_{approval.payload.tool_name}"
							toolId={approval.payload.tool_id}
							approvalId={approval.id}
						/>{/if}
				{/if}
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
				{@const machine = listedMachine(call)}
				{@const sent = sentTo(call)}
				<Trace.Collapsible>
					<Trace.CollapsibleTrigger>
						<Trace.Line status={failed ? 'error' : identifier || sent ? 'success' : undefined}>
							<Trace.Icon
								>{#if failed}<CircleAlert />{:else}<Wrench />{/if}</Trace.Icon
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
				{#if machine}<AppPortLinks {slug} machineId={machine} />{/if}
			{/if}
		{/each}
	</Trace.Root>
{/if}
