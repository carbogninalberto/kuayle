import type { AgentLiveMessage, AgentTask, LiveSendMode } from '$lib/api/agent-tasks';
import type { SessionBlock } from './agent-transcript';

// The conversation with a task's agent, placed in its timeline: what the
// task's creator (or the assistant for them) told the agent sits in the run
// that received it, at the moment it was delivered, and the agent's reply
// (the message that names it, reply_to) is the next item. A message that
// waits is shown where it will be picked up: in the run that is going, or
// after the last step when none is. The server decides who may read or
// send; this only shapes what it returned.

/** Where a message to the agent stands; an agent's own message is received or refused. */
export type MessageState = 'queued' | 'delivered' | 'replied' | 'unanswered' | 'received' | 'rejected';

export interface PlacedMessage {
	message: AgentLiveMessage;
	state: MessageState;
	/** When it entered the run: its delivery, or when it was written. */
	at: string;
	/** The agent's replies to it, oldest first. */
	replies: AgentLiveMessage[];
}

export interface Conversation {
	/** Run id → its messages, oldest first. */
	byRun: Map<string, PlacedMessage[]>;
	/** Messages no listed run holds: waiting with no step running, or of a run that is not listed. */
	pending: PlacedMessage[];
	/** Runs that ended owing a reply. */
	unanswered: Set<string>;
}

type Task = Pick<AgentTask, 'state' | 'run_id'>;
type Run = { id: string; status?: string; replies_unknown?: boolean };
type Step = { current: boolean; status: string; runs: Run[] };

const ended = ['completed', 'cancelled', 'failed'];
// A run that ran to its end. A cancelled one was interrupted or paused: the
// attempt that continues it takes its messages over.
const finished = ['succeeded', 'failed', 'timeout'];

function stateOf(message: AgentLiveMessage, replied: boolean, task: Task, run: Run | undefined): MessageState {
	if (message.direction === 'from_agent') return message.state === 'rejected' ? 'rejected' : 'received';
	if (message.state === 'queued') return 'queued';
	if (replied) return 'replied';
	// Delivered before replies were required: nothing was owed. And when the
	// agent's last messages could not be collected a reply may exist unseen:
	// it is not called missing.
	if (!message.deliveries || run?.replies_unknown) return 'delivered';
	// The attempt that received it can still answer while it has not run to
	// its end; a missing reply never fails it, so its end is what counts.
	const current = message.run_id === task.run_id && !ended.includes(task.state);
	return current && !finished.includes(run?.status ?? '') ? 'delivered' : 'unanswered';
}

/** Places a task's live messages in the runs of its timeline. */
export function placeConversation(messages: AgentLiveMessage[], steps: Step[], task: Task): Conversation {
	const listed = new Map(steps.flatMap((step) => step.runs.map((run) => [run.id, run] as const)));
	// The run that is going takes what still waits.
	const going = steps.find((step) => step.current && step.status === 'running')?.runs.at(-1)?.id;
	const sent = new Map<number, AgentLiveMessage>();
	for (const message of messages)
		if (message.direction === 'to_agent' && message.seq != null) sent.set(message.seq, message);
	const replies = new Map<string, AgentLiveMessage[]>();
	const own: AgentLiveMessage[] = [];
	for (const message of messages) {
		const target =
			message.direction === 'from_agent' && message.state === 'received' && message.reply_to != null
				? sent.get(message.reply_to)
				: undefined;
		// A reply to a message that is still listed reads right under it.
		if (target && target.state === 'delivered') replies.set(target.id, [...(replies.get(target.id) ?? []), message]);
		else own.push(message);
	}
	const conversation: Conversation = { byRun: new Map(), pending: [], unanswered: new Set() };
	for (const message of own) {
		const answers = replies.get(message.id) ?? [];
		const placed: PlacedMessage = {
			message,
			state: stateOf(message, answers.length > 0, task, message.run_id ? listed.get(message.run_id) : undefined),
			at: (message.direction === 'to_agent' && message.delivered_at) || message.created_at,
			replies: answers
		};
		const run = placed.state === 'queued' ? going : (message.run_id ?? undefined);
		if (run && listed.has(run)) {
			conversation.byRun.set(run, [...(conversation.byRun.get(run) ?? []), placed]);
			if (placed.state === 'unanswered') conversation.unanswered.add(run);
		} else conversation.pending.push(placed);
	}
	const byTime = (a: PlacedMessage, b: PlacedMessage) => Date.parse(a.at) - Date.parse(b.at);
	for (const list of conversation.byRun.values()) list.sort(byTime);
	conversation.pending.sort(byTime);
	return conversation;
}

export type ConversationItem =
	{ kind: 'block'; key: string; block: SessionBlock } | { kind: 'message'; key: string; placed: PlacedMessage };

// When a block of the session happened, if its output says.
function blockTime(block: SessionBlock): number {
	const at =
		block.kind === 'text' || block.kind === 'user'
			? block.at
			: block.kind === 'tool' || block.kind === 'subagent'
				? block.tool.started
				: block.kind === 'steps'
					? block.tools[0]?.started
					: undefined;
	return at ? Date.parse(at) : NaN;
}

/**
 * A run's session with its messages in place: each before the first thing
 * the agent did after it arrived, and at the end when nothing followed.
 */
export function interleave(blocks: SessionBlock[], messages: PlacedMessage[]): ConversationItem[] {
	const items: ConversationItem[] = [];
	let next = 0;
	const push = () => {
		const placed = messages[next++];
		items.push({ kind: 'message', key: `message-${placed.message.id}`, placed });
	};
	for (const block of blocks) {
		const time = blockTime(block);
		while (next < messages.length && Date.parse(messages[next].at) < time) push();
		items.push({ kind: 'block', key: block.key, block });
	}
	while (next < messages.length) push();
	return items;
}

/** Context key: how a message in the timeline asks to be answered, when the viewer may write to the agent. */
export const answerContext = Symbol('agent-answer');
export type AnswerContext = () => ((question: AgentLiveMessage) => void) | undefined;

export interface SendOptions {
	/** The viewer may write to this task's agent. */
	open: boolean;
	/** What "now" means for this task, if anything: interrupt the working agent, or start one. */
	now: LiveSendMode | null;
	/**
	 * Starting now gives something up, so the person is asked once: the task
	 * is waiting (CI, an approval, a merge, a review) and a new iteration
	 * abandons that wait and counts as a visit and an iteration.
	 */
	confirm: boolean;
}

type Sendable = Pick<AgentTask, 'kind' | 'state' | 'creator_id' | 'provider'> & {
	usage_limit_message?: string | null;
	workflow_paused_at?: string | null;
	interrupting?: boolean;
};

/**
 * What the composer offers for a task. Only its creator writes to its agent,
 * until it is completed or cancelled; an agent that is only being stopped by
 * a pause or an interruption still takes messages, for when it continues. A
 * working agent can be interrupted, with or without a workflow (Claude Code
 * reads a plain message between its steps; the other harnesses are
 * interrupted by any message, so they need no second action); a task with no
 * agent working can be started for the message.
 */
export function sendOptions(task: Sendable, viewer: string | undefined): SendOptions {
	const open =
		task.kind !== 'review' &&
		!!viewer &&
		task.creator_id === viewer &&
		!['completed', 'cancelled'].includes(task.state) &&
		(task.state !== 'cancelling' || !!task.workflow_paused_at || !!task.interrupting);
	if (!open) return { open, now: null, confirm: false };
	if (task.state === 'running' || task.state === 'waiting_input')
		return { open, now: task.provider === 'claude-code' ? 'interrupt' : null, confirm: false };
	const startable =
		task.state === 'failed' || task.state === 'awaiting_review' || (task.state === 'blocked' && !task.usage_limit_message);
	return { open, now: startable ? 'start' : null, confirm: startable && task.state === 'awaiting_review' };
}

/** A message that answers an agent's question: the question quoted, then the answer. */
export function answerTo(question: string, answer: string) {
	const quoted = question.replace(/\s+/g, ' ').trim();
	const short = [...quoted].length > 280 ? [...quoted].slice(0, 279).join('') + '…' : quoted;
	return `In answer to your question "${short}":\n${answer}`;
}
