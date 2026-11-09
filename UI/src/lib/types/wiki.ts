/** A wiki belongs to exactly one team or one project. */
export type WikiScope = { kind: 'team' | 'project'; id: string };

export interface WikiPageSummary {
	id: string;
	team_id: string | null;
	project_id: string | null;
	title: string;
	/** Increments on every save; sent back so stale edits are rejected (409). */
	version: number;
	updated_by: string | null;
	updated_by_name: string | null;
	created_at: string;
	updated_at: string;
	/** Set on registry pages: the day (YYYY-MM-DD) the page logs. */
	registry_date?: string | null;
}

export interface WikiPage extends WikiPageSummary {
	/** Sanitized editor HTML. */
	content: string;
	created_by: string | null;
}

export interface CreateWikiPageRequest {
	title: string;
	content: string;
}

export interface UpdateWikiPageRequest {
	title?: string;
	content?: string;
	version: number;
}

/** One recorded change: consecutive saves by one person are merged. */
export interface WikiRevision {
	id: string;
	version: number;
	old_title: string;
	new_title: string;
	old_content: string;
	new_content: string;
	edited_by: string | null;
	edited_by_name: string | null;
	created_at: string;
	updated_at: string;
}

export interface OverviewSchedule {
	enabled: boolean;
	frequency: 'daily' | 'weekly';
	/** 0 is Sunday. */
	weekday: number;
	/** "HH:MM" in overview_timezone. */
	time: string;
	period_days: number;
}

export interface WikiSettings {
	registry_enabled: boolean;
	overview_schedule?: OverviewSchedule;
	overview_timezone?: string;
	overview_next_run_at?: string | null;
	overview_last_run_at?: string | null;
	overview_last_error?: string | null;
}

/** One day of the registry: the content of that day's page. */
export interface RegistryDay {
	date: string;
	page_id: string;
	title: string;
	content: string;
	version: number;
	updated_at: string;
}

export interface RegistryDays {
	days: RegistryDay[];
	next_before: string | null;
}

/** An AI summary of registry entries for a period. */
export interface RegistryOverview {
	id: string;
	period_start: string;
	period_end: string;
	content: string;
	generated_at: string;
	generated_by_name: string | null;
	trigger: 'manual' | 'schedule';
}

export interface RegistryOverviews {
	overviews: RegistryOverview[];
	next_before: string | null;
}

/** A page's place in the wiki tree. A folder is a page of kind "folder". */
export interface WikiTreeNode {
	id: string;
	parent_id: string | null;
	position: number;
	kind: 'page' | 'folder';
}

export type WikiPublishMode = 'off' | 'internal' | 'public';

/** How a page is published; url is the public link while public. */
export interface WikiPublication {
	mode: WikiPublishMode;
	children: boolean;
	url: string | null;
}

/** A page as a reader sees it: no authors or internal metadata. */
export interface ReaderPage {
	id: string;
	parent_id: string | null;
	title: string;
	content: string;
	position: number;
	updated_at: string;
}

export interface PublicWiki {
	workspace_name: string;
	root_id: string;
	pages: ReaderPage[];
}
