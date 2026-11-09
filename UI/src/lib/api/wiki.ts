import { api } from './client';
import type {
	CreateWikiPageRequest,
	UpdateWikiPageRequest,
	WikiPage,
	WikiPageSummary,
	WikiRevision,
	WikiScope,
	WikiSettings
} from '$lib/types/wiki';

function wikiPath(slug: string, scope: WikiScope): string {
	const base = scope.kind === 'team' ? 'teams' : 'projects';
	return `/api/workspaces/${slug}/${base}/${scope.id}/wiki`;
}

export function listWikiPages(slug: string, scope: WikiScope): Promise<WikiPageSummary[]> {
	return api.get<WikiPageSummary[]>(wikiPath(slug, scope));
}

export function getWikiPage(slug: string, scope: WikiScope, pageId: string): Promise<WikiPage> {
	return api.get<WikiPage>(`${wikiPath(slug, scope)}/${pageId}`);
}

export function createWikiPage(slug: string, scope: WikiScope, data: CreateWikiPageRequest): Promise<WikiPage> {
	return api.post<WikiPage>(wikiPath(slug, scope), data);
}

export function updateWikiPage(
	slug: string,
	scope: WikiScope,
	pageId: string,
	data: UpdateWikiPageRequest
): Promise<WikiPage> {
	return api.patch<WikiPage>(`${wikiPath(slug, scope)}/${pageId}`, data);
}

export function deleteWikiPage(slug: string, scope: WikiScope, pageId: string): Promise<void> {
	return api.delete<void>(`${wikiPath(slug, scope)}/${pageId}`);
}

/** Recorded changes of a page, newest first. */
export function listWikiPageVersions(slug: string, scope: WikiScope, pageId: string): Promise<WikiRevision[]> {
	return api.get<WikiRevision[]>(`${wikiPath(slug, scope)}/${pageId}/versions`);
}

/** Rewrites selected text of a saved page with the workspace's AI settings. */
export function reworkWikiSelection(
	slug: string,
	scope: WikiScope,
	pageId: string,
	selectedText: string
): Promise<{ description: string }> {
	return api.post<{ description: string }>(`${wikiPath(slug, scope)}/${pageId}/rework`, {
		selected_text: selectedText
	});
}

export function getWikiSettings(slug: string, scope: WikiScope): Promise<WikiSettings> {
	return api.get<WikiSettings>(`${wikiPath(slug, scope)}/settings`);
}

export function updateWikiSettings(slug: string, scope: WikiScope, settings: WikiSettings): Promise<WikiSettings> {
	return api.patch<WikiSettings>(`${wikiPath(slug, scope)}/settings`, settings);
}
