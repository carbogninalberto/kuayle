export interface User {
	id: string;
	email: string;
	name: string;
	display_name: string;
	avatar_url: string | null;
	is_sysadmin: boolean;
	/** Set when this session was minted by workspace SSO and is scoped to it. */
	sso_workspace_slug?: string;
}

export interface LoginRequest {
	email: string;
	password: string;
}

export interface RegisterRequest {
	email: string;
	password: string;
	name: string;
	invite_token?: string;
}

export interface UpdateProfileRequest {
	name?: string;
	display_name?: string;
	avatar_url?: string | null;
}
