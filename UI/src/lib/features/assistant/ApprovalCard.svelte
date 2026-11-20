<script lang="ts">
	import { ChevronDown, GitBranch, Lock, Server, Eye, Workflow, ExternalLink } from '@lucide/svelte';
	import { Button } from '$lib/components/ui/button/index.js';
	import * as Alert from '$lib/components/ui/alert/index.js';
	import * as Item from '$lib/components/ui/item/index.js';
	import * as CodeBlock from '$lib/components/ui/code-block/index.js';
	import { getLocale, m } from '$lib/features/i18n/messages';
	import { agentInfo } from '$lib/features/agents/agents';
	import { repositorySourceLabel } from '$lib/features/dev-machines/repository-source';
	import {
		isWikiApproval,
		type ApprovalEdit,
		type ConversationApproval,
		type ApprovalModelChoice,
		type GitHubAccessGap
	} from '$lib/api/conversations';
	import type { WorkflowList } from '$lib/api/workflows';
	import ApprovalModelPicker from './ApprovalModelPicker.svelte';
	import BulkIssueChanges from './BulkIssueChanges.svelte';
	import Markdown from './Markdown.svelte';
	import MergeHeadChange, { mergeHeadSummary } from './MergeHeadChange.svelte';
	import ReleaseApprovalCard from './ReleaseApprovalCard.svelte';
	import ShipApprovalCard from './ShipApprovalCard.svelte';
	import WorkflowChoice from './WorkflowChoice.svelte';
	import { commandMachineSize } from './command-machine';
	import { releaseNotesInvalid } from './ReleaseNotesField.svelte';
	import { safePullRequestUrl } from './merged-work';
	import { customWorkflows, DEFAULT_WORKFLOW, isCustomChoice, workflowEdit } from './workflow-choice';

	let {
		approval,
		slug,
		workflowList,
		onresolve
	}: {
		approval: ConversationApproval;
		slug: string;
		workflowList: WorkflowList | null;
		onresolve: (approval: ConversationApproval, decision: 'approve' | 'deny', edit?: ApprovalEdit) => Promise<void>;
	} = $props();
	let expanded = $state(false),
		opened = $state(false),
		deciding = $state(false),
		error = $state('');
	let releaseNotes = $state<string>(),
		modelChoice = $state<ApprovalModelChoice>();
	let selectedWorkflow = $state<string>();
	const proposedWorkflow = $derived(approval.action === 'delegate_issue' ? approval.payload.workflow_id : undefined);
	const workflow = $derived(selectedWorkflow ?? (proposedWorkflow || DEFAULT_WORKFLOW));
	const offersWorkflow = $derived(workflowList && (customWorkflows(workflowList).length > 0 || proposedWorkflow));
	const draft = $derived(
		approval.action === 'create_release'
			? approval.payload.notes
			: approval.action === 'ship_pull_request'
				? approval.payload.release.notes
				: undefined
	);
	const invalid = $derived(draft !== undefined && releaseNotesInvalid(releaseNotes ?? draft));
	const needsReview = $derived(
		!opened && [
			'run_command', 'run_command_write', 'run_migration', 'merge_pull_request',
			'ship_pull_request', 'delete_wiki_page', 'run_custom_tool'
		].includes(approval.action)
	);
	const gap = $derived((approval.payload as { github_access?: GitHubAccessGap }).github_access);
	const merge = $derived(
		approval.action === 'merge_pull_request'
			? approval.payload
			: approval.action === 'ship_pull_request'
				? approval.payload.merge
				: undefined
	);
	const warning = $derived.by(() => {
		if (gap)
			return gap.unverified ? m['assistant.github_access_unverified_title']() : m['assistant.github_access_title']();
		if (approval.action === 'follow_up_task' && approval.payload.merged_pull_request)
			return m['assistant.follow_up_merged']({ number: approval.payload.merged_pull_request.number });
		if (merge?.head_change) return mergeHeadSummary(merge.head_change, merge.published_commit);
		return '';
	});
	const identifier = $derived('identifier' in approval.payload ? approval.payload.identifier : '');
	const title = $derived('title' in approval.payload && approval.payload.title);
	function chooseWorkflow(choice: string) {
		selectedWorkflow = choice;
		if (isCustomChoice(choice)) modelChoice = undefined;
	}
	async function decide(decision: 'approve' | 'deny') {
		if (deciding || (decision === 'approve' && (invalid || needsReview))) return;
		deciding = true;
		error = '';
		const edit = {
			...modelChoice,
			...(offersWorkflow && approval.action === 'delegate_issue' ? workflowEdit(workflow, proposedWorkflow) : {}),
			...(releaseNotes !== undefined && releaseNotes !== draft ? { notes: releaseNotes } : {})
		};
		try {
			await onresolve(approval, decision, decision === 'approve' ? edit : undefined);
		} catch (cause) {
			const failure = cause as { status?: number; error?: { message?: string } };
			if (failure.status !== 410)
				error = (failure.status === 422 && failure.error?.message) || m['assistant.approval_error']();
		} finally {
			deciding = false;
		}
	}
</script>

{#snippet action()}
	{#if approval.action === 'bulk_update_issues'}
		{m['assistant.bulk_issues']({ count: approval.payload.changes.length })}
	{:else if approval.action === 'merge_pull_request'}
		{m['assistant.merge_title']({ identifier, number: approval.payload.pull_request })}
	{:else if approval.action === 'reply_to_review_comment' || approval.action === 'resolve_review_thread'}
		{approval.action === 'reply_to_review_comment'
			? m['assistant.pr_reply_title']({
					author: approval.payload.author ?? '',
					number: approval.payload.pull_request,
					identifier
				})
			: m['assistant.pr_resolve_title']({
					path: approval.payload.path ?? '',
					identifier
				})}
	{:else if approval.action === 'update_pull_request_branch'}
		{m['assistant.pr_branch_title']({ number: approval.payload.pull_request, identifier })}
	{:else if approval.action === 'mark_pull_request_ready'}
		{approval.payload.draft
			? m['assistant.pr_draft_title']({ number: approval.payload.pull_request, identifier })
			: m['assistant.pr_ready_title']({ number: approval.payload.pull_request, identifier })}
	{:else if approval.action === 'accept_agent_work'}
		{m['assistant.accept_title']({ identifier })}
	{:else if approval.action === 'request_agent_review'}
		{m['assistant.review_title']({
			identifier,
			agent: agentInfo(approval.payload.harness).name
		})}
	{:else if approval.action === 'run_check'}
		{m['assistant.run_check_title']({ identifier })}
	{:else if approval.action === 'run_custom_tool'}
		{m['assistant.custom_run_title']({
			tool: `custom_${approval.payload.tool_name}`,
			version: String(approval.payload.version),
			identifier
		})}
	{:else if approval.action === 'follow_up_task'}
		{m['assistant.follow_up_title']({ identifier })}
	{:else if approval.action === 'publish_task_changes'}
		{m['assistant.publish_task_title']({ identifier })}
	{:else if approval.action === 'message_task'}
		{m['assistant.message_task_title']({ identifier })}
	{:else if approval.action === 'start_preview'}
		{m['assistant.preview_title']({ identifier })}
	{:else if approval.action === 'create_release'}
		{m['assistant.release_title']({ tag: approval.payload.tag, repository: approval.payload.repository })}
	{:else if approval.action === 'ship_pull_request'}
		{m['assistant.ship_title']({
			identifier: approval.payload.identifier,
			number: approval.payload.pull_request,
			tag: approval.payload.release.tag,
			repository: approval.payload.repository
		})}
	{:else if approval.action === 'upgrade_instance'}
		{m['assistant.upgrade_title']({
			current: approval.payload.current_version,
			latest: approval.payload.latest_version
		})}
	{:else if isWikiApproval(approval)}
		{approval.action === 'delete_wiki_page'
			? m['assistant.wiki_delete_title']({ title: approval.payload.title ?? '' })
			: approval.action === 'restore_wiki_page_version'
				? m['assistant.wiki_restore_title']({ title: approval.payload.title ?? '' })
				: approval.action === 'share_wiki_page'
					? approval.payload.rotate
						? m['assistant.wiki_share_rotate_title']({ title: approval.payload.title ?? '' })
						: approval.payload.publish_mode === 'public'
							? m['assistant.wiki_share_public_title']({ title: approval.payload.title ?? '' })
							: m['assistant.wiki_share_unpublish_title']({ title: approval.payload.title ?? '' })
					: m['assistant.wiki_disable_registry_title']()}
	{:else if approval.action === 'expose_port'}
		{m['assistant.port_title']({ identifier, port: approval.payload.port })}
	{:else if approval.action === 'start_temporary_machine'}
		{m['assistant.temp_machine_title']()}
	{:else if approval.action === 'run_command' || approval.action === 'run_command_write' || approval.action === 'run_migration'}
		{approval.action === 'run_migration'
			? m['assistant.migration_title']({
					machine: approval.payload.machine,
					action: approval.payload.action
				})
			: approval.payload.write
				? m['assistant.command_write_title']({ machine: approval.payload.machine })
				: m['assistant.command_title']({ machine: approval.payload.machine })}
	{:else}
		{m['assistant.approval_title']({ identifier })}
	{/if}
{/snippet}

<article
	data-testid="approval-card"
	aria-labelledby="approval-title-{approval.id}"
	class="min-w-0 rounded-xl border bg-card text-card-foreground shadow-sm"
>
	<div class="px-3 pt-2.5 pb-2 sm:px-4">
		<Button
			variant="plain"
			aria-expanded={expanded}
			aria-describedby={needsReview ? 'approval-review-' + approval.id : undefined}
			aria-controls="approval-details-{approval.id}"
			class="flex w-full items-start gap-2 rounded-md text-left focus-visible:outline-2 focus-visible:outline-ring"
			onclick={() => {
				expanded = !expanded;
				opened = true;
			}}
		>
			<ChevronDown
				size={16}
				aria-hidden="true"
				class="mt-0.5 shrink-0 text-muted-foreground transition-transform motion-reduce:transition-none {expanded
					? ''
					: '-rotate-90'}"
			/>
			<span class="min-w-0 [overflow-wrap:anywhere]">
				{#if title}<span id="approval-title-{approval.id}" class="block text-sm leading-snug font-semibold"
						>{title}</span
					>{/if}
				<span
					id={title ? undefined : 'approval-title-' + approval.id}
					class={title ? 'block text-xs text-muted-foreground' : 'block text-sm font-semibold'}>{@render action()}</span
				>
			</span>
		</Button>
	</div>
	<div
		id="approval-details-{approval.id}"
		inert={!expanded}
		aria-hidden={!expanded}
		class="grid transition-[grid-template-rows] duration-200 ease-out motion-reduce:transition-none {expanded
			? 'grid-rows-[1fr]'
			: 'grid-rows-[0fr]'}"
	>
		<div class="min-h-0 overflow-hidden">
			{#if opened}
				<div
					data-testid="approval-details"
					data-selectable="text"
					class="max-h-[min(35dvh,22rem)] space-y-3 overflow-y-auto overscroll-contain border-t p-3 text-sm [overflow-wrap:anywhere] sm:p-4"
				>
					{#if gap}
						<Alert.Root class="mb-2 border-[var(--status-warning)]/40" data-testid="github-access-gap">
							<Alert.Title
								>{gap.unverified
									? m['assistant.github_access_unverified_title']()
									: m['assistant.github_access_title']()}</Alert.Title
							>
							<Alert.Description class="space-y-1">
								<p>{gap.message}</p>
								{#if gap.settings_url}
									<a
										href={gap.settings_url}
										target="_blank"
										rel="noopener noreferrer"
										class="underline underline-offset-2">{m['assistant.github_access_link']()}</a
									>
								{/if}
							</Alert.Description>
						</Alert.Root>
					{/if}
					{#if approval.action === 'bulk_update_issues'}
						<BulkIssueChanges payload={approval.payload} />
					{:else if approval.action === 'merge_pull_request'}
						<Item.Description class="line-clamp-none"
							>{m['assistant.merge_description']({
								title: approval.payload.title,
								commit: approval.payload.commit.slice(0, 7),
								method: approval.payload.merge_method ?? 'squash',
								repository: approval.payload.repository
							})}</Item.Description
						>
						{#if approval.payload.head_change}<MergeHeadChange
								change={approval.payload.head_change}
								publishedCommit={approval.payload.published_commit}
							/>{/if}
						<a
							class="inline-flex items-center gap-1 text-xs underline underline-offset-2"
							href={approval.payload.pull_request_url}
							target="_blank"
							rel="noopener noreferrer"><ExternalLink size={12} />{m['tasks.open_pull_request']()}</a
						>
					{:else if approval.action === 'reply_to_review_comment' || approval.action === 'resolve_review_thread'}
						{@const reply = approval.action === 'reply_to_review_comment'}

						<Item.Description class="line-clamp-none"
							>{reply
								? m['assistant.pr_reply_description']({
										path: approval.payload.path ?? '',
										repository: approval.payload.repository
									})
								: m['assistant.pr_resolve_description']({
										author: approval.payload.author ?? '',
										number: approval.payload.pull_request,
										repository: approval.payload.repository
									})}</Item.Description
						>
						{#if approval.payload.bot}<p class="text-xs text-muted-foreground">
								{m['assistant.pr_comment_bot']({ author: approval.payload.author ?? '' })}
							</p>{/if}
						{#if approval.payload.excerpt}
							<p class="text-xs font-medium text-muted-foreground">{m['assistant.pr_comment_excerpt']()}</p>
							<CodeBlock.Root size="sm"
								><CodeBlock.Content code={approval.payload.excerpt} wrap class="max-h-24 text-xs" /></CodeBlock.Root
							>
						{/if}
						{#if reply}
							<p class="text-xs font-medium text-muted-foreground">{m['assistant.pr_reply_body']()}</p>
							<CodeBlock.Root size="sm"
								><CodeBlock.Content code={approval.payload.body ?? ''} wrap class="max-h-48 text-xs" /></CodeBlock.Root
							>
						{/if}
						<a
							class="inline-flex items-center gap-1 text-xs underline underline-offset-2"
							href={approval.payload.pull_request_url}
							target="_blank"
							rel="noopener noreferrer"><ExternalLink size={12} />{m['tasks.open_pull_request']()}</a
						>
					{:else if approval.action === 'update_pull_request_branch'}
						<Item.Description class="line-clamp-none"
							>{m['assistant.pr_branch_description']({
								base: approval.payload.base ?? '',
								head: (approval.payload.head ?? '').slice(0, 7),
								repository: approval.payload.repository
							})}</Item.Description
						>
						<a
							class="inline-flex items-center gap-1 text-xs underline underline-offset-2"
							href={approval.payload.pull_request_url}
							target="_blank"
							rel="noopener noreferrer"><ExternalLink size={12} />{m['tasks.open_pull_request']()}</a
						>
					{:else if approval.action === 'mark_pull_request_ready'}
						<Item.Description class="line-clamp-none"
							>{m['assistant.pr_draft_description']({
								title: approval.payload.title,
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
						<Item.Description class="line-clamp-none"
							>{m['assistant.review_description']({
								number: approval.payload.pull_request,
								commit: approval.payload.commit.slice(0, 7)
							})}</Item.Description
						>
						{#if approval.payload.focus}<Markdown source={approval.payload.focus} />{/if}
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
						<Item.Description class="line-clamp-none"
							>{m['assistant.run_check_description']({ title: approval.payload.title })}</Item.Description
						>
						<!-- The exact argv that runs, as recorded on the check. -->
						<CodeBlock.Root size="sm"
							><CodeBlock.Content
								code={JSON.stringify(approval.payload.command ?? [])}
								wrap
								class="max-h-48 text-xs"
							/></CodeBlock.Root
						>
					{:else if approval.action === 'run_custom_tool'}
						<Item.Description class="line-clamp-none break-words">{approval.payload.description}</Item.Description>
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
								code={JSON.stringify(approval.payload.inputs, null, 2)}
								wrap
								class="max-h-48 text-xs"
							/></CodeBlock.Root
						>
						{#if approval.payload.read_only}<p class="flex items-center gap-1 text-xs text-muted-foreground">
								<Eye size={12} class="shrink-0" />{m['assistant.custom_run_read_only']()}
							</p>{/if}
					{:else if approval.action === 'follow_up_task'}
						{#if approval.payload.merged_pull_request}
							{@const merged = approval.payload.merged_pull_request}
							{@const href = safePullRequestUrl(merged.url)}
							<!-- The issue's work was merged (KUA-193): approving runs the agent again anyway. -->
							<Alert.Root
								class="border-[var(--status-warning)]/40 text-[var(--status-warning)]"
								data-testid="follow-up-merged"
							>
								<Alert.Description class="space-y-1">
									<p>{m['assistant.follow_up_merged']({ number: merged.number })}</p>
									{#if href}<a
											class="inline-flex items-center gap-1 underline underline-offset-2"
											{href}
											target="_blank"
											rel="noopener noreferrer"
											><ExternalLink size={12} />{m['tasks.merged_work_link']({ number: merged.number })}</a
										>{/if}
								</Alert.Description>
							</Alert.Root>
						{/if}
					{:else if approval.action === 'publish_task_changes'}
						<Item.Description class="line-clamp-none">{m['assistant.publish_task_description']()}</Item.Description>
					{:else if approval.action === 'message_task'}
						<!-- The whole message, exactly as the agent receives it. -->
						<Markdown source={approval.payload.body} />
					{:else if approval.action === 'start_preview'}
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
					{:else if approval.action === 'create_release'}
						<ReleaseApprovalCard
							id={approval.id}
							payload={approval.payload}
							bind:notes={releaseNotes}
							disabled={deciding}
						/>
					{:else if approval.action === 'ship_pull_request'}
						<ShipApprovalCard
							id={approval.id}
							payload={approval.payload}
							bind:notes={releaseNotes}
							disabled={deciding}
						/>
					{:else if approval.action === 'upgrade_instance'}
						<Item.Description class="line-clamp-none">{m['assistant.upgrade_description']()}</Item.Description>
						{#if approval.payload.changelog}<p class="text-xs font-medium text-muted-foreground">
								{m['assistant.upgrade_changelog']()}
							</p>
							<CodeBlock.Root size="sm"
								><CodeBlock.Content code={approval.payload.changelog} wrap class="max-h-48 text-xs" /></CodeBlock.Root
							>{/if}
					{:else if isWikiApproval(approval)}
						<Item.Description class="line-clamp-none"
							>{approval.action === 'restore_wiki_page_version'
								? m['assistant.wiki_restore_description']({
										state:
											approval.payload.restore_state === 'before'
												? m['issue.versions.before']()
												: m['issue.versions.after'](),
										date: approval.payload.version_at
											? new Date(approval.payload.version_at).toLocaleString(getLocale())
											: '',
										wiki: approval.payload.scope_name
									})
								: approval.action === 'delete_wiki_page'
									? m['assistant.wiki_delete_description']({ wiki: approval.payload.scope_name })
									: approval.action === 'share_wiki_page'
										? approval.payload.rotate
											? m['assistant.wiki_share_rotate_description']({ wiki: approval.payload.scope_name })
											: approval.payload.publish_mode === 'public'
												? approval.payload.publish_children
													? m['assistant.wiki_share_public_children_description']({ wiki: approval.payload.scope_name })
													: m['assistant.wiki_share_public_description']({ wiki: approval.payload.scope_name })
												: m['assistant.wiki_share_unpublish_description']({ wiki: approval.payload.scope_name })
										: m['assistant.wiki_disable_registry_description']({
												wiki: approval.payload.scope_name
											})}</Item.Description
						>
					{:else if approval.action === 'expose_port'}
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
					{:else if approval.action === 'start_temporary_machine'}
						{@const size = commandMachineSize(approval.payload)}

						<Item.Description class="line-clamp-none"
							>{m['assistant.temp_machine_description']({
								...size,
								minutes: approval.payload.ttl_minutes
							})}</Item.Description
						>
						<p class="flex items-center gap-1 text-xs text-muted-foreground">
							<Lock size={12} class="shrink-0" />{m['assistant.temp_machine_no_credentials']()}
						</p>
						<p class="flex items-center gap-1 text-xs text-muted-foreground">
							<Server size={12} class="shrink-0" />{m['assistant.temp_machine_quota']({
								used: approval.payload.quota_used,
								limit: approval.payload.quota_limit
							})}
						</p>
					{:else if approval.action === 'run_command' || approval.action === 'run_command_write' || approval.action === 'run_migration'}
						{@const size = commandMachineSize(approval.payload)}

						{#if approval.action === 'run_migration' && approval.payload.description}
							<Item.Description class="line-clamp-none">{approval.payload.description}</Item.Description>
						{/if}
						<!-- The exact command that runs, as sh -c. -->
						<CodeBlock.Root size="sm"
							><CodeBlock.Content code={approval.payload.command} wrap class="max-h-48 text-xs" /></CodeBlock.Root
						>
						<p class="flex min-w-0 items-center gap-1 text-xs text-muted-foreground">
							<Server size={12} class="shrink-0" /><span class="break-words"
								>{m['assistant.command_machine']({ machine: approval.payload.machine, ...size })} · {approval.payload
									.reused
									? m['assistant.command_reused']()
									: approval.payload.temporary
										? m['assistant.command_temporary']()
										: m['assistant.command_own_machine']()}</span
							>
						</p>
						<p class="flex min-w-0 items-center gap-1 text-xs text-muted-foreground">
							<Lock size={12} class="shrink-0" /><span class="min-w-0"
								>{approval.action === 'run_migration'
									? m['assistant.migration_note']({ seconds: approval.payload.timeout_seconds })
									: approval.payload.write
										? m['assistant.command_write_folder_note']({
												directory: approval.payload.working_directory,
												seconds: approval.payload.timeout_seconds
											})
										: m['assistant.command_read_only_note']({
												directory: approval.payload.working_directory,
												seconds: approval.payload.timeout_seconds
											})}</span
							>
						</p>
					{:else}
						{#if approval.payload.repository}<p class="flex min-w-0 items-center gap-1 text-xs text-muted-foreground">
								<GitBranch size={12} class="shrink-0" /><span class="break-words"
									>{m['assistant.approval_repository']({
										repository: approval.payload.repository,
										branch: approval.payload.base_branch ?? '',
										source: repositorySourceLabel(approval.payload.repository_source ?? 'workspace')
									})}</span
								>
							</p>{/if}
						{#if !offersWorkflow && approval.payload.workflow}<p
								class="flex min-w-0 items-center gap-1 text-xs text-muted-foreground"
							>
								<Workflow size={12} class="shrink-0" /><span class="break-words"
									>{m['workflows.choose_label']()}: {approval.payload.workflow}</span
								>
							</p>
							{#if approval.payload.workflow_id !== 'standard'}<p class="text-xs text-muted-foreground">
									{m['workflows.composer_note']()}
								</p>{/if}{/if}
					{/if}
					{#if (approval.action === 'delegate_issue' || approval.action === 'follow_up_task') && approval.payload.effort}
						<p class="text-xs text-muted-foreground">{m['tasks.effort']()}: {approval.payload.effort}</p>
					{/if}
					{#if approval.action === 'delegate_issue' || approval.action === 'follow_up_task' || approval.action === 'request_agent_review'}
						<div class="flex flex-wrap items-start gap-2 empty:hidden [&>div]:flex-[1_1_6rem]
							[&_[role=combobox]]:w-full [&_[role=combobox]]:max-w-full
							[&_[data-slot=select-trigger]]:h-7 [&_[data-slot=select-value]]:block [&_[data-slot=select-value]]:truncate">
							{#if approval.action === 'delegate_issue' && offersWorkflow && workflowList}
								<WorkflowChoice
									list={workflowList}
									id="approval-workflow-{approval.id}"
									bind:value={() => workflow, chooseWorkflow}
									disabled={deciding}
									extra={approval.payload.workflow_id
										? { id: approval.payload.workflow_id, name: approval.payload.workflow ?? '' }
										: undefined}
								/>
							{/if}
							{#if approval.action !== 'delegate_issue' || !isCustomChoice(workflow)}
								<ApprovalModelPicker
									{slug}
									issueId={approval.issue_id}
									proposed={approval.action === 'follow_up_task'
										? {
												harness: approval.payload.harness,
												model: approval.payload.model || approval.payload.current_model,
												server: approval.payload.model_server_id,
												current: !approval.payload.model
											}
										: approval.payload}
									harnessLocked={approval.action === 'follow_up_task'}
									disabled={deciding}
									onchange={(choice) => (modelChoice = choice)}
								/>
							{/if}
						</div>
					{/if}
					{#if approval.action === 'delegate_issue' || approval.action === 'follow_up_task'}
						<Markdown source={approval.payload.instructions} />
					{/if}
				</div>
			{/if}
		</div>
	</div>
	{#if error}<p role="alert" class="px-3 pb-2 text-xs text-destructive">{error}</p>{/if}
	{#if warning && !expanded}<p class="px-3 pb-1 text-xs font-medium sm:px-4">{warning}</p>{/if}
	{#if needsReview}<p id="approval-review-{approval.id}" class="px-3 pb-1 text-xs text-muted-foreground sm:px-4">{m['assistant.approval_review_details']()}</p>{/if}
	<div class="flex items-center gap-2 px-3 pb-2.5 sm:px-4" class:pt-2={expanded}>
		<div class="flex min-w-0 flex-wrap items-center gap-x-2 text-[11px] text-muted-foreground">
			<span class="inline-flex items-center gap-1.5"
				><span class="size-1.5 rounded-full bg-[var(--status-warning)]"></span>{m['automations.pending']()}</span
			>
			<time datetime={approval.created_at}
				>{new Date(approval.created_at).toLocaleTimeString(getLocale(), { hour: '2-digit', minute: '2-digit' })}</time
			>
			{#if approval.message_model}<span class="truncate" title={approval.message_model}>{approval.message_model}</span>{/if}
		</div>
		<div class="ml-auto flex shrink-0 items-center gap-1.5">
			<Button size="sm" variant="outline" disabled={deciding} onclick={() => void decide('deny')}
				>{m['assistant.approval_deny']()}</Button
			>
			<Button size="sm" disabled={deciding || invalid || needsReview} onclick={() => void decide('approve')}
				>{m['assistant.approval_approve']()}</Button
			>
		</div>
	</div>
</article>
