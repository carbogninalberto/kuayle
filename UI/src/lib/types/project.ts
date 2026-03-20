export type ProjectStatus = 'planned' | 'in_progress' | 'completed' | 'cancelled';

export interface ProjectProgress {
	total: number;
	completed: number;
	cancelled: number;
}

export interface Project {
	id: string;
	name: string;
	description: string | null;
	status: ProjectStatus;
	team_id: string | null;
	lead_id: string | null;
	start_date: string | null;
	target_date: string | null;
	sort_order: number;
	progress?: ProjectProgress;
	created_at: string;
	updated_at: string;
}
