import { api } from './client';

export interface SystemUpdateStatus {
	enabled: boolean;
	running: boolean;
	message?: string;
}

export interface SystemUpdateStart {
	running: boolean;
	message: string;
}

export function getSystemUpdateStatus(): Promise<SystemUpdateStatus> {
	return api.get<SystemUpdateStatus>('/api/system/update-status');
}

export function startSystemUpdate(): Promise<SystemUpdateStart> {
	return api.post<SystemUpdateStart>('/api/system/update');
}
