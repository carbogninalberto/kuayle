import type { TaskMessage } from '$lib/api/agent-tasks';

// The room of a task on a pipeline (KUA-252) as the rows of its agent view,
// in the room's order (seq). What the owner posts and what Kuayle posts
// (facts, phase moves, summaries) is a row each. A role's turn is one run of
// its agent: everything the turn posted is one row, where its first post is,
// and opens the run's transcript. The server decides who may read the room
// and orders it; this only shapes what it returned.

/** The most posts one read of the room returns. */
export const ROOM_PAGE = 200;

export type RoomRow =
	| { type: 'owner' | 'kuayle'; key: string; message: TaskMessage }
	| {
			type: 'member';
			key: string;
			role: string;
			/** The run of the turn; null for a role's post outside a turn. */
			runId: string | null;
			/** Oldest first. */
			messages: TaskMessage[];
	  };

/** The rows of a room, oldest first. */
export function roomRows(messages: readonly TaskMessage[]): RoomRow[] {
	const rows: RoomRow[] = [];
	const turns = new Map<string, Extract<RoomRow, { type: 'member' }>>();
	for (const message of [...messages].sort((a, b) => a.seq - b.seq)) {
		if (message.author_kind !== 'member') {
			rows.push({ type: message.author_kind === 'human' ? 'owner' : 'kuayle', key: `seq-${message.seq}`, message });
			continue;
		}
		const runId = message.run_id;
		const turn = runId ? turns.get(runId) : undefined;
		if (turn) {
			turn.messages.push(message);
			continue;
		}
		const row = {
			type: 'member' as const,
			key: runId ? `run-${runId}` : `seq-${message.seq}`,
			role: message.author_role ?? '',
			runId,
			messages: [message]
		};
		if (runId) turns.set(runId, row);
		rows.push(row);
	}
	return rows;
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
