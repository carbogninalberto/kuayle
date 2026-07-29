import { api } from './client';
import { emitAppRefresh } from './refresh';

export type InviteLinkRole = 'member' | 'guest';
export type InviteStatus = 'valid' | 'expired' | 'revoked' | 'exhausted';

export interface InviteLink {
	id: string;
	role: InviteLinkRole;
	created_by: string;
	expires_at: string;
	max_uses: number | null;
	use_count: number;
	revoked_at: string | null;
	created_at: string;
	/** Only present in the create response — the URL is shown exactly once. */
	invite_url?: string;
}

export interface CreateInviteLinkRequest {
	role: InviteLinkRole;
	expires_in_days?: number;
	max_uses?: number;
}

export interface InvitePreview {
	workspace_name: string;
	workspace_slug: string;
	workspace_logo_url: string | null;
	role: InviteLinkRole;
	status: InviteStatus;
}

export interface InviteAcceptResponse {
	workspace_id: string;
	workspace_name: string;
	workspace_slug: string;
	role: InviteLinkRole;
}

export interface AppConfig {
	registration_enabled: boolean;
}

export async function createInviteLink(
	slug: string,
	req: CreateInviteLinkRequest
): Promise<InviteLink> {
	const result = await api.post<InviteLink>(`/api/workspaces/${slug}/invite-links`, req);
	emitAppRefresh(['invite-links'], slug);
	return result;
}

export function listInviteLinks(slug: string): Promise<InviteLink[]> {
	return api.get<InviteLink[]>(`/api/workspaces/${slug}/invite-links`);
}

export async function revokeInviteLink(slug: string, id: string): Promise<{ status: string }> {
	const result = await api.delete<{ status: string }>(`/api/workspaces/${slug}/invite-links/${id}`);
	emitAppRefresh(['invite-links'], slug);
	return result;
}

export function getInvitePreview(token: string): Promise<InvitePreview> {
	return api.get<InvitePreview>(`/api/invite/${token}`);
}

export function acceptInvite(token: string): Promise<InviteAcceptResponse> {
	return api.post<InviteAcceptResponse>(`/api/invite/${token}/accept`);
}

export function getConfig(): Promise<AppConfig> {
	return api.get<AppConfig>('/api/config');
}
