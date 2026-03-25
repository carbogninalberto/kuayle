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
	goals: string | null;
	retrospective: string | null;
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
	goals?: string;
	start_date: string;
	end_date: string;
}

export interface CycleBurndownPoint {
	date: string;
	scope: number;
	started: number;
	completed: number;
}

export interface UpdateCycleRequest {
	name?: string;
	description?: string;
	goals?: string;
	retrospective?: string;
	status?: CycleStatus;
	start_date?: string;
	end_date?: string;
}

export interface CompleteCycleRequest {
	retrospective?: string;
	carry_over?: boolean;
}

export interface VelocityPoint {
	cycle_id: string;
	cycle_name: string;
	cycle_number: number;
	scope: number;
	completed: number;
	cancelled: number;
	start_date: string | null;
	end_date: string | null;
}
