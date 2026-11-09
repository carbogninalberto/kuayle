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
	/** Hex colour such as #3b82f6, or null for the default. */
	color: string | null;
	/** A Lucide PascalCase name, `emoji:<emoji>`, or null for the default. */
	icon: string | null;
	progress?: ProjectProgress;
	created_at: string;
	updated_at: string;
}
