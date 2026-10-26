import { api } from './client';

export interface Conversation {
	id: string;
	workspace_id: string;
	creator_id: string;
	issue_id?: string | null;
	team_id?: string | null;
	title: string;
	created_at: string;
	updated_at: string;
}
export type MessageStatus = 'queued' | 'streaming' | 'completed' | 'failed' | 'interrupted' | 'cancelled';
export interface ConversationMessage {
	id: string;
	conversation_id: string;
	sequence: number;
	request_id: string;
	parent_id: string | null;
	role: 'user' | 'assistant';
	content: string;
	status: MessageStatus;
	provider: string;
	model: string;
	created_at: string;
	updated_at: string;
}
export interface ConversationEvent {
	id: string;
	cursor: number;
	conversation_id: string;
	type: 'message.created' | 'message.delta' | 'message.updated' | 'message.completed';
	payload: ConversationMessage | { message_id: string; delta?: string; status?: MessageStatus };
}
export interface ConversationIssue {
	id: string;
	identifier: string;
	title: string;
	description: string;
	status: string;
}
export interface ConversationHistory {
	issue?: ConversationIssue;
	conversation: Conversation;
	messages: ConversationMessage[];
	cursor: number;
	has_more: boolean;
}
export interface AssistantStatus {
	configured: boolean;
	provider?: string;
	model?: string;
}
const base = (slug: string) => `/api/workspaces/${encodeURIComponent(slug)}`;
export const conversations = {
	status: (slug: string) => api.get<AssistantStatus>(`${base(slug)}/assistant/status`),
	list: (slug: string) => api.get<Conversation[]>(`${base(slug)}/conversations`),
	openIssue: (slug: string, issue_id: string) => api.post<Conversation>(`${base(slug)}/conversations`, { issue_id }),
	create: (slug: string, title: string) => api.post<Conversation>(`${base(slug)}/conversations`, { title }),
	history: (slug: string, id: string, before = 0) =>
		api.get<ConversationHistory>(`${base(slug)}/conversations/${encodeURIComponent(id)}?before=${before}`),
	events: (slug: string, id: string, after: number) =>
		api.get<ConversationEvent[]>(`${base(slug)}/conversations/${encodeURIComponent(id)}/events?after=${after}`),
	send: (slug: string, id: string, request_id: string, content: string) =>
		api.post<ConversationMessage>(`${base(slug)}/conversations/${encodeURIComponent(id)}/messages`, {
			request_id,
			content
		}),
	retry: (slug: string, id: string, message: string, request_id: string) =>
		api.post<ConversationMessage>(
			`${base(slug)}/conversations/${encodeURIComponent(id)}/messages/${encodeURIComponent(message)}/retry`,
			{ request_id }
		),
	cancel: (slug: string, id: string) => api.post<void>(`${base(slug)}/conversations/${encodeURIComponent(id)}/cancel`),
	streamURL: (slug: string, id: string, after: number) =>
		`${location.protocol === 'https:' ? 'wss:' : 'ws:'}//${location.host}${base(slug)}/conversations/${encodeURIComponent(id)}/stream?after=${after}`
};
