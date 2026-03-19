export type CycleStatus = 'upcoming' | 'active' | 'completed';

export interface CycleProgress {
	total: number;
	completed: number;
	cancelled: number;
}

export interface Cycle {
	id: string;
	team_id: string;
	name: string;
	number: number;
	status: CycleStatus;
	description: string | null;
	start_date: string | null;
	end_date: string | null;
	completed_at: string | null;
	progress?: CycleProgress;
	created_at: string;
	updated_at: string;
}

export interface CreateCycleRequest {
	name: string;
	description?: string;
	start_date?: string;
	end_date?: string;
}

export interface UpdateCycleRequest {
	name?: string;
	description?: string;
	status?: CycleStatus;
	start_date?: string;
	end_date?: string;
}
