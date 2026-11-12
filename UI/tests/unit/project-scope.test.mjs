import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
	commonTeamId,
	flattenProjectGroups,
	groupProjectsByTeam,
	projectsForIssues
} from '../../src/lib/features/projects/project-scope.ts';

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

const named = [
	{ id: 'open', name: 'Open' },
	{ id: 'other-open', name: 'Other open' },
	{ id: 'secret', name: 'Secret' }
];

test('grouping puts the issue team first, then other teams that have projects, then workspace ones', () => {
	const groups = groupProjectsByTeam(projects, named, 'open');
	assert.deepEqual(ids(groups.own), ['open']);
	assert.deepEqual(
		groups.teams.map((group) => [group.team.id, ids(group.projects)]),
		[
			['other-open', ['other-open']],
			['secret', ['secret']]
		]
	);
	assert.deepEqual(ids(groups.workspace), ['workspace']);
	assert.deepEqual(ids(flattenProjectGroups(groups)), ['open', 'other-open', 'secret', 'workspace']);
});

test('grouping without a single team has no own group and lists every team', () => {
	const groups = groupProjectsByTeam(projects, named, null);
	assert.deepEqual(groups.own, []);
	assert.deepEqual(groups.teams.map((group) => group.team.id), ['open', 'other-open', 'secret']);
});

test('grouping drops teams without projects and projects of unknown teams', () => {
	const groups = groupProjectsByTeam([openProject, otherSecretProject], named, null);
	assert.deepEqual(groups.teams.map((group) => group.team.id), ['open']);
	assert.deepEqual(groups.workspace, []);
});

test('the common team is the shared one of a selection, else none', () => {
	assert.equal(commonTeamId([{ team_id: 'a' }, { team_id: 'a' }]), 'a');
	assert.equal(commonTeamId([{ team_id: 'a' }, { team_id: 'b' }]), null);
	assert.equal(commonTeamId([]), null);
});
