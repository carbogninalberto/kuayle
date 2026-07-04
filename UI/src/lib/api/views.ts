import { api } from './client';
import type { View, CreateViewRequest, UpdateViewRequest } from '$lib/types/view';

function emitViewsChanged(slug: string, action: 'created' | 'updated' | 'deleted', view: View | { id: string }) {
	if (typeof window === 'undefined') return;
	window.dispatchEvent(new CustomEvent('views:changed', { detail: { slug, action, view } }));
}

export function listViews(slug: string): Promise<View[]> {
	return api.get<View[]>(`/api/workspaces/${slug}/views`);
}

export function getView(slug: string, id: string): Promise<View> {
	return api.get<View>(`/api/workspaces/${slug}/views/${id}`);
}

export async function createView(slug: string, data: CreateViewRequest): Promise<View> {
	const view = await api.post<View>(`/api/workspaces/${slug}/views`, data);
	emitViewsChanged(slug, 'created', view);
	return view;
}

export async function updateView(slug: string, id: string, data: UpdateViewRequest): Promise<View> {
	const view = await api.patch<View>(`/api/workspaces/${slug}/views/${id}`, data);
	emitViewsChanged(slug, 'updated', view);
	return view;
}

export async function deleteView(slug: string, id: string): Promise<void> {
	await api.delete<void>(`/api/workspaces/${slug}/views/${id}`);
	emitViewsChanged(slug, 'deleted', { id });
}
