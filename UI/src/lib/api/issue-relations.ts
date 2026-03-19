import { api } from './client';
import type { Issue, IssueRelation } from '$lib/types/issue';

export function listRelations(slug: string, identifier: string): Promise<IssueRelation[]> {
	return api.get<IssueRelation[]>(`/api/workspaces/${slug}/issues/${identifier}/relations`);
}

export function createRelation(
	slug: string,
	identifier: string,
	data: { related_identifier: string; type: string }
): Promise<IssueRelation> {
	return api.post<IssueRelation>(
		`/api/workspaces/${slug}/issues/${identifier}/relations`,
		data
	);
}

export function deleteRelation(
	slug: string,
	identifier: string,
	relationId: string
): Promise<void> {
	return api.delete<void>(
		`/api/workspaces/${slug}/issues/${identifier}/relations/${relationId}`
	);
}

export function listSubIssues(slug: string, identifier: string): Promise<Issue[]> {
	return api.get<Issue[]>(`/api/workspaces/${slug}/issues/${identifier}/sub-issues`);
}
