import { api } from './client';

// Configurable workflows (KUA-95): agent profiles, workflows (immutable
// versions of a state machine graph), defaults and a task's workflow run.
// The server validates every graph and enforces who may edit.

export type AgentRole = 'implementer' | 'reviewer' | 'tester' | 'docs_writer' | 'security_reviewer';

export interface AgentProfile {
	id: string;
	workspace_id: string;
	name: string;
	role: AgentRole;
	harness: string;
	model: string;
	effort: string;
	instructions: string;
	/** Workspace-wide library skills the agent gets, in order. */
	skill_ids: string[];
	builtin_key?: string | null;
	archived_at?: string | null;
	created_at: string;
	updated_at: string;
}

export interface AgentProfileInput {
	name: string;
	role: AgentRole;
	harness: string;
	model: string;
	effort: string;
	instructions: string;
	/** Replaces the profile's skills; omitted keeps them. */
	skill_ids?: string[];
}

export interface AgentProfileList {
	profiles: AgentProfile[];
	/** Owners and admins edit; the server enforces it. */
	can_manage: boolean;
	harnesses: string[];
	/** The harnesses that take a model and an effort; absent on older servers. */
	model_harnesses?: string[];
	roles: AgentRole[];
}

export type WorkflowNodeType = 'start' | 'agent' | 'check' | 'approval' | 'publish' | 'merge' | 'end';

export type WorkflowCondition =
	| 'agent_finished'
	| 'agent_failed'
	| 'checks_passed'
	| 'checks_failed'
	| 'review_approved'
	| 'review_changes_requested'
	| 'approval_approved'
	| 'approval_denied'
	| 'published'
	| 'publish_failed'
	| 'merged'
	| 'timeout'
	| 'else';

export interface WorkflowNode {
	id: string;
	type: WorkflowNodeType;
	label: string;
	profile_id?: string | null;
	prompt?: string;
	check?: 'tests' | 'ci';
	max_visits?: number;
	timeout_minutes?: number;
	outcome?: 'done' | 'failed';
	position: { x: number; y: number };
}

export interface WorkflowEdge {
	id: string;
	source: string;
	target: string;
	condition: WorkflowCondition;
}

export interface WorkflowBudget {
	max_iterations?: number;
	max_minutes?: number;
	on_exhausted?: 'fail' | 'ask';
}

export interface WorkflowGraph {
	nodes: WorkflowNode[];
	edges: WorkflowEdge[];
	budget: WorkflowBudget;
}

/** A validation error located on a node or a transition. */
export interface WorkflowIssue {
	code: string;
	message: string;
	node_id?: string;
	edge_id?: string;
}

export interface WorkflowSummary {
	/** "standard" for the Standard workflow, built-in or as the workspace edited it. */
	id: string;
	name: string;
	description: string;
	builtin: boolean;
	/** The workspace edited Standard; deleting it restores the built-in flow. */
	customized: boolean;
	latest_version: number;
	version_id?: string | null;
	archived: boolean;
	graph: WorkflowGraph;
	updated_at?: string;
}

export interface WorkflowVersion {
	id: string;
	workflow_id: string;
	version: number;
	created_by?: string | null;
	created_at: string;
}

export interface WorkflowDetail extends WorkflowSummary {
	versions: WorkflowVersion[];
}

export type WorkflowScope = 'workspace' | 'team' | 'project';

export interface WorkflowDefault {
	workspace_id: string;
	scope: WorkflowScope;
	scope_id: string;
	/** null chooses the built-in Standard workflow. */
	workflow_id: string | null;
}

export interface WorkflowList {
	workflows: WorkflowSummary[];
	defaults: WorkflowDefault[];
	can_manage: boolean;
}

export interface WorkflowStep {
	task_id: string;
	sequence: number;
	node_id: string;
	node_type: WorkflowNodeType;
	visit: number;
	from_node?: string | null;
	edge_id?: string | null;
	condition?: WorkflowCondition | null;
	reason: string;
	generation?: number | null;
	review_task_id?: string | null;
	outcome?: string | null;
	detail: string;
	decided_by?: string | null;
	entered_at: string;
	finished_at?: string | null;
}

export type WorkflowStatus = 'running' | 'waiting' | 'paused' | 'done' | 'failed';

/** A task's workflow: its graph and the path it took. */
export interface TaskWorkflow {
	workflow_id: string;
	name: string;
	version: number;
	builtin: boolean;
	graph: WorkflowGraph;
	node: string;
	/** A workflow status, or for Standard tasks the task's state. */
	status: string;
	visits: Record<string, number>;
	iterations: number;
	sequence: number;
	steps: WorkflowStep[];
	profiles: AgentProfile[];
	can_decide: boolean;
	task_state: string;
	task_reason: string;
	task_generation: number;
}

export type WorkflowDecision = 'approve' | 'deny' | 'continue' | 'stop';

export interface WorkflowSaveInput {
	name: string;
	description: string;
	graph?: WorkflowGraph;
	/** A built-in starting point instead of a graph. */
	template?: 'review_loop' | 'full_cycle' | 'standard';
	expected_version?: number;
}

const base = (slug: string) => `/api/workspaces/${encodeURIComponent(slug)}`;
const one = (slug: string, id: string) => `${base(slug)}/workflows/${encodeURIComponent(id)}`;
const profile = (slug: string, id: string) => `${base(slug)}/agent-profiles/${encodeURIComponent(id)}`;
const task = (slug: string, id: string) => `${base(slug)}/agent-tasks/${encodeURIComponent(id)}/workflow`;

export const agentProfiles = {
	list: (slug: string) => api.get<AgentProfileList>(`${base(slug)}/agent-profiles`),
	create: (slug: string, input: AgentProfileInput) => api.post<AgentProfile>(`${base(slug)}/agent-profiles`, input),
	update: (slug: string, id: string, input: AgentProfileInput) => api.put<AgentProfile>(profile(slug, id), input),
	archive: (slug: string, id: string) => api.delete<void>(profile(slug, id))
};

export const workflows = {
	list: (slug: string) => api.get<WorkflowList>(`${base(slug)}/workflows`),
	get: (slug: string, id: string) => api.get<WorkflowDetail>(one(slug, id)),
	create: (slug: string, input: WorkflowSaveInput) => api.post<WorkflowSummary>(`${base(slug)}/workflows`, input),
	/** Saves the graph as the next version; expected_version guards concurrent edits. */
	save: (slug: string, id: string, input: WorkflowSaveInput) => api.put<WorkflowSummary>(one(slug, id), input),
	duplicate: (slug: string, id: string, name?: string) =>
		api.post<WorkflowSummary>(`${one(slug, id)}/duplicate`, name ? { name } : {}),
	/** Deletes a workflow; for "standard", discards the workspace's edits. */
	remove: (slug: string, id: string) => api.delete<void>(one(slug, id)),
	validate: (slug: string, graph: WorkflowGraph) =>
		api.post<{ issues: WorkflowIssue[] }>(`${base(slug)}/workflows/validate`, { graph }),
	/** workflow_id "standard" chooses Standard; null makes a team or project inherit. */
	setDefault: (slug: string, scope: WorkflowScope, scopeId: string, workflowId: string | null) =>
		api.put<WorkflowList>(`${base(slug)}/workflow-defaults`, { scope, scope_id: scopeId, workflow_id: workflowId }),
	task: (slug: string, taskId: string) => api.get<TaskWorkflow>(task(slug, taskId)),
	decide: (slug: string, taskId: string, sequence: number, decision: WorkflowDecision) =>
		api.post<{ task_id: string; state: string; workflow_sequence: number }>(`${task(slug, taskId)}/decision`, {
			sequence,
			decision
		})
};
