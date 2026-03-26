import { api } from './client';
import type {
	GitHubStatus,
	GitHubAvailableRepo,
	GitHubIssueActivity,
	GitHubAutoTransition
} from '$lib/types/github';

export function getGitHubStatus(slug: string): Promise<GitHubStatus> {
	return api.get<GitHubStatus>(`/api/workspaces/${slug}/github/status`);
}

export function getInstallURL(slug: string): Promise<{ url: string }> {
	return api.get<{ url: string }>(`/api/workspaces/${slug}/github/install`);
}

export function handleGitHubCallback(slug: string, installationId: number): Promise<{ id: string; account_login: string }> {
	return api.get<{ id: string; account_login: string }>(`/api/workspaces/${slug}/github/callback?installation_id=${installationId}`);
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

export function getIssueGitHubActivity(slug: string, identifier: string): Promise<GitHubIssueActivity> {
	return api.get<GitHubIssueActivity>(`/api/workspaces/${slug}/issues/${identifier}/github`);
}

export function listAutoTransitions(slug: string): Promise<GitHubAutoTransition[]> {
	return api.get<GitHubAutoTransition[]>(`/api/workspaces/${slug}/github/auto-transitions`);
}

export function updateAutoTransitions(slug: string, transitions: GitHubAutoTransition[]): Promise<void> {
	return api.patch<void>(`/api/workspaces/${slug}/github/auto-transitions`, { transitions });
}
