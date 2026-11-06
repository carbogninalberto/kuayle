import { test } from 'node:test';
import assert from 'node:assert/strict';
import { labelAppliesTo, labelsForIssues } from '../../src/lib/features/labels/label-scope.ts';

const workspaceLabel = { id: 'w', project_id: null };
const labelA = { id: 'a', project_id: 'project-a' };
const labelB = { id: 'b', project_id: 'project-b' };

test('a workspace label applies to any issue, a project label only to that project', () => {
	assert.equal(labelAppliesTo(workspaceLabel, null), true);
	assert.equal(labelAppliesTo(workspaceLabel, 'project-a'), true);
	assert.equal(labelAppliesTo(labelA, 'project-a'), true);
	assert.equal(labelAppliesTo(labelA, 'project-b'), false);
	assert.equal(labelAppliesTo(labelA, null), false, 'an issue without a project takes no project label');
	assert.equal(labelAppliesTo(labelA, undefined), false);
});

test('labels offered for a selection apply to every selected issue', () => {
	const labels = [workspaceLabel, labelA, labelB];
	const ids = (list) => list.map((label) => label.id);
	assert.deepEqual(ids(labelsForIssues(labels, [{ project_id: 'project-a' }])), ['w', 'a']);
	assert.deepEqual(ids(labelsForIssues(labels, [{ project_id: 'project-a' }, { project_id: 'project-a' }])), ['w', 'a']);
	assert.deepEqual(ids(labelsForIssues(labels, [{ project_id: 'project-a' }, { project_id: 'project-b' }])), ['w'], 'mixed projects share only workspace labels');
	assert.deepEqual(ids(labelsForIssues(labels, [{ project_id: 'project-a' }, { project_id: null }])), ['w']);
	assert.deepEqual(ids(labelsForIssues(labels, [{}])), ['w'], 'a missing project is no project');
});
