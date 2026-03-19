import { api } from './client';
import type { View, CreateViewRequest, UpdateViewRequest } from '$lib/types/view';

export function listViews(slug: string): Promise<View[]> {
	return api.get<View[]>(`/api/workspaces/${slug}/views`);
}

export function getView(slug: string, id: string): Promise<View> {
	return api.get<View>(`/api/workspaces/${slug}/views/${id}`);
}

export function createView(slug: string, data: CreateViewRequest): Promise<View> {
	return api.post<View>(`/api/workspaces/${slug}/views`, data);
}

export function updateView(slug: string, id: string, data: UpdateViewRequest): Promise<View> {
	return api.patch<View>(`/api/workspaces/${slug}/views/${id}`, data);
}

export function deleteView(slug: string, id: string): Promise<void> {
	return api.delete<void>(`/api/workspaces/${slug}/views/${id}`);
}
