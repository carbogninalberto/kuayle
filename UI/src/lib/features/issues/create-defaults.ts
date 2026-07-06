import type { IssuePriority } from '$lib/types/issue';

export type IssueCreateDefaults = {
	teamId?: string;
	statusId?: string;
	priority?: IssuePriority;
	projectId?: string | null;
	assigneeIds?: string[];
	labelIds?: string[];
	dueDate?: string | null;
	cycleId?: string | null;
};

const DEFAULTS_STORAGE_KEY = 'kuayle-issue-create-defaults';

export function getIssueCreateDefaults(slug: string): IssueCreateDefaults {
	try {
		if (typeof localStorage === 'undefined' || !slug) return {};
		const data = JSON.parse(localStorage.getItem(DEFAULTS_STORAGE_KEY) ?? '{}');
		return data[slug] ?? {};
	} catch {
		return {};
	}
}

export function setIssueCreateDefaults(slug: string, defaults: IssueCreateDefaults) {
	try {
		if (typeof localStorage === 'undefined' || !slug) return;
		const data = JSON.parse(localStorage.getItem(DEFAULTS_STORAGE_KEY) ?? '{}');
		data[slug] = defaults;
		localStorage.setItem(DEFAULTS_STORAGE_KEY, JSON.stringify(data));
	} catch {
		// Local defaults are best-effort only.
	}
}

export function clearIssueCreateDefaults(slug: string) {
	try {
		if (typeof localStorage === 'undefined' || !slug) return;
		const data = JSON.parse(localStorage.getItem(DEFAULTS_STORAGE_KEY) ?? '{}');
		delete data[slug];
		localStorage.setItem(DEFAULTS_STORAGE_KEY, JSON.stringify(data));
	} catch {
		// ignore corrupt data
	}
}
