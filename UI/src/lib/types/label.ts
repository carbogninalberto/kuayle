export interface Label {
	id: string;
	name: string;
	color: string;
	description: string | null;
	parent_id: string | null;
	created_at: string;
	updated_at: string;
}
