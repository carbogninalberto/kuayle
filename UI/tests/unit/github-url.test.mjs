import assert from 'node:assert/strict';
import test from 'node:test';
import {
	githubRepositoryHomeUrl,
	safeGitHubAppInstallUrl,
	safeGitHubBranchUrl,
	safeGitHubCommitUrl,
	safeGitHubPullRequestUrl,
	safeGitHubReleaseUrl,
	safeGitHubRepositoryUrl
} from '../../src/lib/security/github-url.ts';

const repository = 'carbogninalberto/kuayle';

test('accepts only HTTPS GitHub URLs under the expected repository', () => {
	assert.equal(
		safeGitHubRepositoryUrl('https://github.com/carbogninalberto/kuayle/compare/v1...v2', repository),
		'https://github.com/carbogninalberto/kuayle/compare/v1...v2'
	);
	assert.equal(safeGitHubRepositoryUrl('http://github.com/carbogninalberto/kuayle', repository), null);
	assert.equal(safeGitHubRepositoryUrl('https://github.com.example.com/carbogninalberto/kuayle', repository), null);
	assert.equal(safeGitHubRepositoryUrl('https://github.com/attacker/kuayle', repository), null);
	assert.equal(
		safeGitHubRepositoryUrl('https://github.com@attacker.example/carbogninalberto/kuayle', repository),
		null
	);
	assert.equal(githubRepositoryHomeUrl('carbogninalberto/kuayle'), 'https://github.com/carbogninalberto/kuayle');
	assert.equal(githubRepositoryHomeUrl('carbogninalberto/kuayle/extra'), null);
});

test('validates GitHub resource paths', () => {
	assert.equal(
		safeGitHubPullRequestUrl('https://github.com/carbogninalberto/kuayle/pull/46', repository),
		'https://github.com/carbogninalberto/kuayle/pull/46'
	);
	assert.equal(safeGitHubPullRequestUrl('https://github.com/carbogninalberto/kuayle/issues/46', repository), null);
	assert.equal(
		safeGitHubBranchUrl('https://github.com/carbogninalberto/kuayle/tree/feature/link-policy', repository),
		'https://github.com/carbogninalberto/kuayle/tree/feature/link-policy'
	);
	assert.equal(safeGitHubBranchUrl('https://github.com/carbogninalberto/other/tree/main', repository), null);
	assert.equal(
		safeGitHubCommitUrl('https://github.com/carbogninalberto/kuayle/commit/0123456789abcdef', repository),
		'https://github.com/carbogninalberto/kuayle/commit/0123456789abcdef'
	);
	assert.equal(safeGitHubCommitUrl('https://github.com/carbogninalberto/kuayle/commit/not-a-sha', repository), null);
	assert.equal(
		safeGitHubReleaseUrl('https://github.com/carbogninalberto/kuayle/releases/tag/v0.1.12', repository, 'v0.1.12'),
		'https://github.com/carbogninalberto/kuayle/releases/tag/v0.1.12'
	);
	assert.equal(
		safeGitHubReleaseUrl('https://github.com/carbogninalberto/kuayle/releases/tag/v0.1.11', repository, 'v0.1.12'),
		null
	);
});

test('validates GitHub App installation destinations', () => {
	assert.equal(
		safeGitHubAppInstallUrl('https://github.com/apps/kuayle/installations/new?state=workspace-id'),
		'https://github.com/apps/kuayle/installations/new?state=workspace-id'
	);
	assert.equal(safeGitHubAppInstallUrl('https://attacker.example/apps/kuayle/installations/new'), null);
	assert.equal(safeGitHubAppInstallUrl('https://github.com/marketplace/kuayle'), null);
});
