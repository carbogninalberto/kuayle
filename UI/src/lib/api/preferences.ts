import { api } from './client';

export interface PreferencesData {
	font_size: string;
	pointer_cursors: boolean;
	theme_mode: string;
	light_theme: string;
	dark_theme: string;
	workflow_sort_mode: string;
	workflow_sort_order: string[];
	team_workflow_sort_overrides: Record<string, WorkflowSortOverride>;
	recent_due_dates: string[];
}

export interface WorkflowSortOverride {
	mode: string;
	workflow_sort_order?: string[];
}

export function getPreferences(): Promise<PreferencesData> {
	return api.get<PreferencesData>('/api/preferences');
}

export function updatePreferences(data: Partial<PreferencesData>): Promise<PreferencesData> {
	return api.patch<PreferencesData>('/api/preferences', data);
}
