import assert from 'node:assert/strict';
import test from 'node:test';
import {
	DEFAULT_REPOSITORY_CHOICE,
	repositoryChoiceBlocked,
	repositoryCreateInput
} from '../../src/lib/features/dev-machines/repository-choice.ts';

const defaults = {
	repository: { id: 'team-repo', full_name: 'acme/app', base_branch: 'develop', source: 'team', allowed: true },
	inherited_repository: null,
	repositories: [
		{ id: 'team-repo', full_name: 'acme/app', default_branch: 'main' },
		{ id: 'other', full_name: 'acme/other', default_branch: 'trunk' }
	]
};

test('keeping the preselected default sends no repository so the server applies the configured one', () => {
	assert.equal(repositoryCreateInput(DEFAULT_REPOSITORY_CHOICE, defaults), undefined);
	assert.equal(repositoryCreateInput(DEFAULT_REPOSITORY_CHOICE, null), undefined);
});

test('an explicit choice wins and is sent as a GitHub repository', () => {
	assert.deepEqual(repositoryCreateInput('other', defaults), {
		provider: 'github',
		owner: 'acme',
		name: 'other',
		url: 'https://github.com/acme/other'
	});
	assert.equal(repositoryCreateInput('unknown', defaults), undefined, 'only offered repositories can be chosen');
});

test('a default the policy refuses blocks creation until another repository is chosen', () => {
	const refused = { ...defaults, repository: { ...defaults.repository, allowed: false } };
	assert.equal(repositoryChoiceBlocked(DEFAULT_REPOSITORY_CHOICE, refused), true);
	assert.equal(repositoryChoiceBlocked('other', refused), false);
	assert.equal(repositoryChoiceBlocked(DEFAULT_REPOSITORY_CHOICE, defaults), false);
	assert.equal(repositoryChoiceBlocked(DEFAULT_REPOSITORY_CHOICE, null), false);
});
