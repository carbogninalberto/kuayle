import { api } from './client';
import type { SharedLink, CreateSharedLinkRequest, UpdateSharedLinkRequest } from '$lib/types/shared-link';

export function listSharedLinks(slug: string): Promise<SharedLink[]> {
	return api.get<SharedLink[]>(`/api/workspaces/${slug}/shared-links`);
}

export function createSharedLink(slug: string, data: CreateSharedLinkRequest): Promise<SharedLink> {
	return api.post<SharedLink>(`/api/workspaces/${slug}/shared-links`, data);
}

export function updateSharedLink(slug: string, id: string, data: UpdateSharedLinkRequest): Promise<SharedLink> {
	return api.patch<SharedLink>(`/api/workspaces/${slug}/shared-links/${id}`, data);
}

export function deleteSharedLink(slug: string, id: string): Promise<void> {
	return api.delete<void>(`/api/workspaces/${slug}/shared-links/${id}`);
}
