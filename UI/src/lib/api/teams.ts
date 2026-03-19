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
