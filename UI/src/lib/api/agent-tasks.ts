import { api } from './client';
import type { AgentProvider, AgentRun, DevMachineLogChunk, WorkspaceModelProvider } from '$lib/types/dev-machine';
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
	created_at: string;
	updated_at: string;
}
export interface ValidationStep {
	id: string;
	agent_run_id: string;
	status: 'queued' | 'running' | 'succeeded' | 'failed' | 'cancelled' | 'skipped';
	command_argv: string[];
	exit_code?: number | null;
	started_at?: string | null;
	completed_at?: string | null;
}
export interface PublicationView {
	step: ValidationStep | null;
	branch?: string | null;
	pull_request_url?: string | null;
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
}
export interface TaskOptions {
	providers: AgentProvider[];
	model_provider: WorkspaceModelProvider;
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
	validation: (slug: string, id: string, run: string) =>
		api.get<ValidationStep | null>(`${task(slug, id)}/runs/${encodeURIComponent(run)}/validation`),
	publication: (slug: string, id: string, run: string) =>
		api.get<PublicationView>(`${task(slug, id)}/runs/${encodeURIComponent(run)}/publication`),
	logs: (slug: string, id: string, run: string, after = 0) =>
		api.get<{ logs: DevMachineLogChunk[]; next_after?: number }>(
			`${task(slug, id)}/runs/${encodeURIComponent(run)}/logs?after=${after}`
		)
};
