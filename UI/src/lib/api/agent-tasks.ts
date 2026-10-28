import { api } from './client';
import type { AgentProvider, AgentRun, DevMachineLogChunk, ModelServer } from '$lib/types/dev-machine';
export type TaskState =
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
export interface AgentTask {
	test_command?: string[] | null;
	validation_command?: string[];
	id: string;
	workspace_id: string;
	conversation_id: string | null;
	creator_id: string;
	issue_id: string;
	team_id: string;
	request_id: string;
	prompt: string;
	provider: string;
	model: string;
	thinking: string;
	state: TaskState;
	reason: string;
	generation: number;
	failure_count: number;
	machine_id?: string | null;
	checkout_id: string;
	run_id: string;
	// The attempt stopped because the agent needs the creator's own sign-in.
	sign_in_required?: boolean;
	// A review task reviews another task's published pull request; it is shown
	// on that task's card, never as a task of its own.
	kind?: 'implement' | 'review';
	review_of_task_id?: string;
	// The task's own app process (its preview command), when one was started.
	preview?: TaskPreview | null;
	created_at: string;
	updated_at: string;
}
export interface TaskPreview {
	state: 'starting' | 'running' | 'stopped' | 'failed';
	port_id: string | null;
	port: number | null;
	error: string | null;
}
export interface ValidationStep {
	id: string;
	agent_run_id: string;
	status: 'queued' | 'running' | 'succeeded' | 'failed' | 'cancelled' | 'skipped';
	command_argv: string[];
	exit_code?: number | null;
	summary?: string | null;
	compose_file?: string;
	started_at?: string | null;
	completed_at?: string | null;
}
export interface PublicationView {
	step: ValidationStep | null;
	branch?: string | null;
	pull_request_url?: string | null;
}
export interface ValidationArtifact {
	id: string;
	step_id: string;
	path: string;
	content_type: string;
	size: number;
	created_at: string;
}
export interface AgentQueueItem {
	id: string;
	conversation_id: string;
	state: AgentTask['state'];
	reason: string;
	issue_identifier: string;
	issue_title: string;
	// The coding agent the task runs, e.g. "codex" or "claude-code".
	provider: string;
	updated_at: string;
	machine_id?: string | null;
	machine_name?: string | null;
	checkout_id?: string | null;
}
export interface TaskSignal {
	id: string;
	task_id: string;
	run_id: string;
	kind: 'ci' | 'review';
	external_id: string;
	outcome: string;
	name: string;
	summary: string;
	url: string;
	action: 'recorded' | 'follow_up' | 'budget_exhausted' | 'not_reviewable' | 'untrusted' | 'follow_up_failed';
	follow_up_generation?: number;
	created_at: string;
	updated_at: string;
}
export type SessionState =
	| 'created'
	| 'starting'
	| 'active'
	| 'waiting_input'
	| 'suspended'
	| 'completed'
	| 'failed'
	| 'cancelled';
// A requested agent review of a task's published pull request. Findings are
// the reviewer's untrusted final output.
export interface AgentReview {
	task_id: string;
	reviewed_task_id: string;
	requested_by: string;
	provider: string;
	commit_sha: string;
	repository: string;
	pull_request_number: number;
	pull_request_url: string;
	base_branch: string;
	focus: string;
	attempt: number;
	post_to_pull_request: boolean;
	verdict?: 'approve' | 'request_changes' | 'comment' | 'unknown';
	findings?: string;
	comment_state: 'not_requested' | 'pending' | 'posted' | 'failed';
	completed_at?: string;
	created_at: string;
	updated_at: string;
}
// The harness session a task's attempts ran in, with the review it ran.
export interface AgentSession {
	id: string;
	task_id: string;
	machine_id?: string;
	provider: string;
	purpose: 'implement' | 'review';
	state: SessionState;
	native_session_id?: string;
	started_at?: string;
	ended_at?: string;
	created_at: string;
	updated_at: string;
	task_state: TaskState;
	review?: AgentReview;
}
export interface TaskActivity {
	task_id: string;
	run_id: string;
	log_id: number;
	stream: string;
	truncated: boolean;
	events: Array<{ type: string; payload: unknown }>;
}
export interface TaskTransition {
	sequence: number;
	task_id: string;
	from_state: TaskState | '';
	to_state: TaskState;
	actor_id?: string;
	reason: string;
	created_at: string;
}
export interface TaskInput {
	test_command?: string[];
	request_id: string;
	prompt: string;
	provider: string;
	model: string;
	thinking: string;
	model_server_id?: string;
}
export interface TaskOptions {
	providers: AgentProvider[];
	model_servers: ModelServer[];
	default_provider?: string;
}
const base = (slug: string) => `/api/workspaces/${encodeURIComponent(slug)}`;
const task = (slug: string, id: string) => `${base(slug)}/agent-tasks/${encodeURIComponent(id)}`;
const chat = (slug: string, id: string) => `${base(slug)}/conversations/${encodeURIComponent(id)}`;
export const agentTasks = {
	options: (slug: string, id: string) => api.get<TaskOptions>(`${chat(slug, id)}/task-options`),
	list: (slug: string, id: string, before?: string) =>
		api.get<{ tasks: AgentTask[]; next_before?: string }>(
			`${chat(slug, id)}/tasks${before ? `?before=${encodeURIComponent(before)}` : ''}`
		),
	create: (slug: string, id: string, input: TaskInput) => api.post<AgentTask>(`${chat(slug, id)}/tasks`, input),
	get: (slug: string, id: string) => api.get<AgentTask>(task(slug, id)),
	cancel: (slug: string, id: string) => api.post<void>(`${task(slug, id)}/cancel`, {}),
	// Queue starting or stopping the task's app (its preview command); 202.
	startPreview: (slug: string, id: string) => api.post<void>(`${task(slug, id)}/preview`),
	stopPreview: (slug: string, id: string) => api.delete<void>(`${task(slug, id)}/preview`),
	// Keeps the sign-in finished in the machine's terminal and starts a new attempt.
	continueAfterSignIn: (slug: string, id: string, requestId: string) =>
		api.post<AgentTask>(`${task(slug, id)}/sign-in`, { request_id: requestId }),
	retry: (
		slug: string,
		id: string,
		input: { request_id: string; generation: number; prompt: string; test_command?: string[] }
	) => api.post<AgentTask>(`${task(slug, id)}/retry`, input),
	history: (slug: string, id: string, after = 0) =>
		api.get<{ transitions: TaskTransition[]; next_after?: number }>(`${task(slug, id)}/history?after=${after}`),
	runs: (slug: string, id: string, before?: string) =>
		api.get<{ runs: AgentRun[]; next_before?: string }>(
			`${task(slug, id)}/runs${before ? `?before=${encodeURIComponent(before)}` : ''}`
		),
	artifacts: (slug: string, id: string, run: string) =>
		api.get<ValidationArtifact[]>(`${task(slug, id)}/runs/${encodeURIComponent(run)}/artifacts`),
	artifact: (slug: string, id: string, run: string, artifact: string) =>
		api.blob(`${task(slug, id)}/runs/${encodeURIComponent(run)}/artifacts/${encodeURIComponent(artifact)}`),
	diff: (slug: string, id: string, run: string) =>
		api.get<{ diff: string; truncated: boolean }>(`${task(slug, id)}/runs/${encodeURIComponent(run)}/diff`),
	validation: (slug: string, id: string, run: string) =>
		api.get<ValidationStep | null>(`${task(slug, id)}/runs/${encodeURIComponent(run)}/validation`),
	queue: (slug: string) => api.get<AgentQueueItem[]>(`${base(slug)}/agent-tasks`),
	signals: (slug: string, id: string) => api.get<TaskSignal[]>(`${task(slug, id)}/signals`),
	sessions: (slug: string, id: string) => api.get<{ sessions: AgentSession[] }>(`${task(slug, id)}/sessions`),
	publication: (slug: string, id: string, run: string) =>
		api.get<PublicationView>(`${task(slug, id)}/runs/${encodeURIComponent(run)}/publication`),
	logs: (slug: string, id: string, run: string, after = 0) =>
		api.get<{ logs: DevMachineLogChunk[]; next_after?: number }>(
			`${task(slug, id)}/runs/${encodeURIComponent(run)}/logs?after=${after}`
		)
};
