export interface TeamStatus {
	id: string;
	team_id: string;
	name: string;
	slug: string;
	category: 'backlog' | 'unstarted' | 'started' | 'completed' | 'cancelled';
	color: string | null;
	position: number;
	is_default: boolean;
	created_at: string;
	updated_at: string;
}
