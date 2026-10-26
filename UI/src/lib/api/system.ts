import { api } from './client';

export type SystemUpdateState = 'idle' | 'running' | 'succeeded' | 'failed';

export interface SystemUpdateStatus {
	enabled: boolean;
	running: boolean;
	message?: string;
	state?: SystemUpdateState;
	started_at?: string;
	finished_at?: string;
	/** Failure reason; usually the updater script's last output line. */
	error?: string;
	/** Recent updater output, included only for failed runs. */
	output?: string;
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
