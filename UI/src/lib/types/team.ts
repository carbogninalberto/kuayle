export interface Team {
	id: string;
	name: string;
	key: string;
	description: string | null;
	color: string | null;
	icon: string | null;
	triage_enabled: boolean;
	created_at: string;
	updated_at: string;
}
