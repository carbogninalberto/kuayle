import type { GroupByField } from './issues.state.svelte';

const STORAGE_PREFIX = 'kuayle-issues-collapsed-groups';

function storageKey(workspaceSlug: string, teamId: string, groupBy: Exclude<GroupByField, null>) {
	return `${STORAGE_PREFIX}:${encodeURIComponent(workspaceSlug)}:${encodeURIComponent(teamId)}:${groupBy}`;
}

export function loadCollapsedGroups(workspaceSlug: string, teamId: string, groupBy: GroupByField): Set<string> {
	if (!groupBy || typeof localStorage === 'undefined') return new Set();
	try {
		const raw = localStorage.getItem(storageKey(workspaceSlug, teamId, groupBy));
		if (!raw) return new Set();
		const values: unknown = JSON.parse(raw);
		return Array.isArray(values) ? new Set(values.filter((value): value is string => typeof value === 'string')) : new Set();
	} catch {
		return new Set();
	}
}

export function saveCollapsedGroups(
	workspaceSlug: string,
	teamId: string,
	groupBy: GroupByField,
	groups: Set<string>
) {
	if (!groupBy || typeof localStorage === 'undefined') return;
	try {
		localStorage.setItem(storageKey(workspaceSlug, teamId, groupBy), JSON.stringify([...groups]));
	} catch {
		// Collapsing still works in memory when storage is unavailable.
	}
}
