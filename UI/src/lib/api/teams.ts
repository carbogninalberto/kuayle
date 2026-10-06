import { api } from './client';
import { emitAppRefresh } from './refresh';
import type { Team, TeamMember, CreateTeamInput } from '$lib/types/team';

export function listTeams(slug: string): Promise<Team[]> {
	return api.get<Team[]>(`/api/workspaces/${slug}/teams`);
}

export async function createTeam(
	slug: string,
	data: CreateTeamInput
): Promise<Team> {
	const team = await api.post<Team>(`/api/workspaces/${slug}/teams`, data);
	emitAppRefresh(['workspace', 'teams', 'issues', 'projects', 'views', 'favorites', 'members'], slug);
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
	emitAppRefresh(['workspace', 'teams', 'issues', 'projects', 'views', 'favorites', 'members'], slug);
	return team;
}

export async function deleteTeam(slug: string, teamId: string): Promise<{ status: string }> {
	const result = await api.delete<{ status: string }>(`/api/workspaces/${slug}/teams/${teamId}`);
	emitAppRefresh(['workspace', 'teams', 'issues', 'projects', 'views', 'favorites', 'members'], slug);
	return result;
}

export async function leaveTeam(slug: string, teamId: string): Promise<{ status: string }> {
	const result = await api.post<{ status: string }>(`/api/workspaces/${slug}/teams/${teamId}/leave`);
	emitAppRefresh(['workspace', 'teams', 'issues', 'projects', 'views', 'favorites', 'members'], slug);
	return result;
}

export function listTeamMembers(slug: string, teamId: string): Promise<TeamMember[]> {
 return api.get(`/api/workspaces/${slug}/teams/${teamId}/members`);
}
export async function setTeamVisibility(slug: string, teamId: string, isPrivate: boolean): Promise<Team> {
 const team = await api.patch<Team>(`/api/workspaces/${slug}/teams/${teamId}/visibility`, {
  is_private: isPrivate, confirm_public: !isPrivate, acknowledge_privacy_limitations: isPrivate
 });
 emitAppRefresh(['workspace', 'teams', 'issues', 'projects', 'views', 'favorites', 'members'], slug);
 return team;
}
export async function addTeamMember(slug: string, teamId: string, userId: string) {
 await api.post(`/api/workspaces/${slug}/teams/${teamId}/members`, { user_id: userId });
 emitAppRefresh(['teams', 'members', 'issues'], slug);
}
export async function removeTeamMember(slug: string, teamId: string, userId: string) {
 await api.delete(`/api/workspaces/${slug}/teams/${teamId}/members/${userId}`);
 emitAppRefresh(['teams', 'members', 'issues'], slug);
}
