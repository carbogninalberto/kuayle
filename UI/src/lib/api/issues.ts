import { api } from './client';
import type {
	Issue,
	CreateIssueRequest,
	UpdateIssueRequest,
	Comment,
	IssueHistory,
	IssueUndo,
	BulkIssueUndo
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

export function duplicateIssue(
	slug: string,
	identifier: string,
	includeSubIssues = false
): Promise<Issue> {
	return api.post<Issue>(`/api/workspaces/${slug}/issues/${identifier}/duplicate`, {
		include_sub_issues: includeSubIssues
	});
}

export function convertIssueToProject(
	slug: string,
	identifier: string
): Promise<{ project: { id: string; name: string } }> {
	return api.post<{ project: { id: string; name: string } }>(
		`/api/workspaces/${slug}/issues/${identifier}/convert-to-project`
	);
}

export function updateIssue(
	slug: string,
	identifier: string,
	req: UpdateIssueRequest
): Promise<Issue> {
	return api.patch<Issue>(`/api/workspaces/${slug}/issues/${identifier}`, req);
}

export function deleteIssue(slug: string, identifier: string): Promise<{ status: string; restorable_until: string }> {
	return api.delete<{ status: string; restorable_until: string }>(`/api/workspaces/${slug}/issues/${identifier}`);
}

/** Puts back the values of an edit if the issue was not edited since (409 EDITED_MEANWHILE otherwise). */
export function undoIssue(slug: string, identifier: string, undo: IssueUndo): Promise<Issue> {
	return api.post<Issue>(`/api/workspaces/${slug}/issues/${identifier}/undo`, undo);
}

/** Undoes a bulk edit as one step: every issue or none. */
export function bulkUndoIssues(slug: string, undo: BulkIssueUndo): Promise<{ updated: number }> {
	return api.post<{ updated: number }>(`/api/workspaces/${slug}/issues/bulk/undo`, undo);
}

/** Brings back a deleted issue within the grace period (410 GRACE_EXPIRED after it). */
export function restoreIssue(slug: string, identifier: string): Promise<Issue> {
	return api.post<Issue>(`/api/workspaces/${slug}/issues/${identifier}/restore`, {});
}

export function bulkRestoreIssues(slug: string, req: { issue_ids: string[] }): Promise<{ restored: number }> {
	return api.post<{ restored: number }>(`/api/workspaces/${slug}/issues/bulk/restore`, req);
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

export function subscribeToIssue(slug: string, identifier: string): Promise<{ is_subscribed: boolean }> {
	return api.post<{ is_subscribed: boolean }>(`/api/workspaces/${slug}/issues/${identifier}/subscribe`);
}

export function unsubscribeFromIssue(slug: string, identifier: string): Promise<{ is_subscribed: boolean }> {
	return api.delete<{ is_subscribed: boolean }>(`/api/workspaces/${slug}/issues/${identifier}/subscribe`);
}

export function signIssuePromptAssets(
	slug: string,
	identifier: string
): Promise<{ assets: Record<string, string>; expires_at: string }> {
	return api.post<{ assets: Record<string, string>; expires_at: string }>(
		`/api/workspaces/${slug}/issues/${identifier}/prompt-assets`
	);
}

export function expandIssueDescription(
	slug: string,
	identifier: string,
	req?: { selected_text?: string }
): Promise<{ description: string }> {
	return api.post<{ description: string }>(
		`/api/workspaces/${slug}/issues/${identifier}/expand-description`,
		req
	);
}

/** Expands the description of an issue that has not been created yet. */
export function expandDraftIssueDescription(
	slug: string,
	req: { title: string; description: string; selected_text?: string }
): Promise<{ description: string }> {
	return api.post<{ description: string }>(`/api/workspaces/${slug}/issues/expand-description`, req);
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
		cycle_id?: string;
		parent_id?: string;
		project_id?: string;
	}
): Promise<{ updated: number; undo?: BulkIssueUndo }> {
	return api.patch<{ updated: number; undo?: BulkIssueUndo }>(`/api/workspaces/${slug}/issues/bulk`, req);
}
