<script lang="ts">
	import { tick, untrack } from 'svelte';
	import { CheckCheck, ChevronRight, CircleHelp, CircleSlash, Flag, Info, RefreshCw } from '@lucide/svelte';
	import { page } from '$app/state';
	import * as Avatar from '$lib/components/ui/avatar/index.js';
	import * as Dialog from '$lib/components/ui/dialog/index.js';
	import RoomBlockerNotice from './RoomBlockerNotice.svelte';
	import DiffPanel from './DiffPanel.svelte';
	import { sendOptions } from './agent-conversation';
	import { Badge } from '$lib/components/ui/badge/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import * as Bubble from '$lib/components/ui/bubble/index.js';
	import * as Collapsible from '$lib/components/ui/collapsible/index.js';
	import * as Message from '$lib/components/ui/message/index.js';
	import * as MessageScroller from '$lib/components/ui/message-scroller/index.js';
	import { authState } from '$lib/features/auth/auth.state.svelte';
	import { appToast } from '$lib/features/toast/toast';
	import AgentLogo from '$lib/features/agents/AgentLogo.svelte';
	import { agentInfo } from '$lib/features/agents/agents';
	import { agentTasks, type AgentTask, type TaskMessage } from '$lib/api/agent-tasks';
	import type { WorkflowStepRun } from '$lib/api/workflows';
	import type { ConversationState } from './conversation-state.svelte';
	import type { TaskRun } from './task-run.svelte';
	import { stepDuration, type StepStatus as Status } from './agent-timeline';
	import AgentStepRun from './AgentStepRun.svelte';
	import AgentPostBody from './AgentPostBody.svelte';
	import CopyMessageButton from './CopyMessageButton.svelte';
	import TurnLine from './TurnLine.svelte';
	import { decides, decisionAnchor, factLabel, foldText, openRoomBlocker, roomRows, roomSplits, rowRoles, type FactLabel, type RoomRow, type RoomTurn } from './task-room';
	import { activeRun } from './task-column';
	import { firstLine, shownRows, turnSummary, workTokens, type ThreadFilter } from './thread-rows';
	import { formatDuration } from './shell-rules';
	import { getLocale, m } from '$lib/features/i18n/messages';

	let {
		chat,
		task,
		run,
		filter = { role: '', decisions: false },
		now = Date.now(),
		onreset
	}: {
		chat: ConversationState;
		task: AgentTask;
		run: TaskRun;
		filter?: ThreadFilter;
		// The current time, ticking while a turn runs.
		now?: number;
		// Shows every line again when the filter keeps none.
		onreset?: () => void;
	} = $props();
	type Turn = Extract<RoomRow, { type: 'member' }>;
	type Facts = Extract<RoomRow, { type: 'facts' }>;

	const room = $derived(chat.taskRooms[task.id] ?? []);
	// A turn is a row from its run, before it posts (KUA-321): a running turn
	// is never missing. A run names its role; the task's own attempts, its
	// writer's turns, may not.
	const writer = $derived(run.pipeline?.roster.find((member) => member.writer)?.role);
	const turns = $derived(
		(run.view?.runs ?? []).flatMap((item): RoomTurn[] => {
			const role = item.role ?? (item.task_id === task.id ? writer : undefined);
			return role ? [{ id: item.id, role, at: item.started_at ?? item.created_at }] : [];
		})
	);
	const pinned = $derived(new Set(task.pending_decisions?.filter((item) => item.kind === 'ask' || item.kind === 'request').map((item) => item.seq)));
	const threadPosts = $derived(room.filter((post) => !pinned.has(post.seq)));
	const rows = $derived(roomRows(threadPosts, turns));
	const shown = $derived(shownRows(rows, filter, rowRoles, (row) => !!row.decision));
	const posts = $derived(threadPosts.filter((post) => post.author_kind !== 'kuayle'));
	const conversation = $derived(shownRows(posts, filter,
		(post) => [...(post.author_role ? [post.author_role] : []), ...(post.to_roles ?? [])], decides));
	const bySeq = $derived(new Map(room.map((post) => [post.seq, post])));
	const writable = $derived(sendOptions(task, authState.user?.id).open);
	const author = (post: TaskMessage) => post.author_kind === 'human' ? m['tasks.live_from_you']() :
		post.author_kind === 'kuayle' ? m['tasks.room_kuayle']() : '@' + post.author_role;
	let followed = '';
	$effect(() => {
		const anchor = page.url.hash.slice(1);
		if (!anchor) { followed = ''; return; }
		if (anchor === followed) return;
		const decision = task.pending_decisions?.find((item) =>
			decisionAnchor(task.id, item.kind, item.seq) === anchor ||
			((item.kind === 'ask' || item.kind === 'request') && 'room-post-' + item.seq === anchor));
		if (!decision && !posts.some((post) => 'room-post-' + post.seq === anchor)) return;
		followed = anchor;
		if (!decision) untrack(() => onreset?.());
		const target = decision ? decisionAnchor(task.id, decision.kind, decision.seq) : anchor;
		void tick().then(() => document.getElementById(target)?.focus());
	});
	const blocker = $derived(run.view?.status === 'paused' || task.workflow_paused_at ? openRoomBlocker(room, task.pending_decisions) : undefined);
	const splits = $derived(roomSplits(room));
	let approving = $state<number | null>(null);

	async function approveSplit(seq: number) {
		if (approving !== null) return;
		approving = seq;
		try {
			await agentTasks.approveSplit(chat.slug, task.id, seq);
			await run.loadRoom();
		} catch (error) {
			appToast.apiError(error, m['issue.toast.failed_sub_issues']());
		} finally {
			approving = null;
		}
	}

	// A turn opens the run that wrote it. Runs are read with the task's run;
	// one the room names before that read has it read again, once.
	const runs = $derived(new Map((run.view?.runs ?? []).map((item) => [item.id, item])));
	const asked = new Set<string>();
	$effect(() => {
		const unknown = rows.filter((row) => row.type === 'member' && row.runId && !runs.has(row.runId) && !asked.has(row.runId));
		if (!unknown.length) return;
		for (const row of unknown) asked.add((row as Turn).runId!);
		untrack(() => void run.load());
	});
	// A running role stays visible even before it has posted.
	const live = $derived(new Set([...runs.values()].filter((item) => activeRun.has(item.status)).map((item) => item.id)));
	const turnStatus = (status: string): Status | undefined =>
		({
			queued: 'waiting',
			starting: 'running',
			running: 'running',
			waiting_input: 'running',
			succeeded: 'done',
			failed: 'failed',
			timeout: 'failed',
			cancelled: 'stopped'
		})[status] as Status | undefined;
	function turnTime(item: WorkflowStepRun, status: Status | undefined) {
		const ms = stepDuration(
			{ startedAt: item.started_at ?? item.created_at, finishedAt: item.completed_at ?? undefined, status: status ?? 'done' },
			now
		);
		return ms === undefined || ms < 1000 ? '' : formatDuration(ms);
	}
	let opened = $state<Record<string, boolean>>({});
	const working = $derived(shown.filter((row): row is Turn =>
		row.type === 'member' && !!row.runId && live.has(row.runId) && !row.messages.length));
	const system = $derived(shown.filter((row) => row.type === 'facts' || row.type === 'kuayle' ||
		(row.type === 'member' && row.runId && !live.has(row.runId))));
	let output = $state<string | null>(null);
	let outputTrigger: HTMLElement | null = null;
	function showOutput(id: string | null, event: MouseEvent) {
		outputTrigger = event.currentTarget as HTMLElement;
		output = id;
	}
	const selected = $derived(output ? rows.find((row): row is Turn => row.type === 'member' && row.runId === output) : undefined);
	const phaseLabel = (id = '') => run.pipeline?.phases.find((phase) => phase.id === id)?.label || id;
	const verdictLabel: Record<string, () => string> = {
		approve: m['tasks.review_verdict_approve'],
		request_changes: m['tasks.review_verdict_request_changes'],
		comment: m['tasks.review_verdict_comment']
	};
	// What a fact of Kuayle's means, in a few words.
	const words: Record<string, (label: FactLabel) => string> = {
		pr_opened: () => m['tasks.fact_pr_opened'](),
		pr_missing: () => m['tasks.fact_pr_missing'](),
		pr_draft: () => m['tasks.fact_pr_draft'](),
		pr_ready: () => m['tasks.fact_pr_ready'](),
		pr_open: () => m['tasks.fact_pr_open'](),
		merged: () => m['tasks.fact_merged'](),
		closed: () => m['tasks.fact_closed'](),
		up_to_date: () => m['tasks.fact_up_to_date'](),
		behind: () => m['tasks.fact_behind'](),
		conflict: () => m['tasks.fact_conflict'](),
		no_conflict: () => m['tasks.fact_no_conflict'](),
		collision: () => m['tasks.fact_collision'](),
		no_collision: () => m['tasks.fact_no_collision'](),
		ci_running: () => m['tasks.fact_ci_running'](),
		ci_passed: () => m['tasks.fact_ci_passed'](),
		ci_failed: () => m['tasks.fact_ci_failed'](),
		ci_waiting: () => m['tasks.fact_ci_waiting'](),
		ci_unknown: () => m['tasks.fact_ci_unknown'](),
		approved: () => m['tasks.fact_approved'](),
		not_approved: () => m['tasks.fact_not_approved'](),
		no_overlap: () => m['tasks.fact_no_overlap'](),
		head: (label) => m['tasks.fact_head']({ commit: (label as { commit: string }).commit }),
		base: (label) => m['tasks.fact_base']({ branch: (label as { branch: string }).branch }),
		overlap: (label) => m['tasks.fact_overlap']({ issues: (label as { issues: string }).issues }),
		script_passed: (label) => m['tasks.fact_script_passed']({ check: (label as { check: string }).check }),
		script_failed: (label) => m['tasks.fact_script_failed']({ check: (label as { check: string }).check }),
		text: (label) => (label as { text: string }).text
	};
	const say = (label: FactLabel) => words[label.kind](label);
	const then = (first: string, next: string) => m['tasks.fact_then']({ first, next });

	// What a structured post says besides its text; a refused post keeps
	// only its text.
	function headline(message: TaskMessage): string {
		const data = message.data ?? {};
		if (message.reject_reason) return '';
		switch (message.kind) {
			case 'phase':
				return m['tasks.room_phase']({ phase: phaseLabel(data.phase) });
			case 'request':
				if (data.action === 'approve' && data.commit) {
					return m['tasks.room_merge_approved']({ commit: data.commit.slice(0, 7) });
				}
				return m['tasks.room_request']({
					action: data.action === 'split' ? m['sharedComponents.filter_builder.sub_issues']() : data.action ?? ''
				});
			case 'waiver': {
				const check = data.check ?? '';
				if (data.standing) return m['tasks.room_waiver_standing']({ check });
				const waiver = m['tasks.room_waiver']({ check });
				return data.commit ? `${waiver} · ${m['tasks.room_commit']({ commit: data.commit.slice(0, 7) })}` : waiver;
			}
			case 'grant':
				if (data.action === 'approve' && !data.check) return m['tasks.room_grant_approval']();
				if (data.action === 'split') return m['tasks.room_grant_split']();
				return m['tasks.room_grant']({ check: data.check ?? '' });
			case 'fact':
				return message.body ? '' : say(factLabel(message));
		}
		return '';
	}
	const clock = (value: string) =>
		new Date(value).toLocaleTimeString(getLocale(), { hour: '2-digit', minute: '2-digit' });
	const full = (value: string) =>
		new Date(value).toLocaleString(getLocale(), { dateStyle: 'medium', timeStyle: 'medium' });
</script>

{#snippet when(value: string)}
	<time datetime={value} title={full(value)} class="shrink-0 tabular-nums">{clock(value)}</time>
{/snippet}

{#snippet chevron()}
	<ChevronRight
		size={13}
		aria-hidden="true"
		class="shrink-0 text-muted-foreground/60 transition-transform group-aria-expanded:rotate-90 motion-reduce:transition-none"
	/>
{/snippet}

{#snippet footer(message: TaskMessage)}
	{#if message.reject_reason}<p class="flex min-w-0 items-start gap-1.5 px-1 text-[11px] text-muted-foreground">
			<CircleSlash size={13} aria-hidden="true" class="mt-px shrink-0" /><span class="min-w-0 break-words"
				>{m['tasks.room_rejected']({ reason: message.reject_reason })}</span
			>
		</p>{/if}
	<Message.Footer class="gap-1.5 px-1 text-[11px] font-normal">
		{#if !message.reject_reason && message.kind === 'ask'}<span
				role="img"
				aria-label={m['assistant.agent_question']()}
				title={m['assistant.agent_question']()}
				class="inline-flex shrink-0 text-[var(--status-warning)]"><CircleHelp size={13} aria-hidden="true" /></span
			>{/if}
		{@render when(message.created_at)}
		{#if message.run_id}
			<Button variant="link" size="xs" class="h-auto px-1 text-xs text-muted-foreground" aria-haspopup="dialog" onclick={(event) => showOutput(message.run_id, event)}>{m['tasks.room_output']()}</Button>
		{/if}
		{#if writable && message.author_kind === 'member' && !message.reject_reason}
			<Button variant="ghost" size="xs" class="h-auto px-1 text-xs text-muted-foreground" onclick={() => {
				chat.roomReply = message;
				chat.composerFocus++;
			}}>{m['tasks.room_reply']()}</Button>
		{/if}
	</Message.Footer>
{/snippet}

{#snippet direction(message: TaskMessage)}
	{@const parent = message.reply_to == null ? undefined : bySeq.get(message.reply_to)}
	<p class="flex flex-wrap items-baseline gap-x-1.5 px-1 text-xs">
		<span class="font-semibold">{author(message)}</span>
		<span class="text-muted-foreground">→ {message.to_roles?.length ? message.to_roles.map((role) => '@' + role).join(', ') : '@all'}</span>
	</p>
	{#if parent}
		<blockquote class="max-w-full border-l-2 pl-2 text-xs text-muted-foreground">{author(parent)} · {firstLine(parent.body || headline(parent) || '#' + parent.seq, 140)}</blockquote>
	{/if}
{/snippet}

{#snippet ownerPost(message: TaskMessage)}
	{@const head = headline(message)}
	<Message.Root align="end" role="article" aria-label={m['tasks.live_from_you']()} data-seq={message.seq}>
		<Message.Content class="items-end gap-1">
			{@render direction(message)}
			{#if head}<p class="px-1 text-xs text-muted-foreground">{head}</p>{/if}
			{#if message.body}<Bubble.Root variant="muted" class="max-w-[92%]"
					><Bubble.Content class="rounded-2xl rounded-br-md ps-3.5 pe-9 py-2 break-words whitespace-pre-wrap"
						>{message.body}</Bubble.Content
					>
					<CopyMessageButton text={message.body} />
				</Bubble.Root>{/if}
			{@render footer(message)}
		</Message.Content>
	</Message.Root>
{/snippet}

{#snippet kuaylePost(message: TaskMessage)}
	{@const head = headline(message)}
	<Message.Root align="start" role="article" aria-label={m['tasks.room_kuayle']()} data-seq={message.seq}>
		<Message.Content>
			<Bubble.Root variant="ghost" class="max-w-full"
				><Bubble.Content class="flex min-w-0 items-start gap-2 text-xs text-muted-foreground">
					{#if message.kind === 'phase'}<Flag size={13} aria-hidden="true" class="mt-px shrink-0" />{:else if message.kind === 'fact' && message.data?.fact === 'approved'}<CheckCheck
							size={13}
							aria-hidden="true"
							class="mt-px shrink-0 text-[var(--status-success)]"
						/>{:else}<Info size={13} aria-hidden="true" class="mt-px shrink-0" />{/if}
					<span class="min-w-0 flex-1 break-words" data-selectable="text"
						><span class="font-medium text-foreground">{m['tasks.room_kuayle']()}</span>
						· {#if head}{head}{/if}{#if head && message.body}<br />{/if}<span class="whitespace-pre-wrap"
							>{message.body}</span
						></span
					>
					{@render when(message.created_at)}
				</Bubble.Content></Bubble.Root
			>
		</Message.Content>
	</Message.Root>
{/snippet}

{#snippet facts(row: Facts)}
	{@const last = row.messages[row.messages.length - 1]}
	<Message.Root align="start" role="article" aria-label={m['tasks.room_kuayle']()} data-row="facts">
		<Collapsible.Root
			class="min-w-0 flex-1"
			bind:open={() => opened[row.key] ?? false, (open) => (opened[row.key] = open)}
		>
			<Collapsible.Trigger
				class="group flex w-full min-w-0 cursor-pointer items-center gap-2 rounded-md py-1 text-left text-xs text-muted-foreground outline-none hover:text-foreground focus-visible:ring-3 focus-visible:ring-ring/50"
			>
				<Info size={13} aria-hidden="true" class="shrink-0" />
				<span class="min-w-0 flex-1 truncate"
					><span class="font-medium text-foreground">{m['tasks.room_kuayle']()}</span> · {foldText(
						row.messages,
						say,
						then
					)}</span
				>
				{@render when(last.created_at)}
				{@render chevron()}
			</Collapsible.Trigger>
			<Collapsible.Content>
				{#if opened[row.key]}<ol
						class="mt-1 ml-1.5 flex min-w-0 flex-col gap-2 border-l py-1.5 pl-4 text-xs sm:pl-5"
						aria-label={m['tasks.room_facts']()}
					>
						{#each row.messages as message (message.seq)}<li class="min-w-0" data-seq={message.seq}>
								<p class="flex min-w-0 items-baseline gap-2">
									<span class="min-w-0 flex-1 break-words text-foreground">{say(factLabel(message))}</span>
									<span class="text-muted-foreground">{@render when(message.created_at)}</span>
								</p>
								{#if message.body}<p class="break-words whitespace-pre-wrap text-muted-foreground" data-selectable="text">
										{message.body}
									</p>{/if}
								{#if message.to_roles?.length}<p class="text-muted-foreground">
										{m['tasks.room_to']({ roles: message.to_roles.join(', ') })}
									</p>{/if}
							</li>{/each}
					</ol>{/if}
			</Collapsible.Content>
		</Collapsible.Root>
	</Message.Root>
{/snippet}

{#snippet memberPost(message: TaskMessage)}
	{@const head = headline(message)}
	{@const refused = !!message.reject_reason}
	{@const split = splits.get(message.seq)}
	<div class="flex min-w-0 flex-col gap-1" data-seq={message.seq} data-refused={refused || undefined}>
		{#if message.kind === 'verdict' && !refused && message.data?.verdict}<div class="flex flex-wrap items-center gap-1.5 px-1">
				<Badge variant="outline">{verdictLabel[message.data.verdict]?.() ?? message.data.verdict}</Badge>
				{#if message.data.commit}<span class="font-mono text-xs text-muted-foreground"
						>{m['tasks.room_commit']({ commit: message.data.commit.slice(0, 7) })}</span
					>{/if}
			</div>{/if}
		{#if head}<p class="px-1 text-xs text-muted-foreground">{head}</p>{/if}
		{#if message.body}<Bubble.Root variant="outline" class="max-w-[92%]"
				><Bubble.Content
					data-selectable="text"
					class="rounded-2xl rounded-bl-md border ps-3.5 pe-9 py-2 {message.kind === 'ask' &&
					!refused
						? 'border-[var(--status-warning)]/50'
						: ''} {refused ? 'text-muted-foreground' : ''}">
					<AgentPostBody source={message.body} />
				</Bubble.Content>
				<CopyMessageButton text={message.body} />
			</Bubble.Root>{/if}
		{#if split}
			<ol class="ms-5 list-decimal space-y-2 text-sm" data-selectable="text">
				{#each split.issues as issue}
					<li class="break-words">
						<span class="font-medium">{issue.title}</span>
						<p class="whitespace-pre-wrap text-muted-foreground">{issue.description}</p>
					</li>
				{/each}
			</ol>
			{#if task.pending_decisions == null && split.pending && task.creator_id === authState.user?.id}
				<Button variant="outline" size="sm" class="w-fit" disabled={approving !== null}
					onclick={() => void approveSplit(message.seq)}>{m['tasks.room_create_sub_issues']()}</Button>
			{/if}
		{/if}
		{@render footer(message)}
	</div>
{/snippet}

{#snippet turn(row: Turn)}
	{@const known = row.runId ? runs.get(row.runId) : undefined}
	{@const status = known ? turnStatus(known.status) : undefined}
	{#snippet marks()}
		{#if row.verdict}<Badge variant="outline" class="h-5 shrink-0 px-1.5 text-[11px] font-normal"
				>{verdictLabel[row.verdict]?.() ?? row.verdict}</Badge
			>{/if}
		{#if row.asks}<span
				role="img"
				aria-label={m['assistant.agent_question']()}
				title={m['assistant.agent_question']()}
				class="inline-flex shrink-0 text-[var(--status-warning)]"><CircleHelp size={14} aria-hidden="true" /></span
			>{/if}
	{/snippet}
	<Message.Root align="start" role="article" aria-label={row.role} data-row="turn">
		<Button variant="plain" aria-haspopup="dialog" onclick={(event) => showOutput(row.runId, event)} class="group flex w-full min-w-0 items-center gap-2 rounded-md py-1.5 text-left focus-visible:ring-2">
				<TurnLine
					role={row.role}
					summary={turnSummary(known, row.messages) || (live.has(row.runId ?? '') ? m['tasks.member_working']() : '')}
					duration={known ? turnTime(known, status) : ''}
					startedAt={known?.started_at ?? known?.created_at}
					tokens={known ? workTokens([known]) : undefined}
					{status}
					{marks}
				/>
		</Button>
	</Message.Root>
{/snippet}

{#if blocker}
	<div class="border-l-2 border-[var(--status-warning)] pl-3">
		<RoomBlockerNotice post={blocker} {chat} onfollow={onreset} />
	</div>
{/if}
<ol aria-label={m['tasks.room']()} class="flex min-w-0 flex-col gap-4 py-2" data-testid="task-room">
	{#each conversation as message (message.seq)}
		<MessageScroller.Item messageId={'seq-' + message.seq}>
			{#snippet child({ props })}
				<li {...props} id={'room-post-' + message.seq} tabindex="-1" class="min-w-0 scroll-m-3 rounded-md focus-visible:ring-2 focus-visible:ring-ring">
					{#if message.author_kind === 'human'}
						{@render ownerPost(message)}
					{:else}
						<Message.Root role="article" aria-label={message.author_role ?? ''}>
							<Avatar.Root size="sm" aria-hidden="true">
								<Avatar.Fallback>{message.author_role?.slice(0, 1).toUpperCase()}</Avatar.Fallback>
							</Avatar.Root>
							<Message.Content class="gap-1">
								{@render direction(message)}
								{@render memberPost(message)}
							</Message.Content>
						</Message.Root>
					{/if}
				</li>
			{/snippet}
		</MessageScroller.Item>
	{/each}
</ol>
{#each working as row (row.key)}{@render turn(row)}{/each}
{#if system.length}
	<Collapsible.Root>
		<Collapsible.Trigger class="group inline-flex items-center gap-1.5 rounded-md py-1 text-xs text-muted-foreground hover:text-foreground focus-visible:ring-2">
			{@render chevron()} {m['tasks.room_system']()}
		</Collapsible.Trigger>
		<Collapsible.Content class="space-y-1 border-l pl-3">
			{#each system as row (row.key)}
				{#if row.type === 'kuayle'}{@render kuaylePost(row.message)}{:else if row.type === 'facts'}{@render facts(row)}{:else if row.type === 'member'}{@render turn(row)}{/if}
			{/each}
		</Collapsible.Content>
	</Collapsible.Root>
{/if}
{#if posts.length && !conversation.length}<div class="flex flex-wrap items-center gap-2" role="status">
		<p class="text-sm text-muted-foreground">{m['tasks.filter_empty']()}</p>
		{#if onreset}<Button variant="ghost" size="sm" onclick={onreset}>{m['tasks.filter_reset']()}</Button>{/if}
	</div>{/if}
{#if !pinned.size && !posts.length && !working.length && !run.roomFailed}<p class="text-sm text-muted-foreground" role="status">
		{run.roomLoaded ? m['tasks.room_empty']() : m['assistant.loading']()}
	</p>{/if}

<Dialog.Root open={!!output} onOpenChange={(open) => { if (!open) output = null; }}>
	<Dialog.Content class="max-h-[85dvh] overflow-y-auto sm:max-w-3xl" onCloseAutoFocus={(event) => {
		if (outputTrigger?.isConnected) { event.preventDefault(); outputTrigger.focus(); }
	}}>
		<Dialog.Header>
			<Dialog.Title>{m['tasks.room_output']()}</Dialog.Title>
			<Dialog.Description>{selected?.role}</Dialog.Description>
		</Dialog.Header>
		{#if output && runs.get(output)}
			{@const item = runs.get(output)!}
			<p class="flex items-center gap-2 text-xs text-muted-foreground" data-testid="turn-details">
				{#if item.provider}<AgentLogo agent={item.provider} size="sm" />{agentInfo(item.provider).name}{/if}
				{item.model} · {@render when(item.started_at ?? item.created_at)}
			</p>
			<AgentStepRun {chat} run={item} active={item.status === 'running'} pending={live.has(item.id)} />
			{#if !live.has(item.id)}
				<DiffPanel slug={chat.slug} taskId={item.task_id} runId={item.id} title={selected?.role ?? ''} />
			{/if}
		{:else}
			<p role="status" class="text-sm text-muted-foreground">{m['tasks.attempt_missing']()}</p>
			<Button variant="outline" size="sm" onclick={() => void run.load()}>{m['tasks.refresh']()}</Button>
		{/if}
	</Dialog.Content>
</Dialog.Root>

{#if run.roomFailed}<div class="flex flex-wrap items-center gap-2">
		<p role="alert" class="text-sm text-destructive">{m['tasks.load_error']()}</p>
		<Button variant="ghost" size="sm" onclick={() => void run.loadRoom()}><RefreshCw />{m['tasks.refresh']()}</Button>
	</div>{/if}
