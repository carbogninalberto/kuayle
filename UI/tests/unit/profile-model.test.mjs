import { test } from 'node:test';
import assert from 'node:assert/strict';
import { profileSubmission, takesModel } from '../../src/lib/features/workflows/profile-model.ts';

const list = { model_harnesses: ['codex', 'claude-code'] };
const draft = (harness, model, effort) => ({
	name: ' Cheap ',
	role: 'implementer',
	harness,
	model,
	effort,
	instructions: 'Keep it small',
	skill_ids: ['s1']
});

test('the server decides which coding agents take a model and effort', () => {
	assert.equal(takesModel(list, 'codex'), true);
	assert.equal(takesModel(list, 'opencode'), false);
	assert.equal(takesModel({ model_harnesses: ['opencode'] }, 'opencode'), true, 'no agent is special in the UI');
	assert.equal(takesModel({}, 'opencode'), true, 'a server without the field restricts nothing; it validates on save');
});

test('a stale model and effort are not submitted for an agent that takes neither', () => {
	assert.deepEqual(profileSubmission(draft('opencode', 'deepseek-v4.1-flash', 'high'), list), {
		...draft('opencode', '', ''),
		name: 'Cheap'
	});
});

test('an agent that takes a model keeps it, trimmed', () => {
	const sent = profileSubmission(draft('codex', ' gpt-5.5 ', 'high'), list);
	assert.equal(sent.model, 'gpt-5.5');
	assert.equal(sent.effort, 'high');
	assert.equal(sent.name, 'Cheap');
	assert.deepEqual(sent.skill_ids, ['s1']);
});
