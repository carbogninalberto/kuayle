import { api } from './client';

export type SSODefaultRole = 'member' | 'guest';

export interface SSOConfig {
	issuer_url: string;
	client_id: string;
	has_client_secret: boolean;
	allowed_domains: string[];
	default_role: SSODefaultRole;
	require_sso: boolean;
	enabled: boolean;
	provider_name: string;
	managed_by_instance: boolean;
	redirect_uri: string;
}

export interface SSOConfigRequest {
	issuer_url: string;
	client_id: string;
	/** Write-only: omit or leave empty to keep the stored secret. */
	client_secret?: string;
	allowed_domains: string[];
	default_role: SSODefaultRole;
	require_sso: boolean;
	enabled: boolean;
	provider_name: string;
}

export interface SSOPublicInfo {
	enabled: boolean;
	provider_name: string;
}

const path = (slug: string) => `/api/workspaces/${encodeURIComponent(slug)}/sso`;

export const sso = {
	get: (slug: string) => api.get<SSOConfig>(path(slug)),
	save: (slug: string, request: SSOConfigRequest) => api.put<SSOConfig>(path(slug), request),
	remove: (slug: string) => api.delete<{ status: string }>(path(slug)),
	publicInfo: (slug: string) =>
		api.get<SSOPublicInfo>(`/api/auth/sso/${encodeURIComponent(slug)}`, {
			redirectOnUnauthorized: false,
			retryUnauthorized: false
		})
};
