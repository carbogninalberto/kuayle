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

// Connection links for kuayle_cli. The plaintext link is present only in the
// create response; the server stores the code's hash.
export interface CLIConnectionLink {
	id: string;
	name: string;
	code_prefix: string;
	scopes: string[];
	workspace_slugs: string[] | null;
	token_expires_at: string | null;
	expires_at: string;
	max_uses: number;
	uses: number;
	created_at: string;
}

export interface CreatedCLIConnectionLink extends CLIConnectionLink {
	link: string;
}

export interface CreateCLILinkRequest {
	name?: string;
	scopes: string[];
	workspace_slugs?: string[];
	ttl_minutes?: number;
	max_uses?: number;
	token_expires_at?: string;
}

export function listCLILinks(): Promise<CLIConnectionLink[]> {
	return api.get<CLIConnectionLink[]>('/api/cli-links');
}

export function createCLILink(data: CreateCLILinkRequest): Promise<CreatedCLIConnectionLink> {
	return api.post<CreatedCLIConnectionLink>('/api/cli-links', data);
}

export function revokeCLILink(id: string): Promise<void> {
	return api.delete<void>(`/api/cli-links/${id}`);
}
