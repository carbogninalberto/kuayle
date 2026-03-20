import { api } from './client';

export interface PreferencesData {
	font_size: string;
	pointer_cursors: boolean;
	theme_mode: string;
	light_theme: string;
	dark_theme: string;
}

export function getPreferences(): Promise<PreferencesData> {
	return api.get<PreferencesData>('/api/preferences');
}

export function updatePreferences(data: Partial<PreferencesData>): Promise<PreferencesData> {
	return api.patch<PreferencesData>('/api/preferences', data);
}
