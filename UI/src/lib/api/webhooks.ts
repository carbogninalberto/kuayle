import { api } from './client';

export interface Webhook {
	id: string;
	url: string;
	events: string[];
	is_active: boolean;
	created_at: string;
	updated_at: string;
}

export function listWebhooks(slug: string): Promise<Webhook[]> {
	return api.get<Webhook[]>(`/api/workspaces/${slug}/webhooks`);
}

export function createWebhook(slug: string, data: { url: string; secret: string; events: string[] }): Promise<Webhook> {
	return api.post<Webhook>(`/api/workspaces/${slug}/webhooks`, data);
}

export function updateWebhook(slug: string, id: string, data: { url?: string; events?: string[]; is_active?: boolean }): Promise<Webhook> {
	return api.patch<Webhook>(`/api/workspaces/${slug}/webhooks/${id}`, data);
}

export function deleteWebhook(slug: string, id: string): Promise<void> {
	return api.delete<void>(`/api/workspaces/${slug}/webhooks/${id}`);
}
