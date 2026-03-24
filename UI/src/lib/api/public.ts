import type { PublicShareMeta, PublicIssue } from '$lib/types/shared-link';
import type { PaginatedResponse } from '$lib/types/common';

async function publicFetch<T>(path: string): Promise<T> {
	const res = await fetch(path, {
		headers: { 'Content-Type': 'application/json' }
	});

	if (!res.ok) {
		const error = await res.json().catch(() => ({
			error: { code: 'UNKNOWN', message: res.statusText }
		}));
		throw error;
	}

	return res.json();
}

export function getShareMeta(token: string): Promise<PublicShareMeta> {
	return publicFetch<PublicShareMeta>(`/api/public/share/${token}`);
}

export function listShareIssues(
	token: string,
	params?: Record<string, string>
): Promise<PaginatedResponse<PublicIssue>> {
	const query = params ? '?' + new URLSearchParams(params).toString() : '';
	return publicFetch<PaginatedResponse<PublicIssue>>(`/api/public/share/${token}/issues${query}`);
}
