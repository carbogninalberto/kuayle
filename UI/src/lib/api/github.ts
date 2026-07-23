import { api } from './client';
import type { GitHubStatus, GitHubAvailableRepo, GitHubIssueActivity, GitHubAutoTransition } from '$lib/types/github';
import {
	safeGitHubAppInstallUrl,
	safeGitHubBranchUrl,
	safeGitHubCommitUrl,
	safeGitHubPullRequestUrl
} from '$lib/security/github-url';

export function getGitHubStatus(slug: string): Promise<GitHubStatus> {
	return api.get<GitHubStatus>(`/api/workspaces/${slug}/github/status`);
}

export function getManifestSetup(slug: string): Promise<{ manifest: Record<string, any>; submit_url: string }> {
	return api.post<{ manifest: Record<string, any>; submit_url: string }>(`/api/workspaces/${slug}/github/setup`, {});
}

export function handleManifestCallback(slug: string, code: string): Promise<{ app_id: number; app_slug: string }> {
	return api.get<{ app_id: number; app_slug: string }>(`/api/workspaces/${slug}/github/setup/callback?code=${code}`);
}

export async function getInstallURL(slug: string): Promise<{ url: string }> {
	const result = await api.get<{ url: string }>(`/api/workspaces/${slug}/github/install`);
	return { url: safeGitHubAppInstallUrl(result.url) ?? '' };
}

export function deleteGitHubApp(slug: string): Promise<void> {
	return api.delete<void>(`/api/workspaces/${slug}/github/app`);
}

export function handleGitHubCallback(
	slug: string,
	installationId: number
): Promise<{ id: string; account_login: string }> {
	return api.get<{ id: string; account_login: string }>(
		`/api/workspaces/${slug}/github/callback?installation_id=${installationId}`
	);
}

export function disconnectGitHub(slug: string): Promise<void> {
	return api.delete<void>(`/api/workspaces/${slug}/github/disconnect`);
}

export function listGitHubRepos(slug: string): Promise<GitHubAvailableRepo[]> {
	return api.get<GitHubAvailableRepo[]>(`/api/workspaces/${slug}/github/repos`);
}

export function linkGitHubRepos(slug: string, githubRepoIds: number[]): Promise<void> {
	return api.post<void>(`/api/workspaces/${slug}/github/repos`, { github_repo_ids: githubRepoIds });
}

export function unlinkGitHubRepo(slug: string, id: string): Promise<void> {
	return api.delete<void>(`/api/workspaces/${slug}/github/repos/${id}`);
}

export async function getIssueGitHubActivity(slug: string, identifier: string): Promise<GitHubIssueActivity> {
	const activity = await api.get<GitHubIssueActivity>(`/api/workspaces/${slug}/issues/${identifier}/github`);
	return {
		pull_requests: activity.pull_requests.map((pullRequest) => ({
			...pullRequest,
			html_url: safeGitHubPullRequestUrl(pullRequest.html_url, pullRequest.repo_full_name) ?? ''
		})),
		branches: activity.branches.map((branch) => ({
			...branch,
			html_url: safeGitHubBranchUrl(branch.html_url, branch.repo_full_name) ?? ''
		})),
		commits: activity.commits.map((commit) => ({
			...commit,
			html_url: safeGitHubCommitUrl(commit.html_url, commit.repo_full_name) ?? ''
		}))
	};
}

export function listAutoTransitions(slug: string): Promise<GitHubAutoTransition[]> {
	return api.get<GitHubAutoTransition[]>(`/api/workspaces/${slug}/github/auto-transitions`);
}

export function updateAutoTransitions(slug: string, transitions: GitHubAutoTransition[]): Promise<void> {
	return api.patch<void>(`/api/workspaces/${slug}/github/auto-transitions`, { transitions });
}
