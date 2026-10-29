import type { AgentLiveChatMessage, AgentTask, TaskActivity } from './agent-tasks';
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
	// The effective project (an issue chat's is its issue's), only when the
	// owner can see it. Projects tag and filter; they never share a chat.
	project_id?: string | null;
	project?: ConversationProject | null;
}
export interface ConversationProject {
	id: string;
	name: string;
	team_id: string | null;
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
	attachments?: ConversationAttachment[];
}
// An image on a user message; served only to the conversation's readers.
export interface ConversationAttachment {
	id: string;
	conversation_id: string;
	message_id?: string;
	filename: string;
	content_type: string;
	size: number;
	width: number;
	height: number;
	position: number;
	created_at: string;
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
		| 'task.review_updated'
		| 'task.live_message'
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
		| AgentLiveChatMessage
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
export interface DelegationPayload {
	identifier: string;
	title: string;
	instructions: string;
	/** The development repository resolved when approval was requested. */
	repository?: string;
	base_branch?: string;
	repository_source?: 'issue' | 'project' | 'team' | 'workspace';
}
// A merge approval is bound to the reviewed commit shown to the user.
export interface MergePayload {
	identifier: string;
	title: string;
	task_id: string;
	repository: string;
	pull_request: number;
	pull_request_url: string;
	commit: string;
}
// An acceptance approval is bound to the reviewed attempt and pull request;
// accepting completes the task in Kuayle and never merges.
export interface AcceptPayload {
	identifier: string;
	title: string;
	task_id: string;
	generation: number;
	pull_request: number;
	pull_request_url: string;
	commit: string;
}
export interface ReviewPayload {
	identifier: string;
	title: string;
	task_id: string;
	harness: string;
	focus?: string;
	post_to_pull_request: boolean;
	pull_request: number;
	pull_request_url: string;
	commit: string;
}
export interface FollowUpPayload {
	identifier: string;
	title: string;
	task_id: string;
	generation: number;
	instructions: string;
}
// Who can open an app port link: only the machine's owner, signed in.
export type AppPortAudience = 'owner';
export interface PreviewPayload {
	identifier: string;
	title: string;
	task_id: string;
	machine_id: string;
	machine: string;
	command: string[];
	// The preferred port; the machine picks a free one when it is taken.
	port?: number;
	audience: AppPortAudience;
}
export interface ExposePortPayload {
	identifier: string;
	title: string;
	task_id: string;
	machine_id: string;
	machine: string;
	port: number;
	label?: string;
	host: string;
	// The machine already exposed the port when approval was requested.
	existing: boolean;
	audience: AppPortAudience;
}
export interface RunCheckPayload {
	identifier: string;
	title: string;
	task_id: string;
	generation: number;
	// Absent: the task's configured check command.
	command?: string[];
}
// A custom tool run is bound to the tool version and the validated inputs shown.
export interface RunCustomToolPayload {
	identifier: string;
	title: string;
	tool_id: string;
	tool_name: string;
	version: number;
	description: string;
	read_only: boolean;
	interpreter: string;
	inputs: Record<string, string | number | boolean>;
	task_id?: string;
	generation?: number;
	machine_id?: string;
}
export type ConversationApproval = ApprovalBase &
	(
		| { action: 'delegate_issue'; payload: DelegationPayload }
		| { action: 'merge_pull_request'; payload: MergePayload }
		| { action: 'accept_agent_work'; payload: AcceptPayload }
		| { action: 'request_agent_review'; payload: ReviewPayload }
		| { action: 'follow_up_task'; payload: FollowUpPayload }
		| { action: 'start_preview'; payload: PreviewPayload }
		| { action: 'expose_port'; payload: ExposePortPayload }
		| { action: 'run_check'; payload: RunCheckPayload }
		| { action: 'run_custom_tool'; payload: RunCustomToolPayload }
	);
interface ApprovalBase {
	id: string;
	conversation_id: string;
	tool_call_id: string;
	actor_id: string;
	issue_id: string;
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
	// What the conversation's coding agents sent it during the page's span.
	agent_messages?: AgentLiveChatMessage[];
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
	rename: (slug: string, id: string, title: string) =>
		api.patch<Conversation>(`${base(slug)}/conversations/${encodeURIComponent(id)}`, { title }),
	remove: (slug: string, id: string) => api.delete<void>(`${base(slug)}/conversations/${encodeURIComponent(id)}`),
	status: (slug: string) => api.get<AssistantStatus>(`${base(slug)}/assistant/status`),
	// project: a project id, or 'none' for chats without a visible project.
	list: (slug: string, project?: string) =>
		api.get<Conversation[]>(
			`${base(slug)}/conversations${project ? `?project_id=${encodeURIComponent(project)}` : ''}`
		),
	setProject: (slug: string, id: string, project_id: string | null) =>
		api.patch<Conversation>(`${base(slug)}/conversations/${encodeURIComponent(id)}`, { project_id }),
	openIssue: (slug: string, issue_id: string) => api.post<Conversation>(`${base(slug)}/conversations`, { issue_id }),
	create: (slug: string, title: string, project_id?: string | null) =>
		api.post<Conversation>(`${base(slug)}/conversations`, project_id ? { title, project_id } : { title }),
	history: (slug: string, id: string, before = 0) =>
		api.get<ConversationHistory>(`${base(slug)}/conversations/${encodeURIComponent(id)}?before=${before}`),
	events: (slug: string, id: string, after: number) =>
		api.get<ConversationEvent[]>(`${base(slug)}/conversations/${encodeURIComponent(id)}/events?after=${after}`),
	send: (slug: string, id: string, request_id: string, content: string, attachment_ids: string[] = []) =>
		api.post<ConversationMessage>(`${base(slug)}/conversations/${encodeURIComponent(id)}/messages`, {
			request_id,
			content,
			...(attachment_ids.length ? { attachment_ids } : {})
		}),
	attachmentURL: (slug: string, id: string, attachment: string) =>
		`${base(slug)}/conversations/${encodeURIComponent(id)}/attachments/${encodeURIComponent(attachment)}`,
	removeAttachment: (slug: string, id: string, attachment: string) =>
		api.delete<void>(
			`${base(slug)}/conversations/${encodeURIComponent(id)}/attachments/${encodeURIComponent(attachment)}`
		),
	uploadAttachment: (slug: string, id: string, file: File, onProgress?: (fraction: number) => void) =>
		uploadWithProgress<ConversationAttachment>(
			`${base(slug)}/conversations/${encodeURIComponent(id)}/attachments`,
			file,
			onProgress
		),
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

export interface UploadError {
	status: number;
	error?: { code?: string; message?: string };
}

// fetch has no upload progress, so images upload through XHR with the same
// session cookie; an expired session is refreshed once, as the client does.
function uploadWithProgress<T>(
	path: string,
	file: File,
	onProgress?: (fraction: number) => void,
	retried = false
): Promise<T> {
	return new Promise<T>((resolve, reject) => {
		const xhr = new XMLHttpRequest();
		xhr.open('POST', path);
		xhr.withCredentials = true;
		xhr.responseType = 'json';
		xhr.upload.onprogress = (event) => {
			if (event.lengthComputable) onProgress?.(event.loaded / event.total);
		};
		xhr.onerror = () => reject({ status: 0 } satisfies UploadError);
		xhr.onload = () => {
			if (xhr.status === 401 && !retried) {
				api.post<void>('/api/auth/refresh', undefined, { redirectOnUnauthorized: false }).then(
					() => uploadWithProgress<T>(path, file, onProgress, true).then(resolve, reject),
					() => reject({ status: 401 } satisfies UploadError)
				);
				return;
			}
			if (xhr.status >= 200 && xhr.status < 300) resolve(xhr.response as T);
			else reject({ status: xhr.status, ...(xhr.response ?? {}) } satisfies UploadError);
		};
		const form = new FormData();
		form.append('file', file, file.name);
		xhr.send(form);
	});
}
