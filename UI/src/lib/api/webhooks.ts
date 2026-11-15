import { api } from './client';

export interface Webhook {
	id: string;
	url: string;
	events: string[];
	is_active: boolean;
	created_at: string;
	updated_at: string;
}

/** One entry of a webhook's delivery log; it never carries the payload. */
export interface WebhookDelivery {
	id: string;
	event: string;
	status: 'pending' | 'delivered' | 'failed';
	attempts: number;
	last_status_code: number | null;
	last_error: string | null;
	next_attempt_at: string | null;
	created_at: string;
	updated_at: string;
	completed_at: string | null;
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

export function listWebhookDeliveries(slug: string, id: string): Promise<WebhookDelivery[]> {
	return api.get<WebhookDelivery[]>(`/api/workspaces/${slug}/webhooks/${id}/deliveries`);
}
