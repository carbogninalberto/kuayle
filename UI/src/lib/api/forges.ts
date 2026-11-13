import { api } from './client';
import type {
	ForgeConnection,
	ForgeConnectionInput,
	ForgeConnectionUpdate,
	ForgeIssueActivity,
	ForgeRepo,
	ForgeRepoInput
} from '$lib/types/forge';
import { safeForgeUrl } from '$lib/security/forge-url';
import { normalizeForgeActivity } from './forge-activity';

export function listForgeConnections(slug: string): Promise<ForgeConnection[]> {
	return api.get<ForgeConnection[]>(`/api/workspaces/${slug}/forges/connections`);
}

export function createForgeConnection(
	slug: string,
	input: ForgeConnectionInput
): Promise<ForgeConnection> {
	return api.post<ForgeConnection>(`/api/workspaces/${slug}/forges/connections`, input);
}

export function updateForgeConnection(
	slug: string,
	id: string,
	update: ForgeConnectionUpdate
): Promise<ForgeConnection> {
	return api.patch<ForgeConnection>(`/api/workspaces/${slug}/forges/connections/${id}`, update);
}

export function deleteForgeConnection(slug: string, id: string): Promise<void> {
	return api.delete<void>(`/api/workspaces/${slug}/forges/connections/${id}`);
}

export function listForgeRepos(slug: string, connectionId: string): Promise<ForgeRepo[]> {
	return api.get<ForgeRepo[]>(
		`/api/workspaces/${slug}/forges/connections/${connectionId}/repos`
	);
}

export function linkForgeRepo(
	slug: string,
	connectionId: string,
	input: ForgeRepoInput
): Promise<ForgeRepo> {
	return api.post<ForgeRepo>(
		`/api/workspaces/${slug}/forges/connections/${connectionId}/repos`,
		input
	);
}

export function unlinkForgeRepo(
	slug: string,
	connectionId: string,
	repoId: string
): Promise<void> {
	return api.delete<void>(
		`/api/workspaces/${slug}/forges/connections/${connectionId}/repos/${repoId}`
	);
}

export async function getIssueForgeActivity(
	slug: string,
	identifier: string
): Promise<ForgeIssueActivity> {
	const activity = normalizeForgeActivity(
		await api.get<ForgeIssueActivity>(`/api/workspaces/${slug}/issues/${identifier}/forges`)
	);
	return {
		merge_requests: activity.merge_requests.map((mr) => ({
			...mr,
			web_url: safeForgeUrl(mr.web_url) ?? ''
		})),
		branches: activity.branches.map((branch) => ({
			...branch,
			web_url: safeForgeUrl(branch.web_url) ?? ''
		})),
		commits: activity.commits.map((commit) => ({
			...commit,
			web_url: safeForgeUrl(commit.web_url) ?? ''
		}))
	};
}
