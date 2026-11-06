// Pure rules behind the assistant's three-column shell. They take plain data
// (no Svelte state, no API calls) so they can be unit tested directly.

export type TaskStateName =
	| 'queued'
	| 'preparing'
	| 'running'
	| 'waiting_input'
	| 'validating'
	| 'publishing'
	| 'awaiting_review'
	| 'blocked'
	| 'completed'
	| 'failed'
	| 'cancelling'
	| 'cancelled';

const activeStates = ['queued', 'preparing', 'running', 'validating', 'publishing', 'cancelling'];
const waitingStates = ['waiting_input', 'awaiting_review', 'blocked'];
// Only work that ended well, or was deliberately stopped, rolls up into the
// "finished" row. A failure stays listed until the person deals with it.
const finishedStates = ['completed', 'cancelled'];

export type TaskIndicator = 'running' | 'waiting' | 'failed';

export function taskIndicator(state: string): TaskIndicator | undefined {
	if (activeStates.includes(state)) return 'running';
	if (waitingStates.includes(state)) return 'waiting';
	if (state === 'failed') return 'failed';
	return undefined;
}

export function isFinishedTask(state: string) {
	return finishedStates.includes(state);
}

/** Tasks the composer can address: the agent is still there to read it. */
export function isMessageableTask(state: string) {
	return !['completed', 'cancelled', 'cancelling'].includes(state);
}

type Listed = { id: string; state: string; updated_at: string };

/**
 * Splits the background task list into what stays visible (running, waiting
 * on the person, failed) and what rolls into the "N finished" row. Live states
 * from the open conversation override the polled queue.
 */
export function splitBackgroundTasks<T extends Listed>(items: T[], live: Record<string, { state: string }> = {}) {
	const current = items.map((item) => (live[item.id] ? { ...item, state: live[item.id].state } : item));
	const rank = (item: T) => {
		const indicator = taskIndicator(item.state);
		return indicator === 'failed' ? 0 : indicator === 'waiting' ? 1 : indicator === 'running' ? 2 : 3;
	};
	const open = current
		.filter((item) => !isFinishedTask(item.state))
		.sort((a, b) => rank(a) - rank(b) || Date.parse(b.updated_at) - Date.parse(a.updated_at));
	const finished = current.filter((item) => isFinishedTask(item.state));
	return { open, finished };
}

/** The strongest indicator among a conversation's tasks, for its sidebar row. */
export function conversationIndicator(
	conversationId: string,
	items: { conversation_id: string; state: string }[]
): TaskIndicator | undefined {
	const found = items.filter((item) => item.conversation_id === conversationId).map((i) => taskIndicator(i.state));
	if (found.includes('failed')) return 'failed';
	if (found.includes('waiting')) return 'waiting';
	if (found.includes('running')) return 'running';
	return undefined;
}

/**
 * Collapsing earlier history keeps the latest `keep` messages and reports how
 * many earlier ones are folded away behind "Show N earlier messages".
 */
export function visibleHistory<T>(messages: T[], collapsed: boolean, keep = 6) {
	if (!collapsed || messages.length <= keep) return { shown: messages, hidden: 0 };
	return { shown: messages.slice(-keep), hidden: messages.length - keep };
}

/**
 * The model context includes up to the latest `limit` completed messages
 * (see BE/docs/conversations.md), so usage is how much of that window this
 * conversation already fills.
 */
export function contextUsage(messages: { status: string }[], hasMore: boolean, limit = 50) {
	const completed = messages.filter((message) => message.status === 'completed').length;
	const used = hasMore ? limit : Math.min(completed, limit);
	return { used, limit, percent: Math.round((used / limit) * 100) };
}

/**
 * Interleaves timed items (agent messages, tasks) with the conversation's
 * messages: each item is placed before the first message created after it.
 */
export function placeByTime<M extends { id: string; created_at: string }, I extends { created_at: string }>(
	messages: M[],
	items: I[]
) {
	const sorted = [...items].sort((a, b) => Date.parse(a.created_at) - Date.parse(b.created_at));
	const before: Record<string, I[]> = {};
	let next = 0;
	for (const message of messages) {
		const at = Date.parse(message.created_at);
		const group: I[] = [];
		while (next < sorted.length && Date.parse(sorted[next].created_at) < at) group.push(sorted[next++]);
		if (group.length) before[message.id] = group;
	}
	return { before, after: sorted.slice(next) };
}

// A conversation row dragged onto a folder carries its id under this type,
// so links, files or text dragged from elsewhere are never treated as a move.
export const CONVERSATION_DRAG_TYPE = 'application/x-kuayle-conversation';

export function isConversationDrag(types: readonly string[] | DOMStringList | undefined | null) {
	return !!types && Array.from(types as ArrayLike<string>).includes(CONVERSATION_DRAG_TYPE);
}

/** The move a drop asks for, or null when it changes nothing. */
export function dropMove(
	conversation: { id: string; folder_id?: string | null } | undefined,
	target: string | null
): { id: string; folder: string | null } | null {
	if (!conversation) return null;
	if ((conversation.folder_id ?? null) === target) return null;
	return { id: conversation.id, folder: target };
}

/** "web_search" → "Web search": a readable label for a tool nobody described. */
export function humanizeToolName(name: string) {
	const words = name.replace(/[_-]+/g, ' ').trim();
	return words ? words.charAt(0).toUpperCase() + words.slice(1) : name;
}

/** Milliseconds a finished call took, when both ends were recorded. */
export function elapsed(from: string, to?: string | null) {
	if (!to) return undefined;
	const ms = Date.parse(to) - Date.parse(from);
	return Number.isFinite(ms) && ms >= 0 ? ms : undefined;
}

export function formatDuration(ms: number) {
	if (ms < 1000) return `${Math.max(0, Math.round(ms))}ms`;
	const seconds = Math.round(ms / 1000);
	if (seconds < 60) return `${seconds}s`;
	const minutes = Math.floor(seconds / 60);
	if (minutes < 60) return `${minutes}m ${seconds % 60}s`;
	const hours = Math.floor(minutes / 60);
	return `${hours}h ${minutes % 60}m`;
}

/** Splits step chips into the ones shown inline and the overflow behind "+N". */
export function splitSteps<T>(steps: T[], inline = 3) {
	if (steps.length <= inline + 1) return { inline: steps, overflow: [] as T[] };
	return { inline: steps.slice(0, inline), overflow: steps.slice(inline) };
}

// Why an iteration of a coding task started, from recorded data only: the
// task's state transitions (each new iteration is a transition to "queued"
// out of the state the previous one ended in) and the GitHub signals that
// queued a follow-up iteration (follow_up_generation).
export type IterationReason =
	| 'first'
	| 'review_changes'
	| 'ci_failed'
	| 'follow_up'
	| 'retry_failed'
	| 'retry_blocked'
	| 'restarted';

export function iterationReasons(
	transitions: { sequence: number; from_state: string; to_state: string }[],
	signals: { kind: string; action: string; follow_up_generation?: number | null }[]
): Record<number, IterationReason> {
	const reasons: Record<number, IterationReason> = { 1: 'first' };
	// The first "queued" is the task's creation; each later one starts the next iteration.
	const queued = [...transitions].sort((a, b) => a.sequence - b.sequence).filter((t) => t.to_state === 'queued');
	queued.slice(1).forEach((transition, index) => {
		const generation = index + 2;
		reasons[generation] =
			transition.from_state === 'failed'
				? 'retry_failed'
				: transition.from_state === 'blocked'
					? 'retry_blocked'
					: transition.from_state === 'awaiting_review'
						? 'follow_up'
						: 'restarted';
	});
	// A GitHub review, comment or CI result that queued the iteration names it best.
	for (const signal of signals) {
		const generation = signal.follow_up_generation;
		if (signal.action !== 'follow_up' || !generation || generation < 2) continue;
		reasons[generation] = signal.kind === 'ci' ? 'ci_failed' : 'review_changes';
	}
	return reasons;
}
