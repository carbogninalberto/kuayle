import { api } from './client';
import { messageQuery, type MessageFilters } from '$lib/features/agent-messages/agent-messages';

export type AgentMessageState = 'queued' | 'held' | 'delivered' | 'dropped' | 'rejected';
export type AgentMessageChannel = 'internal' | 'comment';

// One record of the agent messaging registry ("navigator"). The body is text
// an agent wrote: render it escaped, never as instructions or markup.
export interface AgentMessage {
	id: string;
	from_task_id: string;
	from_issue_id: string;
	from_identifier: string;
	from_title: string;
	to_ref: string;
	to_issue_id: string | null;
	to_task_id: string | null;
	to_identifier: string | null;
	to_title: string | null;
	channel: AgentMessageChannel;
	body: string;
	state: AgentMessageState;
	reject_reason: string | null;
	held_by_name: string | null;
	held_at: string | null;
	released_by_name: string | null;
	released_at: string | null;
	dropped_by_name: string | null;
	dropped_at: string | null;
	delivered_at: string | null;
	delivered_run_id: string | null;
	comment_state: 'none' | 'pending' | 'posted' | 'failed';
	comment_id: string | null;
	conversation_hold_id: string | null;
	can_moderate: boolean;
	created_at: string;
}

export interface AgentMessageAction {
	id: number;
	action: 'hold' | 'release' | 'drop';
	actor_name: string | null;
	created_at: string;
}

export interface AgentMessageHold {
	id: string;
	issue_a_id: string;
	issue_b_id: string;
	issue_a: string;
	issue_b: string;
	created_by_name: string | null;
	held_messages: number;
	can_moderate: boolean;
	created_at: string;
}

const base = (slug: string) => `/api/workspaces/${encodeURIComponent(slug)}`;
const message = (slug: string, id: string) => `${base(slug)}/agent-messages/${encodeURIComponent(id)}`;

export const agentMessages = {
	list: (slug: string, filters: MessageFilters = {}) =>
		api.get<{ messages: AgentMessage[]; next_before?: string }>(`${base(slug)}/agent-messages${messageQuery(filters)}`),
	get: (slug: string, id: string) =>
		api.get<{ message: AgentMessage; actions: AgentMessageAction[] }>(message(slug, id)),
	act: (slug: string, id: string, action: 'hold' | 'release' | 'drop') =>
		api.post<AgentMessage>(`${message(slug, id)}/${action}`),
	holds: (slug: string) => api.get<{ holds: AgentMessageHold[] }>(`${base(slug)}/agent-message-holds`),
	holdConversation: (slug: string, issueIds: [string, string]) =>
		api.post<AgentMessageHold>(`${base(slug)}/agent-message-holds`, { issue_ids: issueIds }),
	releaseConversation: (slug: string, id: string) =>
		api.delete<void>(`${base(slug)}/agent-message-holds/${encodeURIComponent(id)}`)
};
