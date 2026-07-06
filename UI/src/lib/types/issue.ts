import type { Label } from './label';
import type { User } from './auth';

export type IssueStatus = 'backlog' | 'todo' | 'in_progress' | 'in_review' | 'done' | 'cancelled';
export type IssuePriority = 0 | 1 | 2 | 3 | 4;

export interface StatusInfo {
	id: string;
	name: string;
	category: string;
	color: string | null;
	position: number;
}

export interface Issue {
	id: string;
	identifier: string;
	title: string;
	description: string | null;
	status: IssueStatus;
	status_id?: string;
	status_info?: StatusInfo;
	priority: IssuePriority;
	team_id: string;
	project_id: string | null;
	cycle_id: string | null;
	creator_id: string;
	assignee_id: string | null;
	parent_id: string | null;
	due_date: string | null;
	sort_order: number;
	labels?: Label[];
	creator?: User;
	assignee?: User;
	assignees?: User[];
	parent?: IssueSummary;
	sub_issue_count?: number;
	sub_issue_done?: number;
	created_at: string;
	updated_at: string;
}

export interface IssueSummary {
	id: string;
	identifier: string;
	title: string;
}

export interface CreateIssueRequest {
	title: string;
	description?: string;
	status?: IssueStatus;
	status_id?: string;
	priority?: IssuePriority;
	team_id: string;
	project_id?: string;
	assignee_id?: string;
	assignee_ids?: string[];
	label_ids?: string[];
	parent_id?: string;
	due_date?: string;
	cycle_id?: string;
}

export interface UpdateIssueRequest {
	title?: string;
	description?: string;
	status?: IssueStatus;
	status_id?: string;
	priority?: IssuePriority;
	assignee_id?: string;
	assignee_ids?: string[];
	project_id?: string;
	cycle_id?: string;
	label_ids?: string[];
	parent_id?: string;
	due_date?: string;
	sort_order?: number;
}

export interface IssueHistory {
	id: string;
	issue_id: string;
	user_id: string;
	field: string;
	old_value: string | null;
	new_value: string | null;
	created_at: string;
}

export type RelationType = 'related' | 'blocked_by' | 'blocking' | 'duplicate';

export interface IssueRelation {
	id: string;
	issue_id: string;
	related_issue_id: string;
	type: RelationType;
	related_issue?: Issue;
	created_at: string;
}

export interface IssueTemplate {
	id: string;
	workspace_id: string;
	team_id: string | null;
	title: string;
	description: string | null;
	status: IssueStatus | null;
	priority: IssuePriority | null;
	label_ids: string[];
	assignee_id: string | null;
	recurrence_rule?: unknown;
	next_run_at: string | null;
	is_active: boolean;
	created_by: string;
	created_at: string;
	updated_at: string;
}

export interface CreateIssueTemplateRequest {
	title: string;
	description?: string;
	status?: IssueStatus;
	priority?: IssuePriority;
	label_ids?: string[];
	assignee_id?: string;
	team_id?: string;
}

export interface Comment {
	id: string;
	issue_id: string;
	user_id: string;
	body: string;
	parent_id?: string;
	resolved_at?: string;
	user?: User;
	replies?: Comment[];
	created_at: string;
	updated_at: string;
}

export const STATUS_ORDER: IssueStatus[] = ['in_progress', 'in_review', 'todo', 'backlog', 'done', 'cancelled'];

export const STATUS_LABELS: Record<IssueStatus, string> = {
	backlog: 'Backlog',
	todo: 'Todo',
	in_progress: 'In Progress',
	in_review: 'In Review',
	done: 'Done',
	cancelled: 'Cancelled'
};

export const PRIORITY_LABELS: Record<IssuePriority, string> = {
	0: 'No priority',
	1: 'Urgent',
	2: 'High',
	3: 'Medium',
	4: 'Low'
};
