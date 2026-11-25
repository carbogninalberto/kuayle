import type { TaskMessage } from '$lib/api/agent-tasks';

// The room of a task on a pipeline (KUA-252) as the rows of its agent view,
// in the room's order (seq), one line per event (KUA-286). What the owner
// posts is a row each. Kuayle's facts in a row fold into one row; its phase
// moves, the owner's approval and what it tells the owner stand alone. A
// role's turn is one run of its agent: everything the turn posted is one
// row, where its first post is, and opens the run's transcript. A turn that
// has not posted yet is a row too, from its run, where it started (KUA-321),
// so a running turn is never missing. The server decides who may read the room and orders it; this only shapes what it
// returned.

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
const decides = (message: TaskMessage) =>
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
	const named = new Map<string, Extract<RoomRow, { type: 'member' }>>();
	for (const message of [...messages].sort((a, b) => a.seq - b.seq)) {
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

/**
 * Whether a row starts open: a role's turn that holds a split the owner has
 * not approved yet (roomSplits), so the proposal and its approval are in view
 * in the folded thread (KUA-309), or one still running (`live`, the ids of
 * the runs that have not ended), so its stream is in view (KUA-321). Every
 * other turn starts folded, and a running one folds when it ends.
 */
export function openByDefault(
	row: RoomRow,
	splits: ReturnType<typeof roomSplits>,
	live: ReadonlySet<string> = new Set()
): boolean {
	return (
		row.type === 'member' &&
		((!!row.runId && live.has(row.runId)) ||
			row.messages.some((message) => {
				const split = splits.get(message.seq);
				return !!split && !split.approved;
			}))
	);
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

/** Accepted lead proposals in room order. Only Kuayle's receipt confirms the request it replies to. */
export function roomSplits(messages: readonly TaskMessage[]) {
	const splits = new Map<number, { issues: NonNullable<TaskMessage['data']['issues']>; approved: boolean }>();
	for (const post of messages) {
		if (post.reject_reason) continue;
		if (
			post.author_kind === 'member' && post.author_role === 'lead' &&
			post.kind === 'request' && post.data.action === 'split' && post.data.issues?.length
		) {
			splits.set(post.seq, { issues: post.data.issues, approved: false });
		}
		if (
			post.author_kind === 'kuayle' && post.kind === 'fact' &&
			post.data.fact === 'split_approved' && post.reply_to != null
		) {
			const proposal = splits.get(post.reply_to);
			if (proposal) proposal.approved = true;
		}
	}
	return splits;
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
