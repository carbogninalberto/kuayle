export type ProjectStatus = 'planned' | 'in_progress' | 'completed' | 'cancelled';

export interface Project {
	id: string;
	name: string;
	description: string | null;
	status: ProjectStatus;
	lead_id: string | null;
	start_date: string | null;
	target_date: string | null;
	sort_order: number;
	created_at: string;
	updated_at: string;
}
