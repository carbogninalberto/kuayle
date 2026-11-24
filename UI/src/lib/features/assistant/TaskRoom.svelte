<script lang="ts">
	import { untrack } from 'svelte';
	import { ChevronRight, CircleHelp, CircleSlash, Flag, Info, RefreshCw, ScrollText } from '@lucide/svelte';
	import { Badge } from '$lib/components/ui/badge/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import * as Bubble from '$lib/components/ui/bubble/index.js';
	import * as Collapsible from '$lib/components/ui/collapsible/index.js';
	import * as Message from '$lib/components/ui/message/index.js';
	import * as MessageScroller from '$lib/components/ui/message-scroller/index.js';
	import AgentLogo from '$lib/features/agents/AgentLogo.svelte';
	import { agentTasks, type AgentTask, type TaskMessage } from '$lib/api/agent-tasks';
	import type { WorkflowStepRun } from '$lib/api/workflows';
	import type { ConversationState } from './conversation-state.svelte';
	import type { TaskRun } from './task-run.svelte';
	import type { StepStatus as Status } from './agent-timeline';
	import AgentStepRun from './AgentStepRun.svelte';
	import CopyMessageButton from './CopyMessageButton.svelte';
	import StepStatus from './StepStatus.svelte';
	import { ROOM_PAGE, roomRead, roomRows, type RoomRow } from './task-room';
	import { getLocale, m } from '$lib/features/i18n/messages';

	// The room of a task on a pipeline (KUA-252), plainly, in its order: what
	// the owner wrote, what Kuayle posted (facts, phase moves) and each role's
	// turn as one row that opens the run's transcript. The room is read whole
	// once and kept by its events (ConversationState); it is read on from the
	// last unbroken post when the task's run is read again (its events and
	// poll) or when a post arrives after a gap. Text from roles is untrusted
	// and shown as plain text only. A post the room refused is muted, with
	// the reason.
	let { chat, task, run }: { chat: ConversationState; task: AgentTask; run: TaskRun } = $props();
	type Turn = Extract<RoomRow, { type: 'member' }>;

	const room = $derived(chat.taskRooms[task.id] ?? []);
	const rows = $derived(roomRows(room));
	let loaded = $state(false);
	let failed = $state(false);
	let loading = false,
		again = false,
		disposed = false;
	$effect(() => () => (disposed = true));
	// The task has a room: the composer writes to it.
	$effect(() => untrack(() => chat.acceptRoomMessages(task.id, [])));

	async function load() {
		if (loading) {
			again = true;
			return;
		}
		loading = true;
		try {
			let after = roomRead(chat.taskRooms[task.id] ?? []);
			for (let page = 0; page < 50; page++) {
				const messages = (await agentTasks.room(chat.slug, task.id, after)).messages ?? [];
				if (disposed) return;
				chat.acceptRoomMessages(task.id, messages);
				if (messages.length < ROOM_PAGE) break;
				after = messages[messages.length - 1].seq;
			}
			loaded = true;
			failed = false;
		} catch {
			if (!disposed) failed = true;
		} finally {
			loading = false;
			if (again && !disposed) {
				again = false;
				void load();
			}
		}
	}
	$effect(() => {
		void run.view;
		untrack(() => void load());
	});
	$effect(() => {
		void chat.taskRoomCursor[task.id];
		untrack(() => {
			if (roomRead(room) < (room.at(-1)?.seq ?? 0)) void load();
		});
	});

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
	const turnRun = (row: Turn): WorkflowStepRun | undefined =>
		row.runId
			? (runs.get(row.runId) ?? {
					id: row.runId,
					task_id: task.id,
					provider: '',
					model: '',
					status: '',
					created_at: row.messages[0].created_at
				})
			: undefined;
	const turnStatus = (status: string): Status | undefined =>
		({
			queued: 'waiting',
			running: 'running',
			succeeded: 'done',
			failed: 'failed',
			timeout: 'failed',
			cancelled: 'stopped'
		})[status] as Status | undefined;
	let opened = $state<Record<string, boolean>>({});

	const phaseLabel = (id = '') => run.pipeline?.phases.find((phase) => phase.id === id)?.label || id;
	const verdictLabel: Record<string, () => string> = {
		approve: m['tasks.review_verdict_approve'],
		request_changes: m['tasks.review_verdict_request_changes'],
		comment: m['tasks.review_verdict_comment']
	};
	// What a structured post says besides its text; a refused post keeps
	// only its text.
	function headline(message: TaskMessage): string {
		const data = message.data ?? {};
		if (message.reject_reason) return '';
		switch (message.kind) {
			case 'phase':
				return m['tasks.room_phase']({ phase: phaseLabel(data.phase) });
			case 'request':
				return m['tasks.room_request']({ action: data.action ?? '' });
			case 'waiver':
				return m['tasks.room_waiver']({ check: data.check ?? '' });
			case 'grant':
				return m['tasks.room_grant']({ check: data.check ?? '' });
			case 'fact':
				return message.body ? '' : `${data.fact ?? ''}: ${JSON.stringify(data.value ?? null)}`;
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
					{#if message.kind === 'phase'}<Flag size={13} aria-hidden="true" class="mt-px shrink-0" />{:else}<Info
							size={13}
							aria-hidden="true"
							class="mt-px shrink-0"
						/>{/if}
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

{#snippet memberPost(message: TaskMessage)}
	{@const head = headline(message)}
	{@const refused = !!message.reject_reason}
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
					class="rounded-2xl rounded-bl-md border ps-3.5 pe-9 py-2 break-words whitespace-pre-wrap {message.kind === 'ask' &&
					!refused
						? 'border-[var(--status-warning)]/50'
						: ''} {refused ? 'text-muted-foreground' : ''}">{message.body}</Bubble.Content
				>
				<CopyMessageButton text={message.body} />
			</Bubble.Root>{/if}
		{@render footer(message)}
	</div>
{/snippet}

{#snippet turn(row: Turn)}
	{@const turnRunValue = turnRun(row)}
	{@const status = turnRunValue ? turnStatus(turnRunValue.status) : undefined}
	<Message.Root align="start" role="article" aria-label={row.role} data-row="turn">
		<Message.Content class="gap-1.5">
			<Message.Header class="gap-1.5 px-1">
				{#if turnRunValue?.provider}<AgentLogo agent={turnRunValue.provider} size="sm" />{/if}
				<span class="shrink-0 capitalize text-foreground">{row.role}</span>
				{#if turnRunValue?.model}<span class="min-w-0 truncate font-normal">· {turnRunValue.model}</span>{/if}
			</Message.Header>
			{#each row.messages as message (message.seq)}{@render memberPost(message)}{/each}
			{#if turnRunValue}
				<Collapsible.Root bind:open={() => opened[row.key] ?? false, (open) => (opened[row.key] = open)}>
					<Collapsible.Trigger
						class="group flex w-fit max-w-full cursor-pointer items-center gap-1.5 rounded-md px-1 py-0.5 text-xs text-muted-foreground outline-none hover:text-foreground focus-visible:ring-3 focus-visible:ring-ring/50"
						aria-label="{m['tasks.transcript_title']()} · {row.role}"
					>
						<ChevronRight
							size={13}
							aria-hidden="true"
							class="shrink-0 transition-transform group-aria-expanded:rotate-90 motion-reduce:transition-none"
						/>
						<ScrollText size={13} aria-hidden="true" class="shrink-0" />
						<span>{m['tasks.transcript_title']()}</span>
						{#if status}<StepStatus {status} size={13} />{/if}
					</Collapsible.Trigger>
					<Collapsible.Content>
						{#if opened[row.key]}<div class="mt-1 ml-2 border-l py-2 pl-4 sm:pl-5">
								<AgentStepRun
									{chat}
									run={turnRunValue}
									active={turnRunValue.status === 'running'}
									pending={turnRunValue.status === 'running' || turnRunValue.status === 'queued'}
								/>
							</div>{/if}
					</Collapsible.Content>
				</Collapsible.Root>
			{/if}
		</Message.Content>
	</Message.Root>
{/snippet}

<ol aria-label={m['tasks.room']()} class="flex min-w-0 flex-col gap-4 py-2" data-testid="task-room">
	{#each rows as row (row.key)}
		<MessageScroller.Item messageId={row.key}>
			{#snippet child({ props })}
				<li {...props}>
					{#if row.type === 'owner'}{@render ownerPost(row.message)}{:else if row.type === 'kuayle'}{@render kuaylePost(
							row.message
						)}{:else if row.type === 'member'}{@render turn(row)}{/if}
				</li>
			{/snippet}
		</MessageScroller.Item>
	{/each}
</ol>
{#if !rows.length && !failed}<p class="text-sm text-muted-foreground" role="status">
		{loaded ? m['tasks.room_empty']() : m['assistant.loading']()}
	</p>{/if}
{#if failed}<div class="flex flex-wrap items-center gap-2">
		<p role="alert" class="text-sm text-destructive">{m['tasks.load_error']()}</p>
		<Button variant="ghost" size="sm" onclick={() => void load()}><RefreshCw />{m['tasks.refresh']()}</Button>
	</div>{/if}
