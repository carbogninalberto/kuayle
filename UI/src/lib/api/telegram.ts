import { api } from './client';
export interface TelegramSettings {
	configured: boolean;
	owner: boolean;
	enabled: boolean;
	bot_username: string;
	status: 'not_configured' | 'saved' | 'connected' | 'disabled' | 'connection_failed';
	allowed_user_ids?: number[];
	linked_user_id: number | null;
}
export interface TelegramSettingsRequest {
	bot_token?: string | null;
	enabled: boolean;
	allowed_user_ids: number[];
}
const path = (slug: string) => `/api/workspaces/${encodeURIComponent(slug)}/telegram`;
export const telegram = {
	get: (slug: string) => api.get<TelegramSettings>(path(slug)),
	save: (slug: string, request: TelegramSettingsRequest) => api.patch<TelegramSettings>(path(slug), request),
	link: (slug: string, id: number) =>
		api.post<{ command: string; expires_in: number }>(`${path(slug)}/link`, { telegram_user_id: id }),
	unlink: (slug: string) => api.delete<void>(`${path(slug)}/link`)
};
