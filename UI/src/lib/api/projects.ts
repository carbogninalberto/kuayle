import { api } from './client';
import type { Project } from '$lib/types/project';

export function listProjects(slug: string): Promise<Project[]> {
	return api.get<Project[]>(`/api/workspaces/${slug}/projects`);
}

export function listTeamProjects(slug: string, teamId: string): Promise<Project[]> {
	return api.get<Project[]>(`/api/workspaces/${slug}/teams/${teamId}/projects`);
}

export function getProject(slug: string, id: string): Promise<Project> {
	return api.get<Project>(`/api/workspaces/${slug}/projects/${id}`);
}

export function createProject(
	slug: string,
	data: { name: string; description?: string; team_id?: string }
): Promise<Project> {
	return api.post<Project>(`/api/workspaces/${slug}/projects`, data);
}

export function updateProject(
	slug: string,
	id: string,
	data: { name?: string; description?: string; status?: string; team_id?: string | null }
): Promise<Project> {
	return api.patch<Project>(`/api/workspaces/${slug}/projects/${id}`, data);
}

export function deleteProject(slug: string, id: string): Promise<void> {
	return api.delete<void>(`/api/workspaces/${slug}/projects/${id}`);
}
