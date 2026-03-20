export type StatusCategory = 'backlog' | 'unstarted' | 'started' | 'completed' | 'cancelled';

export interface TeamStatus {
	id: string;
	team_id: string;
	name: string;
	slug: string;
	category: StatusCategory;
	color: string | null;
	position: number;
	is_default: boolean;
	project_ids?: string[];
	created_at: string;
	updated_at: string;
}

export const CATEGORY_ORDER: StatusCategory[] = ['backlog', 'unstarted', 'started', 'completed', 'cancelled'];

export const CATEGORY_LABELS: Record<StatusCategory, string> = {
	backlog: 'Backlog',
	unstarted: 'Unstarted',
	started: 'Started',
	completed: 'Completed',
	cancelled: 'Cancelled',
};
