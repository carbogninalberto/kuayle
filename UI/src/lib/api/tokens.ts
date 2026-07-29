import { api } from './client';

// Mirrors BE internal/dto/token.go (PR #55). The plaintext token is only
// present in the create response — it is shown exactly once.
export interface PersonalAccessToken {
	id: string;
	name: string;
	token_prefix: string;
	scopes: string[];
	workspace_slugs: string[] | null; // null = all workspaces
	expires_at: string | null; // null = never expires
	last_used_at: string | null;
	created_at: string;
}

export interface CreatedPersonalAccessToken extends PersonalAccessToken {
	token: string;
}

export interface CreateTokenRequest {
	name: string;
	scopes: string[];
	workspace_slugs?: string[];
	expires_at?: string;
}

export function listTokens(): Promise<PersonalAccessToken[]> {
	return api.get<PersonalAccessToken[]>('/api/tokens');
}

export function createToken(data: CreateTokenRequest): Promise<CreatedPersonalAccessToken> {
	return api.post<CreatedPersonalAccessToken>('/api/tokens', data);
}

export function revokeToken(id: string): Promise<void> {
	return api.delete<void>(`/api/tokens/${id}`);
}
