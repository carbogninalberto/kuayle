export type EstimateScale = 'linear' | 'exponential' | 'fibonacci' | 'tshirt';

export interface Team {
	id: string;
	name: string;
	key: string;
	description: string | null;
	color: string | null;
	icon: string | null;
	estimate_scale: EstimateScale | null;
	created_at: string;
	updated_at: string;
}
