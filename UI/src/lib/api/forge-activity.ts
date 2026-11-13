import type { ForgeIssueActivity } from '$lib/types/forge';

export function normalizeForgeActivity(activity: unknown): ForgeIssueActivity {
	const source = (activity && typeof activity === 'object' ? activity : {}) as Partial<ForgeIssueActivity>;
	return {
		merge_requests: Array.isArray(source.merge_requests) ? source.merge_requests : [],
		branches: Array.isArray(source.branches) ? source.branches : [],
		commits: Array.isArray(source.commits) ? source.commits : []
	};
}
