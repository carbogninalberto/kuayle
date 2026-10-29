import { api } from './client';

export type AssistantToolStatus = 'draft' | 'active';
export type AssistantToolAuthor = 'user' | 'assistant';
export type AssistantToolInterpreter = 'sh' | 'bash' | 'node' | 'python3';
/** `auto` runs without approval and is allowed only for read-only tools. */
export type AssistantToolRunMode = 'approval' | 'auto';
export type AssistantToolRunStatus = 'queued' | 'running' | 'succeeded' | 'failed';

/** One parameter of a custom tool's schema; only scalar types and string enums. */
export interface AssistantToolParameterSchema {
	type: 'string' | 'number' | 'integer' | 'boolean';
	description?: string;
	enum?: string[];
	minLength?: number;
	maxLength?: number;
	minimum?: number;
	maximum?: number;
}

/** A custom tool's parameters: a JSON Schema object of scalar properties. */
export interface AssistantToolParameters {
	type: 'object';
	properties: Record<string, AssistantToolParameterSchema>;
	required?: string[];
	additionalProperties?: false;
}

/** A workspace custom tool. `script` is present only when one tool is read. */
export interface AssistantCustomTool {
	id: string;
	workspace_id: string;
	name: string;
	description: string;
	parameters: AssistantToolParameters;
	script?: string;
	interpreter: AssistantToolInterpreter;
	run_mode: AssistantToolRunMode;
	read_only: boolean;
	timeout_seconds: number;
	enabled: boolean;
	status: AssistantToolStatus;
	/** The version in effect. */
	current_version: number;
	latest_version: number;
	/** A draft edit of an active tool waiting for activation. */
	pending_version: number | null;
	created_by_user_id: string | null;
	author_kind: AssistantToolAuthor;
	created_at: string;
	updated_at: string;
}

export interface AssistantCustomToolVersion {
	tool_id: string;
	version: number;
	name: string;
	description: string;
	parameters: AssistantToolParameters;
	script: string;
	interpreter: AssistantToolInterpreter;
	run_mode: AssistantToolRunMode;
	read_only: boolean;
	timeout_seconds: number;
	status: AssistantToolStatus;
	author_kind: AssistantToolAuthor;
	author_user_id: string | null;
	created_at: string;
}

export interface AssistantCustomToolList {
	tools: AssistantCustomTool[];
	/** The interpreters the check container provides. */
	interpreters: AssistantToolInterpreter[];
	/** Owners and admins can write and test-run tools; the server enforces it. */
	can_manage: boolean;
}

export interface AssistantCustomToolContent {
	name: string;
	description: string;
	parameters: AssistantToolParameters;
	script: string;
	/** Defaults to sh. */
	interpreter?: AssistantToolInterpreter;
	/** Defaults to approval. */
	run_mode?: AssistantToolRunMode;
	read_only?: boolean;
	/** 10 to 1800; defaults to 600. */
	timeout_seconds?: number;
	/** Without a status, updates keep the tool's own; creates default to active. */
	status?: AssistantToolStatus;
}

/** One execution of a tool version on a dev machine. */
export interface AssistantToolRun {
	id: string;
	workspace_id: string;
	tool_id: string;
	tool_version: number;
	tool_name: string;
	inputs: Record<string, string | number | boolean>;
	requested_by_user_id: string | null;
	conversation_id: string | null;
	tool_call_id: string | null;
	approval_id: string | null;
	machine_id: string | null;
	issue_id: string | null;
	task_id: string | null;
	working_directory: string;
	timeout_seconds: number;
	/** An owner's or admin's test run from Settings. */
	test: boolean;
	status: AssistantToolRunStatus;
	exit_code: number | null;
	/** The redacted tail of the output (at most 64 KiB). */
	output: string;
	output_truncated: boolean;
	error: string | null;
	created_at: string;
	started_at: string | null;
	finished_at: string | null;
	duration_ms: number | null;
}

/** A test run: a machine you own, or an issue whose newest coding task you created. */
export interface AssistantToolTestRun {
	inputs: Record<string, string | number | boolean>;
	machine_id?: string;
	identifier?: string;
	/** Defaults to the newest version, even a draft. */
	version?: number;
}

const base = (slug: string) => `/api/workspaces/${encodeURIComponent(slug)}/assistant-tools`;
const one = (slug: string, id: string) => `${base(slug)}/${encodeURIComponent(id)}`;

export const assistantTools = {
	list: (slug: string) => api.get<AssistantCustomToolList>(base(slug)),
	get: (slug: string, id: string) => api.get<AssistantCustomTool>(one(slug, id)),
	versions: (slug: string, id: string) =>
		api.get<{ versions: AssistantCustomToolVersion[] }>(`${one(slug, id)}/versions`),
	create: (slug: string, input: AssistantCustomToolContent) => api.post<AssistantCustomTool>(base(slug), input),
	update: (slug: string, id: string, input: AssistantCustomToolContent) =>
		api.put<AssistantCustomTool>(one(slug, id), input),
	setEnabled: (slug: string, id: string, enabled: boolean) =>
		api.put<AssistantCustomTool>(`${one(slug, id)}/enabled`, { enabled }),
	/** Activates the given draft version, or the newest draft. */
	activate: (slug: string, id: string, version?: number) =>
		api.post<AssistantCustomTool>(`${one(slug, id)}/activate`, version === undefined ? {} : { version }),
	restore: (slug: string, id: string, version: number) =>
		api.post<AssistantCustomTool>(`${one(slug, id)}/versions/${version}/restore`),
	remove: (slug: string, id: string) => api.delete<void>(one(slug, id)),
	testRun: (slug: string, id: string, input: AssistantToolTestRun) =>
		api.post<AssistantToolRun>(`${one(slug, id)}/test-run`, input),
	runs: (slug: string, id: string) => api.get<{ runs: AssistantToolRun[] }>(`${one(slug, id)}/runs`),
	run: (slug: string, runId: string) =>
		api.get<AssistantToolRun>(
			`/api/workspaces/${encodeURIComponent(slug)}/assistant-tool-runs/${encodeURIComponent(runId)}`
		)
};
