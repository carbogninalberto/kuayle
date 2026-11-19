import { api } from './client';

export interface PushSettings {
	source: 'off' | 'app' | 'environment';
	public_key: string;
	subject: string;
	enabled: boolean;
	read_only: boolean;
	subscribed_devices: number;
	requires_reenrollment?: boolean;
}

export const getPushSettings = () => api.get<PushSettings>('/api/system/push/settings');
export const generatePushKeys = (confirm: boolean) => api.post<PushSettings>('/api/system/push/keys', { confirm });
export const savePushSettings = (subject: string, enabled: boolean) =>
	api.put<PushSettings>('/api/system/push/settings', { subject, enabled });
export const sendTestPush = () => api.post<{ sent: number; failed: number }>('/api/system/push/test');
