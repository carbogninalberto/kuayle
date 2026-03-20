import { api } from './client';
import type { TeamStatus } from '$lib/types/team-status';

export function listTeamStatuses(slug: string, teamId: string): Promise<TeamStatus[]> {
	return api.get<TeamStatus[]>(`/api/workspaces/${slug}/teams/${teamId}/statuses`);
}

export function createTeamStatus(
	slug: string,
	teamId: string,
	req: { name: string; category: string; color?: string; project_ids?: string[] }
): Promise<TeamStatus> {
	return api.post<TeamStatus>(`/api/workspaces/${slug}/teams/${teamId}/statuses`, req);
}

export function updateTeamStatus(
	slug: string,
	teamId: string,
	statusId: string,
	req: { name?: string; color?: string; position?: number; project_ids?: string[] }
): Promise<TeamStatus> {
	return api.patch<TeamStatus>(`/api/workspaces/${slug}/teams/${teamId}/statuses/${statusId}`, req);
}

export function deleteTeamStatus(slug: string, teamId: string, statusId: string): Promise<void> {
	return api.delete<void>(`/api/workspaces/${slug}/teams/${teamId}/statuses/${statusId}`);
}
