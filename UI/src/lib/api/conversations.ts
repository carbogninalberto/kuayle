import type { AgentTask, TaskActivity } from './agent-tasks';
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
	folder_id?: string | null;
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
	type:
		| 'message.created'
		| 'message.delta'
		| 'message.updated'
		| 'message.completed'
		| 'task.state_changed'
		| 'task.updated'
		| 'task.activity'
		| 'task.tests_updated'
		| 'task.publication_updated'
		| 'task.signal_recorded'
		| 'tool.started'
		| 'tool.completed'
		| 'approval.requested'
		| 'approval.updated'
		| 'approval.resolved';
	payload:
		| ConversationApproval
		| ConversationToolCall
		| AgentTask
		| TaskActivity
		| ConversationMessage
		| { message_id: string; delta?: string; status?: MessageStatus };
}
export interface ConversationIssue {
	id: string;
	identifier: string;
	title: string;
	description: string;
	status: string;
}
export interface ConversationToolCall {
	id: string;
	conversation_id: string;
	message_id: string;
	sequence: number;
	call_id: string;
	name: string;
	input: Record<string, unknown>;
	status: 'running' | 'completed' | 'failed';
	output?: unknown;
	error?: string;
	issue_id?: string;
	created_at: string;
	completed_at?: string;
}
export interface ConversationApproval {
	id: string;
	conversation_id: string;
	tool_call_id: string;
	action: 'delegate_issue';
	actor_id: string;
	issue_id: string;
	payload: { identifier: string; title: string; instructions: string };
	status: 'pending' | 'approving' | 'approved' | 'denied' | 'expired' | 'failed';
	task_id?: string;
	error?: string;
	expires_at: string;
	resolved_at?: string;
	created_at: string;
	updated_at: string;
}
export interface ConversationHistory {
	issue?: ConversationIssue;
	conversation: Conversation;
	messages: ConversationMessage[];
	tools?: ConversationToolCall[];
	approvals?: ConversationApproval[];
	cursor: number;
	has_more: boolean;
}
export interface AssistantStatus {
	configured: boolean;
	provider?: string;
	model?: string;
}
const base = (slug: string) => `/api/workspaces/${encodeURIComponent(slug)}`;
export interface ConversationFolder {
	id: string;
	name: string;
}
export const conversations = {
	folders: (slug: string) => api.get<ConversationFolder[]>(`${base(slug)}/conversation-folders`),
	createFolder: (slug: string, name: string) =>
		api.post<ConversationFolder>(`${base(slug)}/conversation-folders`, { name }),
	renameFolder: (slug: string, id: string, name: string) =>
		api.patch<ConversationFolder>(`${base(slug)}/conversation-folders/${encodeURIComponent(id)}`, { name }),
	deleteFolder: (slug: string, id: string) =>
		api.delete<void>(`${base(slug)}/conversation-folders/${encodeURIComponent(id)}`),
	move: (slug: string, id: string, folder_id: string | null) =>
		api.patch<Conversation>(`${base(slug)}/conversations/${encodeURIComponent(id)}`, { folder_id }),
	remove: (slug: string, id: string) => api.delete<void>(`${base(slug)}/conversations/${encodeURIComponent(id)}`),
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
	resolveApproval: (slug: string, id: string, approval: string, decision: 'approve' | 'deny') =>
		api.post<ConversationApproval>(
			`${base(slug)}/conversations/${encodeURIComponent(id)}/approvals/${encodeURIComponent(approval)}`,
			{ decision }
		),
	retry: (slug: string, id: string, message: string, request_id: string) =>
		api.post<ConversationMessage>(
			`${base(slug)}/conversations/${encodeURIComponent(id)}/messages/${encodeURIComponent(message)}/retry`,
			{ request_id }
		),
	cancel: (slug: string, id: string) => api.post<void>(`${base(slug)}/conversations/${encodeURIComponent(id)}/cancel`),
	streamURL: (slug: string, id: string, after: number) =>
		`${location.protocol === 'https:' ? 'wss:' : 'ws:'}//${location.host}${base(slug)}/conversations/${encodeURIComponent(id)}/stream?after=${after}`
};
