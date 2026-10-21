import type { Issue, UpdateIssueRequest } from '$lib/types/issue';

export type IssueScope = { kind: 'cycle' | 'project'; id: string; teamId?: string };

export function scopedIssueParams(
	scope: IssueScope,
	filters: Record<string, string | undefined> = {},
	groupBy?: string | null
) {
	const params: Record<string, string> = { sort: 'sort_order', order: 'asc' };
	for (const [key, value] of Object.entries(filters)) if (value) params[key] = value;
	params[scope.kind] = scope.id;
	if (scope.teamId) params.team = scope.teamId;
	if (groupBy) params.group_by = groupBy;
	else delete params.group_by;
	return params;
}

export function belongsToScope(issue: Issue, scope: IssueScope) {
	return issue[scope.kind === 'cycle' ? 'cycle_id' : 'project_id'] === scope.id;
}

// Use the same PATCH contract for cycles and projects. Empty string clears membership.
// Bounded concurrency avoids flooding the API; every result is accounted for on partial failure.
export async function updateIssueMembership(
	issues: Issue[],
	scope: IssueScope,
	remove: boolean,
	update: (identifier: string, patch: UpdateIssueRequest) => Promise<unknown>
): Promise<{ succeeded: Issue[]; failed: Issue[] }> {
	const unique = [...new Map(issues.map((issue) => [issue.id, issue])).values()];
	const succeeded: Issue[] = [];
	const failed: Issue[] = [];
	let next = 0;
	const patch = { [scope.kind === 'cycle' ? 'cycle_id' : 'project_id']: remove ? '' : scope.id };
	await Promise.all(
		Array.from({ length: Math.min(4, unique.length) }, async () => {
			while (next < unique.length) {
				const issue = unique[next++];
				try {
					await update(issue.identifier, patch);
					succeeded.push(issue);
				} catch {
					failed.push(issue);
				}
			}
		})
	);
	return { succeeded, failed };
}
