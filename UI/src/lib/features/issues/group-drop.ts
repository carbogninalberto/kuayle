// Shared by the grouped issue lists: what dropping an issue on a group changes.
// Pure on purpose, so it is unit tested without a browser.

export type DropGroupBy = 'status' | 'priority' | 'assignee' | 'project' | null | undefined;

export interface DropIssue {
	team_id: string;
	status_id?: string | null;
	status?: string;
	priority: number;
	assignee_id?: string | null;
	project_id?: string | null;
}

export interface DropStatus {
	id: string;
	team_id: string;
	name: string;
	category: string;
	position: number;
}

export type GroupDropPatch = Partial<{
	status_id: string;
	priority: number;
	assignee_id: string | null;
	project_id: string | null;
}>;

export type GroupDropResult = { kind: 'noop' } | { kind: 'invalid' } | { kind: 'update'; patch: GroupDropPatch };

export const GROUP_DROP_FIELDS = {
	status: 'status_id',
	priority: 'priority',
	assignee: 'assignee_id',
	project: 'project_id'
} as const;

export function issueGroupKey(issue: DropIssue, groupBy: DropGroupBy): string {
	switch (groupBy) {
		case 'status':
			return issue.status_id ?? issue.status ?? '';
		case 'priority':
			return String(issue.priority);
		case 'assignee':
			return issue.assignee_id ?? 'unassigned';
		case 'project':
			return issue.project_id ?? 'no-project';
		default:
			return 'all';
	}
}

/**
 * Statuses belong to a team, but a list can mix teams. Returns the status of the
 * issue's own team that the dropped-on status stands for: the same status, else
 * the same name, else the first of the same category. Null when there is none.
 */
export function statusForIssueTeam(issue: DropIssue, targetStatusId: string, statuses: DropStatus[]): string | null {
	const target = statuses.find((status) => status.id === targetStatusId);
	if (!target) return null;
	if (target.team_id === issue.team_id) return target.id;
	const own = statuses.filter((status) => status.team_id === issue.team_id);
	const name = target.name.trim().toLowerCase();
	const byName = own.find((status) => status.name.trim().toLowerCase() === name);
	if (byName) return byName.id;
	const byCategory = own
		.filter((status) => status.category === target.category)
		.sort((a, b) => a.position - b.position)[0];
	return byCategory?.id ?? null;
}

export function resolveGroupDrop(
	issue: DropIssue,
	groupBy: DropGroupBy,
	groupKey: string,
	statuses: DropStatus[]
): GroupDropResult {
	if (!groupBy || !(groupBy in GROUP_DROP_FIELDS)) return { kind: 'noop' };
	if (groupBy === 'status') {
		const statusId = statusForIssueTeam(issue, groupKey, statuses);
		if (!statusId) return { kind: 'invalid' };
		if (statusId === issue.status_id) return { kind: 'noop' };
		return { kind: 'update', patch: { status_id: statusId } };
	}
	if (issueGroupKey(issue, groupBy) === groupKey) return { kind: 'noop' };
	if (groupBy === 'priority') {
		const priority = Number(groupKey);
		return Number.isNaN(priority) ? { kind: 'invalid' } : { kind: 'update', patch: { priority } };
	}
	if (groupBy === 'assignee')
		return { kind: 'update', patch: { assignee_id: groupKey === 'unassigned' ? null : groupKey } };
	return { kind: 'update', patch: { project_id: groupKey === 'no-project' ? null : groupKey } };
}
