import { api } from './client';
import type { Team } from '$lib/types/team';

export function listTeams(slug: string): Promise<Team[]> {
	return api.get<Team[]>(`/api/workspaces/${slug}/teams`);
}

export function createTeam(
	slug: string,
	data: { name: string; key: string; description?: string; color?: string }
): Promise<Team> {
	return api.post<Team>(`/api/workspaces/${slug}/teams`, data);
}

export function deleteTeam(slug: string, teamId: string): Promise<{ status: string }> {
	return api.delete<{ status: string }>(`/api/workspaces/${slug}/teams/${teamId}`);
}

export function leaveTeam(slug: string, teamId: string): Promise<{ status: string }> {
	return api.post<{ status: string }>(`/api/workspaces/${slug}/teams/${teamId}/leave`);
}
