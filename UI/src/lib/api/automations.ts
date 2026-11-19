import type { TaskState } from './agent-tasks';
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
	workflow_id?: string;
	pinned_version?: number;
	cron?: string;
	timezone?: string;
	next_run_at?: string;
	schedule_error?: string;
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
	workflow_name?: string;
	pinned_version?: number;
	task_version?: number;
	task_step?: string;
	task_state?: TaskState;
	attempts: number;
	replays: number;
	can_replay?: boolean;
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
	workflow_id?: string;
	version_policy?: 'latest' | 'pin';
	cron?: string;
	timezone?: string;
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
	runs: (slug: string, rule?: string, before?: string) => {
		const query = new URLSearchParams();
		if (rule) query.set('automation_id', rule);
		if (before) query.set('before', before);
		return api.get<AgentAutomationRun[]>(`${base(slug)}/agent-automation-runs${query.size ? '?' + query : ''}`);
	},
	replay: (slug: string, id: string) => api.post<AgentAutomationRun>(`${base(slug)}/agent-automation-runs/${id}/replay`),
	decide: (slug: string, id: string, approve: boolean) =>
		api.post<AgentAutomationRun>(`${base(slug)}/agent-automation-runs/${id}/${approve ? 'approve' : 'dismiss'}`)
};
