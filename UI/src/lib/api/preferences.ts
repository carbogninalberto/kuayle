import { api } from './client';
import type { NotificationEvents, NotificationSound } from '$lib/features/notifications/notification-policy';

export type IssuesGroupByPreference = 'status' | 'priority' | 'assignee' | 'project' | 'none';

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
	issues_group_by: IssuesGroupByPreference;
	browser_notifications: boolean;
	/** Absent on older servers: the workflow editor panel defaults to open. */
	workflow_editor_panel_open?: boolean;
	notification_sound: NotificationSound;
	/** Responses carry every event key; a PATCH may send a partial map, which the server merges. */
	notification_events: Partial<NotificationEvents>;
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
