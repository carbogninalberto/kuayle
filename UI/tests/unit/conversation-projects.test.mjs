import assert from 'node:assert/strict';
import test from 'node:test';
import {
	listQuery,
	matchesProjectFilter,
	newChatProject,
	resolveProjectFilter
} from '../../src/lib/features/assistant/project-filter.ts';

const projects = [
	{ id: 'p-1', name: 'Launch' },
	{ id: 'p-2', name: 'Company' }
];
const chat = (project) => ({ id: 'c', project: project ? { id: project, name: project } : null });

test('lists every conversation unless a project or "no project" is selected', () => {
	assert.equal(listQuery('all'), undefined);
	assert.equal(listQuery('none'), 'none');
	assert.equal(listQuery('p-1'), 'p-1');
});

test('a new chat inherits only a specific project filter', () => {
	assert.equal(newChatProject('all'), null);
	assert.equal(newChatProject('none'), null);
	assert.equal(newChatProject('p-2'), 'p-2');
});

test('a remembered project the user can no longer see falls back to all projects', () => {
	assert.equal(resolveProjectFilter('p-1', projects), 'p-1');
	assert.equal(resolveProjectFilter('none', projects), 'none');
	assert.equal(resolveProjectFilter('gone', projects), 'all');
	assert.equal(resolveProjectFilter(null, projects), 'all');
	assert.equal(resolveProjectFilter(42, projects), 'all');
});

test('a conversation moved out of the filtered project leaves the filtered list', () => {
	assert.equal(matchesProjectFilter(chat('p-1'), 'all'), true);
	assert.equal(matchesProjectFilter(chat(null), 'all'), true);
	assert.equal(matchesProjectFilter(chat('p-1'), 'p-1'), true);
	assert.equal(matchesProjectFilter(chat('p-2'), 'p-1'), false);
	assert.equal(matchesProjectFilter(chat(null), 'none'), true);
	assert.equal(matchesProjectFilter(chat('p-1'), 'none'), false);
});
