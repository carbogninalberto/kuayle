import type { MergedPullRequest } from '$lib/api/agent-tasks';

// KUA-193: a retry, a follow-up or "send and start now" on an issue whose
// work was already merged is refused with 409 MERGED_WORK, naming the pull
// request, until it is sent again confirmed.

/** The merged pull request a refusal names, or null for any other error. */
export function mergedWork(error: unknown): MergedPullRequest | null {
	if (!error || typeof error !== 'object') return null;
	const { status, error: body } = error as {
		status?: unknown;
		error?: { code?: unknown; pull_request?: Partial<MergedPullRequest> | null };
	};
	const pr = body?.pull_request;
	if (status !== 409 || body?.code !== 'MERGED_WORK' || !pr || typeof pr !== 'object') return null;
	if (typeof pr.number !== 'number' || !Number.isInteger(pr.number) || pr.number < 1) return null;
	return {
		number: pr.number,
		url: safePullRequestUrl(pr.url),
		repository: typeof pr.repository === 'string' ? pr.repository : ''
	};
}

/** The address when it is a web link (http or https), otherwise "" so nothing is linked. */
export function safePullRequestUrl(value: unknown): string {
	if (typeof value !== 'string') return '';
	try {
		const url = new URL(value.trim());
		return url.protocol === 'https:' || url.protocol === 'http:' ? url.href : '';
	} catch {
		return '';
	}
}
