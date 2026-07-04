import { api } from './client';
import { emitAppRefresh } from './refresh';
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

export async function createProject(
	slug: string,
	data: { name: string; description?: string; team_id?: string }
): Promise<Project> {
	const project = await api.post<Project>(`/api/workspaces/${slug}/projects`, data);
	emitAppRefresh(['projects'], slug);
	return project;
}

export async function updateProject(
	slug: string,
	id: string,
	data: { name?: string; description?: string; status?: string; team_id?: string | null }
): Promise<Project> {
	const project = await api.patch<Project>(`/api/workspaces/${slug}/projects/${id}`, data);
	emitAppRefresh(['projects'], slug);
	return project;
}

export async function deleteProject(slug: string, id: string): Promise<void> {
	await api.delete<void>(`/api/workspaces/${slug}/projects/${id}`);
	emitAppRefresh(['projects'], slug);
}
