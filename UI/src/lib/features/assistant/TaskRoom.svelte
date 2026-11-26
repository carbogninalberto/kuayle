<script lang="ts">
	import { untrack } from 'svelte';
	import { CheckCheck, ChevronRight, CircleHelp, CircleSlash, Flag, Info, RefreshCw } from '@lucide/svelte';
	import { Badge } from '$lib/components/ui/badge/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import * as Bubble from '$lib/components/ui/bubble/index.js';
	import * as Collapsible from '$lib/components/ui/collapsible/index.js';
	import * as Message from '$lib/components/ui/message/index.js';
	import * as MessageScroller from '$lib/components/ui/message-scroller/index.js';
	import AgentLogo from '$lib/features/agents/AgentLogo.svelte';
	import { agentInfo } from '$lib/features/agents/agents';
	import { type AgentTask, type TaskMessage } from '$lib/api/agent-tasks';
	import type { WorkflowStepRun } from '$lib/api/workflows';
	import type { ConversationState } from './conversation-state.svelte';
	import type { TaskRun } from './task-run.svelte';
	import { stepDuration, type StepStatus as Status } from './agent-timeline';
	import AgentStepRun from './AgentStepRun.svelte';
	import AgentPostBody from './AgentPostBody.svelte';
	import CopyMessageButton from './CopyMessageButton.svelte';
	import TurnLine from './TurnLine.svelte';
	import { factLabel, foldText, openByDefault, roomRows, roomSplits, rowRoles, type FactLabel, type RoomRow, type RoomTurn } from './task-room';
	import { activeRun } from './task-column';
	import { shownRows, turnSummary, workTokens, type ThreadFilter } from './thread-rows';
	import { formatDuration } from './shell-rules';
	import { getLocale, m } from '$lib/features/i18n/messages';

	// The room of a task on a pipeline (KUA-252), one line per event
	// (KUA-286): what the owner wrote, Kuayle's facts in a row folded into one
	// line that opens to each with its reason, Kuayle's phase moves, and each
	// role's turn as one line (role, what it ended with, how long, the tokens,
	// how it went) that opens to the harness, what the turn posted and the
	// run's transcript. The thread's filter keeps one role's lines or the
	// decisions. The room is read whole once and kept by its events
	// (ConversationState); it is read on from the last unbroken post when the
	// task's run is read again (its events and poll) or when a post arrives
	// after a gap.
	// A post the room refused is muted, with the reason.
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
	const pinned = $derived(
		new Set(task.pending_decisions?.filter((d) => d.kind === 'request' || d.kind === 'ask').map((d) => d.seq))
	);
	const rows = $derived(
		roomRows(
			room.filter((post) => !pinned.has(post.seq)),
			turns
		)
	);
	const shown = $derived(shownRows(rows, filter, rowRoles, (row) => !!row.decision));
	const splits = $derived(roomSplits(room));
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
	// The turns that have not ended: they start open, with their stream.
	const live = $derived(new Set([...runs.values()].filter((item) => activeRun.has(item.status)).map((item) => item.id)));
	const turnRun = (row: Turn): WorkflowStepRun | undefined =>
		row.runId
			? (runs.get(row.runId) ?? {
					id: row.runId,
					task_id: task.id,
					provider: '',
					model: '',
					status: '',
					created_at: row.messages[0]?.created_at ?? row.startedAt ?? ''
				})
			: undefined;
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
	// A turn holding a split the owner has not approved, or still running,
	// starts open; a running one folds when it ends.
	const turnOpen = (row: RoomRow) => opened[row.key] ?? openByDefault(row, splits, live);

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
		{#if message.to_roles?.length}<span class="min-w-0 truncate">{m['tasks.room_to']({ roles: message.to_roles.join(', ') })}</span>{/if}
		{@render when(message.created_at)}
	</Message.Footer>
{/snippet}

{#snippet ownerPost(message: TaskMessage)}
	{@const head = headline(message)}
	<Message.Root align="end" role="article" aria-label={m['tasks.live_from_you']()} data-seq={message.seq}>
		<Message.Content class="items-end gap-1">
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
		{/if}
		{@render footer(message)}
	</div>
{/snippet}

{#snippet turn(row: Turn)}
	{@const item = turnRun(row)}
	{@const status = item ? turnStatus(item.status) : undefined}
	{@const known = item && runs.has(item.id) ? item : undefined}
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
		<Collapsible.Root class="min-w-0 flex-1" bind:open={() => turnOpen(row), (open) => (opened[row.key] = open)}>
			<Collapsible.Trigger
				class="group flex w-full min-w-0 cursor-pointer items-center gap-2.5 rounded-md py-1.5 text-left outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
			>
				<TurnLine
					role={row.role}
					summary={turnSummary(known, row.messages) || (live.has(row.runId ?? '') ? m['tasks.member_working']() : '')}
					duration={known ? turnTime(known, status) : ''}
					startedAt={known?.started_at ?? known?.created_at}
					tokens={known ? workTokens([known]) : undefined}
					{status}
					{marks}
				/>
			</Collapsible.Trigger>
			<Collapsible.Content>
				{#if turnOpen(row)}<div class="ml-2.5 flex min-w-0 flex-col gap-3 border-l py-3 pl-4 sm:pl-5">
						{#if known}<p class="flex min-w-0 flex-wrap items-center gap-x-1.5 gap-y-1 text-xs text-muted-foreground" data-testid="turn-details">
								{#if known.provider}<AgentLogo agent={known.provider} size="sm" class="size-4" /><span
										>{agentInfo(known.provider).name}</span
									>{/if}
								{#if known.model}<span class="min-w-0 truncate">· {known.model}</span>{/if}
								<span>·</span>{@render when(known.started_at ?? known.created_at)}
							</p>{/if}
						{#each row.messages as message (message.seq)}{@render memberPost(message)}{/each}
						{#if item}<AgentStepRun
								{chat}
								run={item}
								active={item.status === 'running'}
								pending={item.status === 'running' || item.status === 'queued'}
							/>{/if}
					</div>{/if}
			</Collapsible.Content>
		</Collapsible.Root>
	</Message.Root>
{/snippet}

<ol aria-label={m['tasks.room']()} class="flex min-w-0 flex-col gap-2 py-2" data-testid="task-room">
	{#each shown as row (row.key)}
		<MessageScroller.Item messageId={row.key}>
			{#snippet child({ props })}
				<li {...props}>
					{#if row.type === 'owner'}{@render ownerPost(row.message)}{:else if row.type === 'kuayle'}{@render kuaylePost(
							row.message
						)}{:else if row.type === 'facts'}{@render facts(row)}{:else if row.type === 'member'}{@render turn(row)}{/if}
				</li>
			{/snippet}
		</MessageScroller.Item>
	{/each}
</ol>
{#if rows.length && !shown.length}<div class="flex flex-wrap items-center gap-2" role="status">
		<p class="text-sm text-muted-foreground">{m['tasks.filter_empty']()}</p>
		{#if onreset}<Button variant="ghost" size="sm" onclick={onreset}>{m['tasks.filter_reset']()}</Button>{/if}
	</div>{/if}
{#if !room.length && !rows.length && !run.roomFailed}<p class="text-sm text-muted-foreground" role="status">
		{run.roomLoaded ? m['tasks.room_empty']() : m['assistant.loading']()}
	</p>{/if}
{#if run.roomFailed}<div class="flex flex-wrap items-center gap-2">
		<p role="alert" class="text-sm text-destructive">{m['tasks.load_error']()}</p>
		<Button variant="ghost" size="sm" onclick={() => void run.loadRoom()}><RefreshCw />{m['tasks.refresh']()}</Button>
	</div>{/if}
