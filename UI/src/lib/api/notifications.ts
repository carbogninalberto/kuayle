import { api } from './client';
import type { Notification } from '$lib/types/notification';

export function listNotifications(): Promise<Notification[]> {
	return api.get<Notification[]>('/api/notifications');
}

export function markNotificationRead(id: string): Promise<Notification> {
	return api.patch<Notification>(`/api/notifications/${id}`, {
		read_at: new Date().toISOString()
	});
}

export function markAllRead(): Promise<void> {
	return api.post<void>('/api/notifications/mark-all-read');
}
