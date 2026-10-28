import type { AgentMessage, AgentMessageState } from '$lib/api/agent-messages';

export interface MessageFilters {
	task_id?: string;
	issue_id?: string;
	issue?: string;
	state?: AgentMessageState | '';
	channel?: 'internal' | 'comment' | '';
	before?: string;
}

export type MessageAction = 'hold' | 'release' | 'drop' | 'hold_conversation' | 'release_conversation';

// The controls a moderator gets for a message in its current state. Holding
// applies only before delivery; a conversation needs a resolved recipient.
export function messageActions(
	message: Pick<AgentMessage, 'state' | 'can_moderate' | 'to_issue_id' | 'conversation_hold_id'>
): MessageAction[] {
	if (!message.can_moderate) return [];
	const actions: MessageAction[] = [];
	if (message.state === 'queued') actions.push('hold');
	if (message.state === 'held') actions.push('release');
	if (message.state === 'queued' || message.state === 'held') actions.push('drop');
	if (message.to_issue_id) actions.push(message.conversation_hold_id ? 'release_conversation' : 'hold_conversation');
	return actions;
}

export type MessageTone = 'success' | 'running' | 'warning' | 'error' | 'neutral';

export function messageTone(state: AgentMessageState): MessageTone {
	switch (state) {
		case 'delivered':
			return 'success';
		case 'queued':
			return 'running';
		case 'held':
			return 'warning';
		case 'rejected':
			return 'error';
		default:
			return 'neutral';
	}
}

export function summarize(messages: Pick<AgentMessage, 'state'>[]): Partial<Record<AgentMessageState, number>> {
	const counts: Partial<Record<AgentMessageState, number>> = {};
	for (const message of messages) counts[message.state] = (counts[message.state] ?? 0) + 1;
	return counts;
}

export function messageQuery(filters: MessageFilters): string {
	const params: string[] = [];
	for (const [key, raw] of Object.entries(filters)) {
		let value = typeof raw === 'string' ? raw.trim() : '';
		if (key === 'issue') value = value.toUpperCase();
		if (value) params.push(`${key}=${encodeURIComponent(value)}`);
	}
	return params.length ? `?${params.join('&')}` : '';
}
