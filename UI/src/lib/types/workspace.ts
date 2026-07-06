export interface WorkspaceOwner {
	id: string;
	email: string;
	name: string;
	avatar_url: string | null;
}

export interface Workspace {
	id: string;
	name: string;
	slug: string;
	logo_url: string | null;
	owner_id: string;
	owner?: WorkspaceOwner | null;
	share_link_min_role: string;
	current_user_role: string;
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
