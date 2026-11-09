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
