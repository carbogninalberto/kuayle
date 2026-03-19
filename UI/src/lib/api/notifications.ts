import { api } from './client';
import type { Notification, NotificationListResponse } from '$lib/types/notification';

export function listNotifications(tab?: string): Promise<NotificationListResponse> {
	const query = tab ? `?tab=${tab}` : '';
	return api.get<NotificationListResponse>(`/api/notifications${query}`);
}

export function markNotificationRead(id: string): Promise<Notification> {
	return api.patch<Notification>(`/api/notifications/${id}`, {
		read_at: new Date().toISOString()
	});
}

export function snoozeNotification(id: string, until: string): Promise<Notification> {
	return api.post<Notification>(`/api/notifications/${id}/snooze`, { until });
}

export function archiveNotification(id: string): Promise<Notification> {
	return api.post<Notification>(`/api/notifications/${id}/archive`);
}

export function markAllRead(): Promise<void> {
	return api.post<void>('/api/notifications/mark-all-read');
}
