import { api } from './client';
import { emitAppRefresh } from './refresh';
import type { Notification, NotificationListResponse } from '$lib/types/notification';

export function listNotifications(tab?: string): Promise<NotificationListResponse> {
	const query = tab ? `?tab=${tab}` : '';
	return api.get<NotificationListResponse>(`/api/notifications${query}`);
}

export async function markNotificationRead(id: string): Promise<Notification> {
	const notification = await api.patch<Notification>(`/api/notifications/${id}`, {
		read_at: new Date().toISOString()
	});
	emitAppRefresh(['notifications']);
	return notification;
}

export async function snoozeNotification(id: string, until: string): Promise<Notification> {
	const notification = await api.post<Notification>(`/api/notifications/${id}/snooze`, { until });
	emitAppRefresh(['notifications']);
	return notification;
}

export async function archiveNotification(id: string): Promise<Notification> {
	const notification = await api.post<Notification>(`/api/notifications/${id}/archive`);
	emitAppRefresh(['notifications']);
	return notification;
}

export async function markAllRead(): Promise<void> {
	await api.post<void>('/api/notifications/mark-all-read');
	emitAppRefresh(['notifications']);
}
