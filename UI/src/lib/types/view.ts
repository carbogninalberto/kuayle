export interface ViewFilter {
	[key: string]: string | undefined;
	status?: string;
	priority?: string;
	assignee?: string;
	creator?: string;
	team?: string;
	project?: string;
	label?: string;
	search?: string;
	due_before?: string;
	due_after?: string;
	group_by?: string;
	sort?: string;
	order?: string;
}

export type ViewLayout = 'list' | 'board';

export interface View {
	id: string;
	workspace_id: string;
	creator_id: string;
	name: string;
	description: string | null;
	filters: ViewFilter;
	is_shared: boolean;
	created_at: string;
	updated_at: string;
}

export interface CreateViewRequest {
	name: string;
	description?: string;
	filters: ViewFilter;
	is_shared?: boolean;
}

export interface UpdateViewRequest {
	name?: string;
	description?: string;
	filters?: ViewFilter;
	is_shared?: boolean;
}
