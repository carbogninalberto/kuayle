import { api } from './client';

export interface Favorite {
	id: string;
	entity_type: string;
	entity_id: string;
	position: number;
	created_at: string;
}

export function listFavorites(slug: string): Promise<Favorite[]> {
	return api.get<Favorite[]>(`/api/workspaces/${slug}/favorites`);
}

export function createFavorite(
	slug: string,
	entityType: string,
	entityId: string
): Promise<Favorite> {
	return api.post<Favorite>(`/api/workspaces/${slug}/favorites`, {
		entity_type: entityType,
		entity_id: entityId
	});
}

export function deleteFavorite(slug: string, id: string): Promise<void> {
	return api.delete<void>(`/api/workspaces/${slug}/favorites/${id}`);
}
