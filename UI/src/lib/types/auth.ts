export interface User {
	id: string;
	email: string;
	name: string;
	display_name: string;
	avatar_url: string | null;
	is_sysadmin: boolean;
}

export interface LoginRequest {
	email: string;
	password: string;
}

export interface RegisterRequest {
	email: string;
	password: string;
	name: string;
}

export interface UpdateProfileRequest {
	name?: string;
	display_name?: string;
	avatar_url?: string | null;
}
