import { api } from './client';
import type { Label } from '$lib/types/label';

export function listLabels(slug: string): Promise<Label[]> {
	return api.get<Label[]>(`/api/workspaces/${slug}/labels`);
}

export function createLabel(
	slug: string,
	data: { name: string; color: string; description?: string }
): Promise<Label> {
	return api.post<Label>(`/api/workspaces/${slug}/labels`, data);
}

export function updateLabel(
	slug: string,
	id: string,
	data: { name?: string; color?: string; description?: string }
): Promise<Label> {
	return api.patch<Label>(`/api/workspaces/${slug}/labels/${id}`, data);
}

export function deleteLabel(slug: string, id: string): Promise<void> {
	return api.delete<void>(`/api/workspaces/${slug}/labels/${id}`);
}
