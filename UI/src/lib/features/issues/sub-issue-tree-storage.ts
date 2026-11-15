// Remembers in localStorage whether the issue list nests sub-issues, and which parents are expanded.
// Like collapsed-groups.ts it never throws: unavailable storage falls back to the defaults.

const NEST_KEY = 'kuayle-issues-nest-sub-issues';
const EXPANDED_PREFIX = 'kuayle-issues-expanded-parents';

export const MAX_EXPANDED_PARENTS = 500;

function expandedKey(workspaceSlug: string) {
	return `${EXPANDED_PREFIX}:${encodeURIComponent(workspaceSlug)}`;
}

export function loadNestSubIssues(): boolean {
	if (typeof localStorage === 'undefined') return false;
	try {
		const raw = localStorage.getItem(NEST_KEY);
		if (!raw) return false;
		const value: unknown = JSON.parse(raw);
		return value === true;
	} catch {
		return false;
	}
}

export function saveNestSubIssues(nest: boolean): void {
	if (typeof localStorage === 'undefined') return;
	try {
		localStorage.setItem(NEST_KEY, JSON.stringify(nest));
	} catch {
		// Nesting still works in memory when storage is unavailable.
	}
}

export function loadExpandedParents(workspaceSlug: string): Set<string> {
	if (typeof localStorage === 'undefined') return new Set();
	try {
		const raw = localStorage.getItem(expandedKey(workspaceSlug));
		if (!raw) return new Set();
		const values: unknown = JSON.parse(raw);
		return Array.isArray(values) ? new Set(values.filter((value): value is string => typeof value === 'string')) : new Set();
	} catch {
		return new Set();
	}
}

export function saveExpandedParents(workspaceSlug: string, ids: Iterable<string>): void {
	if (typeof localStorage === 'undefined') return;
	try {
		const all = [...ids];
		const kept = all.slice(Math.max(0, all.length - MAX_EXPANDED_PARENTS));
		localStorage.setItem(expandedKey(workspaceSlug), JSON.stringify(kept));
	} catch {
		// Expansion still works in memory when storage is unavailable.
	}
}
