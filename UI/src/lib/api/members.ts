import { api } from './client';
import { emitAppRefresh } from './refresh';
import type { WorkspaceMember } from '$lib/types/workspace';

export function listMembers(slug: string): Promise<WorkspaceMember[]> {
	return api.get<WorkspaceMember[]>(`/api/workspaces/${slug}/members`);
}

export async function updateMemberRole(
	slug: string,
	userId: string,
	role: string
): Promise<{ status: string }> {
	const result = await api.patch<{ status: string }>(`/api/workspaces/${slug}/members/${userId}`, { role });
	emitAppRefresh(['members'], slug);
	return result;
}

export async function removeMember(slug: string, userId: string): Promise<{ status: string }> {
	const result = await api.delete<{ status: string }>(`/api/workspaces/${slug}/members/${userId}`);
	emitAppRefresh(['members'], slug);
	return result;
}

export async function inviteMember(
	slug: string,
	email: string,
	role: string
): Promise<{ status: string }> {
	const result = await api.post<{ status: string }>(`/api/workspaces/${slug}/invite`, { email, role });
	emitAppRefresh(['members'], slug);
	return result;
}
