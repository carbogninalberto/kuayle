import type { Label } from './label';
import type { User } from './auth';

export type IssueStatus = 'backlog' | 'todo' | 'in_progress' | 'in_review' | 'done' | 'cancelled';
export type IssuePriority = 0 | 1 | 2 | 3 | 4;

export interface Issue {
	id: string;
	identifier: string;
	title: string;
	description: string | null;
	status: IssueStatus;
	priority: IssuePriority;
	team_id: string;
	project_id: string | null;
	cycle_id: string | null;
	creator_id: string;
	assignee_id: string | null;
	parent_id: string | null;
	estimate: number | null;
	due_date: string | null;
	sort_order: number;
	labels?: Label[];
	creator?: User;
	assignee?: User;
	created_at: string;
	updated_at: string;
}

export interface CreateIssueRequest {
	title: string;
	description?: string;
	status?: IssueStatus;
	priority?: IssuePriority;
	team_id: string;
	project_id?: string;
	assignee_id?: string;
	label_ids?: string[];
	parent_id?: string;
	estimate?: number;
	due_date?: string;
	cycle_id?: string;
}

export interface UpdateIssueRequest {
	title?: string;
	description?: string;
	status?: IssueStatus;
	priority?: IssuePriority;
	assignee_id?: string;
	project_id?: string;
	cycle_id?: string;
	label_ids?: string[];
	parent_id?: string;
	estimate?: number;
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

export interface Comment {
	id: string;
	issue_id: string;
	user_id: string;
	body: string;
	user?: User;
	created_at: string;
	updated_at: string;
}

export const STATUS_ORDER: IssueStatus[] = ['backlog', 'todo', 'in_progress', 'in_review', 'done', 'cancelled'];

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
