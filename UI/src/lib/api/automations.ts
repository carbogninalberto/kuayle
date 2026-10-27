import { api } from './client';

export interface AgentAutomation {
	id: string;
	team_id: string;
	owner_id: string;
	name: string;
	enabled: boolean;
	label_id?: string | null;
	status_id?: string | null;
	instructions: string;
	unattended: boolean;
	run_hour?: number | null;
	max_active: number;
	created_at: string;
}

export interface AgentAutomationRun {
	id: string;
	automation_id: string;
	automation_name: string;
	issue_id: string;
	issue_identifier: string;
	issue_title: string;
	status: 'pending_approval' | 'starting' | 'started' | 'dismissed' | 'failed';
	task_id?: string | null;
	error?: string | null;
	created_at: string;
}

export interface AgentAutomationInput {
	team_id: string;
	name: string;
	label_id?: string;
	status_id?: string;
	instructions: string;
	unattended: boolean;
	run_hour?: number;
	max_active: number;
}

const base = (slug: string) => `/api/workspaces/${slug}`;

export const automations = {
	list: (slug: string) => api.get<AgentAutomation[]>(`${base(slug)}/agent-automations`),
	create: (slug: string, input: AgentAutomationInput) =>
		api.post<AgentAutomation>(`${base(slug)}/agent-automations`, input),
	setEnabled: (slug: string, id: string, enabled: boolean) =>
		api.patch<AgentAutomation>(`${base(slug)}/agent-automations/${id}`, { enabled }),
	remove: (slug: string, id: string) => api.delete<void>(`${base(slug)}/agent-automations/${id}`),
	runs: (slug: string) => api.get<AgentAutomationRun[]>(`${base(slug)}/agent-automation-runs`),
	decide: (slug: string, id: string, approve: boolean) =>
		api.post<AgentAutomationRun>(`${base(slug)}/agent-automation-runs/${id}/${approve ? 'approve' : 'dismiss'}`)
};
