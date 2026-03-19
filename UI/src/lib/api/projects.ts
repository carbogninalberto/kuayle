import { api } from './client';
import type { Project } from '$lib/types/project';

export function listProjects(slug: string): Promise<Project[]> {
	return api.get<Project[]>(`/api/workspaces/${slug}/projects`);
}

export function getProject(slug: string, id: string): Promise<Project> {
	return api.get<Project>(`/api/workspaces/${slug}/projects/${id}`);
}

export function createProject(
	slug: string,
	data: { name: string; description?: string }
): Promise<Project> {
	return api.post<Project>(`/api/workspaces/${slug}/projects`, data);
}
