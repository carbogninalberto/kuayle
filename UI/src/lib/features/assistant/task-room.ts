import type { AgentTask, TaskMessage } from '$lib/api/agent-tasks';

/** Only an explicit, contiguous list of alternatives becomes one-click replies. */
export function decisionOptions(body: string): string[] {
	const options = body
		.split('\n')
		.map((line) =>
			line
				.trim()
				.replace(/^[-*] /, '')
				.replaceAll('**', '')
		)
		.filter((line) => /^[A-C][).:]\s+\S/.test(line));
	return options.length >= 2 && options.every((line, index) => line[0] === 'ABC'[index]) ? options : [];
}

export function roomBlocker(task: AgentTask) {
	return task.pending_decisions?.find((d) => d.blocks_pause);
}

export const decisionAnchor = (taskId: string, kind: string, seq: number) => `decision-${taskId}-${kind}-${seq}`;

// Posts stay in sequence in the conversation. The activity lane groups facts
// and turns, including runs that have not posted yet.

/** The most posts one read of the room returns. */
export const ROOM_PAGE = 200;

export type RoomRow =
	| {
			type: 'owner' | 'kuayle';
			key: string;
			message: TaskMessage;
			/** A phase move, a waiver, a grant or the owner's approval. */
			decision: boolean;
	  }
	| { type: 'facts'; key: string; messages: TaskMessage[]; decision?: false }
	| {
			type: 'member';
			key: string;
			role: string;
			/** The run of the turn; null for a role's post outside a turn. */
			runId: string | null;
			/** When the turn started, where it stands before its first post. */
			startedAt?: string;
			/** Oldest first. */
			messages: TaskMessage[];
			/** The turn's verdict, the last the room took. */
			verdict?: 'approve' | 'request_changes' | 'comment';
			/** The turn asked a question. */
			asks: boolean;
			/** It gave a verdict, moved a phase or waived a check. */
			decision: boolean;
	  };

const taken = (message: TaskMessage) => !message.reject_reason;
const approval = (message: TaskMessage) => message.kind === 'fact' && message.data?.fact === 'approved' && message.data.value === true;
export const decides = (message: TaskMessage) =>
	taken(message) && (['phase', 'verdict', 'waiver', 'grant'].includes(message.kind) || approval(message));

// A fact Kuayle folds with the ones around it: not the owner's approval, and
// not one it addressed to the owner (a person must see it).
const folds = (message: TaskMessage) =>
	message.author_kind === 'kuayle' && message.kind === 'fact' && !approval(message) && !message.to_roles?.includes('human');

/** A turn of a role on the task, as its run says it: who, which run, when it started. */
export interface RoomTurn {
	id: string;
	role: string;
	at: string;
}

const rowTime = (row: RoomRow) =>
	Date.parse(
		row.type === 'member'
			? (row.messages[0]?.created_at ?? row.startedAt ?? '')
			: row.type === 'facts'
				? row.messages[0].created_at
				: row.message.created_at
	);

/**
 * The rows of a room, oldest first. A turn in `turns` that has no post in
 * the room is a row of its own, with no messages, before the first row that
 * came after it started; a turn that posted keeps the row its posts made.
 */
export function roomRows(messages: readonly TaskMessage[], turns: readonly RoomTurn[] = []): RoomRow[] {
	const rows: RoomRow[] = [];
	const stalls = new Set<string>();
	const named = new Map<string, Extract<RoomRow, { type: 'member' }>>();
	for (const message of [...messages].sort((a, b) => a.seq - b.seq)) {
		if (message.author_kind === 'kuayle' && message.kind === 'summary' && message.body.startsWith('Nobody is working, and the phase ')) {
			if (stalls.has(message.body)) continue;
			stalls.add(message.body);
		}
		if (message.author_kind !== 'member') {
			const last = rows.at(-1);
			if (!folds(message)) {
				const kuayle = message.author_kind !== 'human';
				rows.push({
					type: kuayle ? 'kuayle' : 'owner',
					key: `seq-${message.seq}`,
					message,
					decision: decides(message)
				});
			} else if (last?.type === 'facts') last.messages.push(message);
			else rows.push({ type: 'facts', key: `facts-${message.seq}`, messages: [message] });
			continue;
		}
		const runId = message.run_id;
		let turn = runId ? named.get(runId) : undefined;
		if (!turn) {
			turn = {
				type: 'member' as const,
				key: runId ? `run-${runId}` : `seq-${message.seq}`,
				role: message.author_role ?? '',
				runId,
				messages: [],
				asks: false,
				decision: false
			};
			if (runId) named.set(runId, turn);
			rows.push(turn);
		}
		turn.messages.push(message);
		if (taken(message) && message.kind === 'ask') turn.asks = true;
		if (taken(message) && message.kind === 'verdict' && message.data?.verdict) turn.verdict = message.data.verdict;
		if (decides(message)) turn.decision = true;
	}
	for (const turn of [...turns].sort((a, b) => Date.parse(a.at) - Date.parse(b.at))) {
		if (named.has(turn.id)) continue;
		const row: Extract<RoomRow, { type: 'member' }> = {
			type: 'member',
			key: `run-${turn.id}`,
			role: turn.role,
			runId: turn.id,
			startedAt: turn.at,
			messages: [],
			asks: false,
			decision: false
		};
		named.set(turn.id, row);
		const after = rows.findIndex((other) => rowTime(other) > Date.parse(turn.at));
		rows.splice(after < 0 ? rows.length : after, 0, row);
	}
	return rows;
}

/** The roles a row concerns: the role that wrote it, or the roles it was addressed to. */
export function rowRoles(row: RoomRow): string[] {
	if (row.type === 'member') return [row.role];
	const posts = row.type === 'facts' ? row.messages : [row.message];
	return [...new Set(posts.flatMap((message) => message.to_roles ?? []))];
}

/** What a fact of Kuayle's means, in a few words the view says. */
export type FactLabel =
	| {
			kind:
				| 'pr_opened'
				| 'pr_missing'
				| 'pr_draft'
				| 'pr_ready'
				| 'pr_open'
				| 'merged'
				| 'closed'
				| 'up_to_date'
				| 'behind'
				| 'conflict'
				| 'no_conflict'
				| 'collision'
				| 'no_collision'
				| 'ci_running'
				| 'ci_passed'
				| 'ci_failed'
				| 'ci_waiting'
				| 'ci_unknown'
				| 'approved'
				| 'not_approved'
				| 'no_overlap';
	  }
	| { kind: 'head'; commit: string }
	| { kind: 'base'; branch: string }
	| { kind: 'overlap'; issues: string }
	| { kind: 'script_passed' | 'script_failed'; check: string }
	| { kind: 'text'; text: string };

const yesNo: Record<string, [FactLabel['kind'], FactLabel['kind']]> = {
	pr_exists: ['pr_opened', 'pr_missing'],
	pr_draft: ['pr_draft', 'pr_ready'],
	merged: ['merged', 'pr_open'],
	closed: ['closed', 'pr_open'],
	head_contains_base: ['up_to_date', 'behind'],
	conflict: ['conflict', 'no_conflict'],
	migration_collision: ['collision', 'no_collision'],
	approved: ['approved', 'not_approved']
};
const ciLabel: Record<string, FactLabel['kind']> = {
	passing: 'ci_passed',
	failing: 'ci_failed',
	pending: 'ci_running',
	none: 'ci_waiting'
};

/** What a fact Kuayle posted says; one it has no words for keeps the first line of its text, or its name. */
export function factLabel(message: TaskMessage): FactLabel {
	const fact = message.data?.fact ?? '';
	const value = message.data?.value as unknown;
	const pair = yesNo[fact];
	if (pair && typeof value === 'boolean') return { kind: value ? pair[0] : pair[1] } as FactLabel;
	switch (fact) {
		case 'head_sha':
			if (typeof value === 'string') return { kind: 'head', commit: value.slice(0, 7) };
			break;
		case 'base_ref':
			if (typeof value === 'string') return { kind: 'base', branch: value };
			break;
		case 'ci_green': {
			const state = (value as { state?: string } | null)?.state ?? '';
			return { kind: ciLabel[state] ?? 'ci_unknown' } as FactLabel;
		}
		case 'overlap':
			if (Array.isArray(value)) return value.length ? { kind: 'overlap', issues: value.join(', ') } : { kind: 'no_overlap' };
			break;
		case 'script': {
			const run = (value ?? {}) as { check?: string; exit?: number };
			if (run.check && typeof run.exit === 'number')
				return { kind: run.exit === 0 ? 'script_passed' : 'script_failed', check: run.check };
			break;
		}
	}
	const text = message.body
		.split('\n')
		.map((line) => line.trim())
		.find(Boolean);
	return { kind: 'text', text: text || fact };
}

/**
 * A folded row as one line: each fact in turn, and where the same fact
 * changed several times in a row, where it started and where it ended
 * ("CI running, then CI passed"). The same words twice in a row are said once.
 */
export function foldText(
	messages: readonly TaskMessage[],
	say: (label: FactLabel) => string,
	then: (first: string, next: string) => string
): string {
	const parts: string[] = [];
	let i = 0;
	while (i < messages.length) {
		let j = i;
		while (j + 1 < messages.length && messages[j + 1].data?.fact === messages[i].data?.fact) j++;
		const first = say(factLabel(messages[i]));
		const last = say(factLabel(messages[j]));
		const part = first === last ? first : then(first, last);
		if (parts.at(-1) !== part) parts.push(part);
		i = j + 1;
	}
	return parts.join(' · ');
}

/**
 * The room with incoming posts added once each (a post never changes, so
 * the copy held is kept), oldest first. Nothing new returns the same room.
 */
export function mergeRoom(known: readonly TaskMessage[] | undefined, incoming: readonly TaskMessage[]): TaskMessage[] {
	const held = new Set(known?.map((message) => message.seq));
	const fresh: TaskMessage[] = [];
	for (const message of incoming) {
		if (held.has(message.seq)) continue;
		held.add(message.seq);
		fresh.push(message);
	}
	if (!fresh.length && known) return known as TaskMessage[];
	return [...(known ?? []), ...fresh].sort((a, b) => a.seq - b.seq);
}

/**
 * The seq a read of the room (oldest first) continues after: the last of
 * the unbroken run of posts from the first. A post missed in between (a
 * lost event) is read again with everything after it.
 */
export function roomRead(messages: readonly TaskMessage[]): number {
	let seq = 0;
	for (const message of messages) {
		if (message.seq !== seq + 1) break;
		seq = message.seq;
	}
	return seq;
}

const splitReceipt = (post: TaskMessage) => post.author_kind === 'kuayle' && post.kind === 'fact' &&
	['split_approved', 'split_refused'].includes(post.data.fact ?? '');

/** Accepted lead proposals stay actionable until the platform settles the exact request. */
export function roomSplits(messages: readonly TaskMessage[]) {
	const splits = new Map<number, { issues: NonNullable<TaskMessage['data']['issues']>; pending: boolean }>();
	for (const post of messages) {
		if (post.reject_reason) continue;
		if (
			post.author_kind === 'member' && post.author_role === 'lead' &&
			post.kind === 'request' && post.data.action === 'split' && post.data.issues?.length
		) {
			splits.set(post.seq, { issues: post.data.issues, pending: true });
		}
		if (splitReceipt(post) && post.reply_to != null) {
			const proposal = splits.get(post.reply_to);
			if (proposal) proposal.pending = false;
		}
	}
	return splits;
}

/** Split requests require a platform receipt. An owner can answer other posts in the room. */
export function openRoomBlocker(messages: readonly TaskMessage[], decisions?: AgentTask['pending_decisions']): TaskMessage | undefined {
	if (decisions?.some((item) => item.blocks_pause)) return;
	const pending = new Map<number, TaskMessage>();
	const verdicts = new Map<string, number>();
	const addressedByOwner = new Map<string, number>();
	let lastBroadcast = 0;
	for (const post of messages) {
		if (post.reject_reason) continue;
		if (post.author_kind === 'human' && post.kind === 'say' && post.reply_to == null) {
			if (post.to_roles?.length) {
				for (const role of post.to_roles) addressedByOwner.set(role, post.seq);
			} else lastBroadcast = post.seq;
		}
		const parent = post.reply_to == null ? undefined : pending.get(post.reply_to);
		if (parent) {
			const split = parent.kind === 'request' && parent.data.action === 'split';
			const addressedReply = post.author_kind === 'human' || (post.author_role && post.author_role !== parent.author_role &&
				(!parent.to_roles?.length || parent.to_roles.includes(post.author_role)));
			if (split ? splitReceipt(post) : addressedReply) pending.delete(parent.seq);
		}
		if (post.author_kind !== 'member') continue;
		if (post.kind === 'verdict' && post.author_role) verdicts.set(post.author_role, post.seq);
		if (post.kind === 'ask' || (post.kind === 'say' && post.to_roles?.includes('human')) ||
			(post.kind === 'request' && post.data.action !== 'approve')) pending.set(post.seq, post);
	}
	// The task API tracks owner requests, including declines and delegated splits.
	const open = [...pending.values()].filter((post) => {
		if (decisions == null || post.kind === 'say') return true;
		return post.data.action !== 'split' && !post.to_roles?.includes('human');
	});
	return open.findLast((post) => post.data.action === 'split') ?? open.findLast((post) => {
		if (lastBroadcast > post.seq || (addressedByOwner.get(post.author_role ?? '') ?? 0) > post.seq) return false;
		if (post.data.action !== 'review') return true;
		return post.to_roles?.length
			? post.to_roles.some((role) => (verdicts.get(role) ?? 0) < post.seq)
			: ![...verdicts.values()].some((seq) => seq > post.seq);
	});
}

/** Catch up from a contiguous cursor, merging each page with any live posts. */
export async function readRoomPages(
	after: number,
	read: (after: number) => Promise<TaskMessage[]>,
	accept: (messages: TaskMessage[]) => void
) {
	for (;;) {
		const messages = await read(after);
		accept(messages);
		if (messages.length < ROOM_PAGE) return;
		const next = messages[messages.length - 1].seq;
		if (next <= after) throw new Error('Room cursor did not advance');
		after = next;
	}
}
