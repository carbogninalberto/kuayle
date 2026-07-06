import { api } from './client';
import { emitAppRefresh } from './refresh';
import type { Workspace } from '$lib/types/workspace';

export function listWorkspaces(): Promise<Workspace[]> {
	return api.get<Workspace[]>('/api/workspaces');
}

export function getWorkspace(slug: string): Promise<Workspace> {
	return api.get<Workspace>(`/api/workspaces/${slug}`);
}

export function createWorkspace(name: string, slug: string): Promise<Workspace> {
	return api.post<Workspace>('/api/workspaces', { name, slug });
}

export async function updateWorkspace(
	slug: string,
	data: {
		name?: string;
		logo_url?: string | null;
		share_link_min_role?: string;
	}
): Promise<Workspace> {
	const workspace = await api.patch<Workspace>(`/api/workspaces/${slug}`, data);
	emitAppRefresh(['workspace'], slug);
	return workspace;
}

export async function deleteWorkspace(slug: string): Promise<{ status: string }> {
	const result = await api.delete<{ status: string }>(`/api/workspaces/${slug}`);
	emitAppRefresh(['workspace'], slug);
	return result;
}
