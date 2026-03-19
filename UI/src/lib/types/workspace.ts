export interface Workspace {
	id: string;
	name: string;
	slug: string;
	logo_url: string | null;
	created_at: string;
	updated_at: string;
}

export interface WorkspaceMember {
	user_id: string;
	email: string;
	name: string;
	role: string;
	created_at: string;
}
