import { test } from 'node:test';
import assert from 'node:assert/strict';
import { projectsForIssues } from '../../src/lib/features/projects/project-scope.ts';

const teams = [
	{ id: 'open', is_private: false },
	{ id: 'other-open', is_private: false },
	{ id: 'secret', is_private: true },
	{ id: 'other-secret', is_private: true }
];
const workspaceProject = { id: 'workspace', team_id: null };
const openProject = { id: 'open', team_id: 'open' };
const otherOpenProject = { id: 'other-open', team_id: 'other-open' };
const secretProject = { id: 'secret', team_id: 'secret' };
const otherSecretProject = { id: 'other-secret', team_id: 'other-secret' };
const projects = [workspaceProject, openProject, otherOpenProject, secretProject, otherSecretProject];
const ids = (list) => list.map((project) => project.id);

test('a public-team issue takes workspace and public-team projects, not private-team ones', () => {
	assert.deepEqual(ids(projectsForIssues(projects, [{ team_id: 'open' }], teams)), ['workspace', 'open', 'other-open']);
});

test('a private-team issue takes only projects of its own team', () => {
	assert.deepEqual(ids(projectsForIssues(projects, [{ team_id: 'secret' }], teams)), ['secret']);
});

test('a selection takes the projects every selected issue can join', () => {
	assert.deepEqual(
		ids(projectsForIssues(projects, [{ team_id: 'open' }, { team_id: 'other-open' }], teams)),
		['workspace', 'open', 'other-open']
	);
	assert.deepEqual(ids(projectsForIssues(projects, [{ team_id: 'open' }, { team_id: 'secret' }], teams)), []);
});

test('a team whose privacy is unknown is treated as private', () => {
	assert.deepEqual(ids(projectsForIssues(projects, [{ team_id: 'unknown' }], teams)), []);
	assert.deepEqual(ids(projectsForIssues([{ id: 'x', team_id: 'unknown' }], [{ team_id: 'open' }], teams)), []);
});
