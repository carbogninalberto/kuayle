import { api } from './client';

/** An agent profile seen as a workspace member (KUA-107). */
export interface AgentIdentity {
	id: string;
	name: string;
	role: string;
	harness: string;
	model: string;
	effort: string;
	avatar_color: string;
	builtin_key?: string;
	/** Teams where the agent can be assigned and mentioned. */
	team_ids: string[];
	/** Whether the workspace's machine policy lets the agent run now. */
	available: boolean;
	unavailable_reason?: string;
}

export interface AgentIdentityList {
	agents: AgentIdentity[];
	can_manage: boolean;
	can_assign: boolean;
}

/** An agent assigned to an issue, with its task's live state. */
export interface IssueAgentAssignee {
	issue_id: string;
	id: string;
	name: string;
	role: string;
	harness: string;
	model: string;
	avatar_color: string;
	assigned_by: string | null;
	assigned_by_name: string | null;
	task_id: string | null;
	task_state: string | null;
	task_reason: string | null;
	error?: string | null;
	created_at: string;
}

const base = (slug: string) => `/api/workspaces/${slug}`;

export const agentsApi = {
	list: (slug: string) => api.get<AgentIdentityList>(`${base(slug)}/agents`),
	update: (slug: string, id: string, input: { avatar_color?: string; team_ids?: string[] }) =>
		api.put<void>(`${base(slug)}/agents/${id}`, input),
	forIssue: (slug: string, identifier: string) =>
		api.get<IssueAgentAssignee[]>(`${base(slug)}/issues/${identifier}/agents`),
	setForIssue: (slug: string, identifier: string, agentIds: string[]) =>
		api.put<IssueAgentAssignee[]>(`${base(slug)}/issues/${identifier}/agents`, { agent_ids: agentIds })
};
