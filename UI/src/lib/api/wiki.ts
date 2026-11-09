import { api } from './client';
import type {
	CreateWikiPageRequest,
	UpdateWikiPageRequest,
	WikiPage,
	WikiPageSummary,
	RegistryDays,
	RegistryOverview,
	RegistryOverviews,
	WikiRevision,
	WikiScope,
	WikiSettings,
	WikiTreeNode,
	WikiPublication,
	WikiPublishMode
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

/** Registry days, newest first, a page at a time. */
export function listRegistryDays(
	slug: string,
	scope: WikiScope,
	before?: string | null,
	limit = 7
): Promise<RegistryDays> {
	const query = new URLSearchParams({ limit: String(limit) });
	if (before) query.set('before', before);
	return api.get<RegistryDays>(`${wikiPath(slug, scope)}/registry?${query}`);
}

export function listRegistryOverviews(
	slug: string,
	scope: WikiScope,
	before?: string | null
): Promise<RegistryOverviews> {
	const query = new URLSearchParams({ limit: '10' });
	if (before) query.set('before', before);
	return api.get<RegistryOverviews>(`${wikiPath(slug, scope)}/registry/overviews?${query}`);
}

export function createRegistryOverview(
	slug: string,
	scope: WikiScope,
	period: { period_start: string; period_end: string }
): Promise<RegistryOverview> {
	return api.post<RegistryOverview>(`${wikiPath(slug, scope)}/registry/overviews`, period);
}

export function getWikiSettings(slug: string, scope: WikiScope): Promise<WikiSettings> {
	return api.get<WikiSettings>(`${wikiPath(slug, scope)}/settings`);
}

export function updateWikiSettings(
	slug: string,
	scope: WikiScope,
	settings: Partial<WikiSettings>
): Promise<WikiSettings> {
	return api.patch<WikiSettings>(`${wikiPath(slug, scope)}/settings`, settings);
}

/** Where every page sits; registry days are not part of the tree. */
export function getWikiTree(slug: string, scope: WikiScope): Promise<WikiTreeNode[]> {
	return api.get<WikiTreeNode[]>(`${wikiPath(slug, scope)}/tree`);
}

/** Creates a page or folder, placed last under an optional parent. */
export function createWikiTreePage(
	slug: string,
	scope: WikiScope,
	data: { title: string; content?: string; kind: 'page' | 'folder'; parent_id: string | null }
): Promise<WikiPage> {
	return api.post<WikiPage>(`${wikiPath(slug, scope)}/tree`, data);
}

/** Moves a page under parent_id (null for the top level) at index among its new siblings. */
export function moveWikiPage(
	slug: string,
	scope: WikiScope,
	pageId: string,
	parentId: string | null,
	index: number
): Promise<WikiTreeNode[]> {
	return api.post<WikiTreeNode[]>(`${wikiPath(slug, scope)}/${pageId}/move`, { parent_id: parentId, index });
}

export function getWikiPublication(slug: string, scope: WikiScope, pageId: string): Promise<WikiPublication> {
	return api.get<WikiPublication>(`${wikiPath(slug, scope)}/${pageId}/publish`);
}

/** Publishes a page; rotate replaces a public link, killing the old one. */
export function publishWikiPage(
	slug: string,
	scope: WikiScope,
	pageId: string,
	data: { mode: WikiPublishMode; children: boolean; rotate?: boolean }
): Promise<WikiPublication> {
	return api.put<WikiPublication>(`${wikiPath(slug, scope)}/${pageId}/publish`, data);
}
