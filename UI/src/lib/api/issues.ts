import { api } from './client';
import type {
	Issue,
	CreateIssueRequest,
	UpdateIssueRequest,
	Comment,
	IssueHistory
} from '$lib/types/issue';
import type { PaginatedResponse } from '$lib/types/common';

export function listIssues(
	slug: string,
	params?: Record<string, string>
): Promise<PaginatedResponse<Issue>> {
	const query = params ? '?' + new URLSearchParams(params).toString() : '';
	return api.get<PaginatedResponse<Issue>>(`/api/workspaces/${slug}/issues${query}`);
}

export function getIssue(slug: string, identifier: string): Promise<Issue> {
	return api.get<Issue>(`/api/workspaces/${slug}/issues/${identifier}`);
}

export function createIssue(slug: string, req: CreateIssueRequest): Promise<Issue> {
	return api.post<Issue>(`/api/workspaces/${slug}/issues`, req);
}

export function createSubIssue(
	slug: string,
	identifier: string,
	req: Omit<CreateIssueRequest, 'team_id' | 'parent_id'>
): Promise<Issue> {
	return api.post<Issue>(`/api/workspaces/${slug}/issues/${identifier}/sub-issues`, req);
}

export function bulkCreateSubIssues(
	slug: string,
	identifier: string,
	issues: Array<Omit<CreateIssueRequest, 'team_id' | 'parent_id'>>
): Promise<Issue[]> {
	return api.post<Issue[]>(`/api/workspaces/${slug}/issues/${identifier}/sub-issues/bulk`, { issues });
}

export function updateIssue(
	slug: string,
	identifier: string,
	req: UpdateIssueRequest
): Promise<Issue> {
	return api.patch<Issue>(`/api/workspaces/${slug}/issues/${identifier}`, req);
}

export function deleteIssue(slug: string, identifier: string): Promise<void> {
	return api.delete<void>(`/api/workspaces/${slug}/issues/${identifier}`);
}

export function listComments(slug: string, identifier: string): Promise<Comment[]> {
	return api.get<Comment[]>(`/api/workspaces/${slug}/issues/${identifier}/comments`);
}

export function createComment(slug: string, identifier: string, body: string, parentId?: string): Promise<Comment> {
	return api.post<Comment>(`/api/workspaces/${slug}/issues/${identifier}/comments`, { body, parent_id: parentId });
}

export function resolveComment(slug: string, identifier: string, commentId: string): Promise<void> {
	return api.post<void>(`/api/workspaces/${slug}/issues/${identifier}/comments/${commentId}/resolve`);
}

export function reopenComment(slug: string, identifier: string, commentId: string): Promise<void> {
	return api.post<void>(`/api/workspaces/${slug}/issues/${identifier}/comments/${commentId}/reopen`);
}

export function getIssueHistory(slug: string, identifier: string): Promise<IssueHistory[]> {
	return api.get<IssueHistory[]>(`/api/workspaces/${slug}/issues/${identifier}/history`);
}

export function signIssuePromptAssets(
	slug: string,
	identifier: string
): Promise<{ assets: Record<string, string>; expires_at: string }> {
	return api.post<{ assets: Record<string, string>; expires_at: string }>(
		`/api/workspaces/${slug}/issues/${identifier}/prompt-assets`
	);
}

export function triageAccept(slug: string, identifier: string): Promise<Issue> {
	return api.post<Issue>(`/api/workspaces/${slug}/issues/${identifier}/triage/accept`);
}

export function triageDecline(slug: string, identifier: string): Promise<Issue> {
	return api.post<Issue>(`/api/workspaces/${slug}/issues/${identifier}/triage/decline`);
}

export function bulkDeleteIssues(
	slug: string,
	req: { issue_ids: string[] }
): Promise<{ deleted: number }> {
	return api.deleteWithBody<{ deleted: number }>(`/api/workspaces/${slug}/issues/bulk`, req);
}

export function bulkUpdateIssues(
	slug: string,
	req: {
		issue_ids: string[];
		status?: string;
		status_id?: string;
		priority?: number;
		assignee_id?: string;
		label_ids?: string[];
		parent_id?: string;
	}
): Promise<{ updated: number }> {
	return api.patch<{ updated: number }>(`/api/workspaces/${slug}/issues/bulk`, req);
}
