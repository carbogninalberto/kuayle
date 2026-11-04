import { api } from './client';
import { emitAppRefresh } from './refresh';
import type { Label } from '$lib/types/label';

/** With a projectId: the workspace-wide labels plus that project's own. */
export function listLabels(slug: string, projectId?: string): Promise<Label[]> {
	const query = projectId ? `?project_id=${encodeURIComponent(projectId)}` : '';
	return api.get<Label[]>(`/api/workspaces/${slug}/labels${query}`);
}

export async function createLabel(
	slug: string,
	data: { name: string; color: string; description?: string; project_id?: string }
): Promise<Label> {
	const label = await api.post<Label>(`/api/workspaces/${slug}/labels`, data);
	emitAppRefresh(['labels'], slug);
	return label;
}

export async function updateLabel(
	slug: string,
	id: string,
	data: { name?: string; color?: string; description?: string }
): Promise<Label> {
	const label = await api.patch<Label>(`/api/workspaces/${slug}/labels/${id}`, data);
	emitAppRefresh(['labels'], slug);
	return label;
}

export async function deleteLabel(slug: string, id: string): Promise<void> {
	await api.delete<void>(`/api/workspaces/${slug}/labels/${id}`);
	emitAppRefresh(['labels'], slug);
}
