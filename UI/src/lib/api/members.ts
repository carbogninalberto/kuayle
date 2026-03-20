import { api } from './client';
import type { WorkspaceMember } from '$lib/types/workspace';

export function listMembers(slug: string): Promise<WorkspaceMember[]> {
	return api.get<WorkspaceMember[]>(`/api/workspaces/${slug}/members`);
}

export function updateMemberRole(
	slug: string,
	userId: string,
	role: string
): Promise<{ status: string }> {
	return api.patch<{ status: string }>(`/api/workspaces/${slug}/members/${userId}`, { role });
}

export function removeMember(slug: string, userId: string): Promise<{ status: string }> {
	return api.delete<{ status: string }>(`/api/workspaces/${slug}/members/${userId}`);
}

export function inviteMember(
	slug: string,
	email: string,
	role: string
): Promise<{ status: string }> {
	return api.post<{ status: string }>(`/api/workspaces/${slug}/invite`, { email, role });
}
