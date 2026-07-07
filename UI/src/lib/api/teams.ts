import { api } from './client';
import { emitAppRefresh } from './refresh';
import type { Team } from '$lib/types/team';

export function listTeams(slug: string): Promise<Team[]> {
	return api.get<Team[]>(`/api/workspaces/${slug}/teams`);
}

export async function createTeam(
	slug: string,
	data: { name: string; key: string; description?: string; color?: string }
): Promise<Team> {
	const team = await api.post<Team>(`/api/workspaces/${slug}/teams`, data);
	emitAppRefresh(['teams'], slug);
	return team;
}

export async function updateTeam(
	slug: string,
	teamId: string,
	data: Partial<{
		name: string;
		description: string | null;
		color: string | null;
		icon: string | null;
		triage_enabled: boolean;
		parent_auto_close_enabled: boolean;
		sub_issue_auto_close_enabled: boolean;
		issue_copy_prompt: string | null;
	}>
): Promise<Team> {
	const team = await api.patch<Team>(`/api/workspaces/${slug}/teams/${teamId}`, data);
	emitAppRefresh(['teams'], slug);
	return team;
}

export async function deleteTeam(slug: string, teamId: string): Promise<{ status: string }> {
	const result = await api.delete<{ status: string }>(`/api/workspaces/${slug}/teams/${teamId}`);
	emitAppRefresh(['teams'], slug);
	return result;
}

export async function leaveTeam(slug: string, teamId: string): Promise<{ status: string }> {
	const result = await api.post<{ status: string }>(`/api/workspaces/${slug}/teams/${teamId}/leave`);
	emitAppRefresh(['teams'], slug);
	return result;
}
