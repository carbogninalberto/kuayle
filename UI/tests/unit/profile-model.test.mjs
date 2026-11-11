import { test } from 'node:test';
import assert from 'node:assert/strict';
import { modelFormat, profileSubmission, takesModel } from '../../src/lib/features/workflows/profile-model.ts';

const list = {
	model_harnesses: ['codex', 'claude-code', 'opencode'],
	model_formats: { opencode: { format: 'provider/model', example: 'opencode-go/deepseek-v4.1-flash' } }
};
const draft = (harness, model, effort) => ({
	name: ' Cheap ',
	role: ' Staff engineer ',
	behaviour: 'plans',
	harness,
	model,
	effort,
	instructions: 'Keep it small',
	skill_ids: ['s1']
});

test('the server decides which coding agents take a model and effort', () => {
	assert.equal(takesModel(list, 'codex'), true);
	assert.equal(takesModel(list, 'opencode'), true);
	assert.equal(takesModel(list, 'qwen-code'), false);
	assert.equal(takesModel({ model_harnesses: ['qwen-code'] }, 'qwen-code'), true, 'no agent is special in the UI');
	assert.equal(takesModel({}, 'qwen-code'), true, 'a server without the field restricts nothing; it validates on save');
});

test('a stale model and effort are not submitted for an agent that takes neither', () => {
	assert.deepEqual(profileSubmission(draft('qwen-code', 'deepseek-v4.1-flash', 'high'), list), {
		...draft('qwen-code', '', ''),
		name: 'Cheap',
		role: 'Staff engineer'
	});
});

test('an agent that takes a model keeps it, trimmed', () => {
	const sent = profileSubmission(draft('codex', ' gpt-5.5 ', 'high'), list);
	assert.equal(sent.model, 'gpt-5.5');
	assert.equal(sent.effort, 'high');
	assert.equal(sent.name, 'Cheap');
	assert.equal(sent.role, 'Staff engineer', 'the role is a free label, sent trimmed');
	assert.equal(sent.behaviour, 'plans');
	assert.deepEqual(sent.skill_ids, ['s1']);
});

test('an agent with a model name form keeps its model and effort and says the form', () => {
	const sent = profileSubmission(draft('opencode', ' opencode-go/deepseek-v4.1-flash ', 'high'), list);
	assert.equal(sent.model, 'opencode-go/deepseek-v4.1-flash');
	assert.equal(sent.effort, 'high');
	assert.deepEqual(modelFormat(list, 'opencode'), {
		format: 'provider/model',
		example: 'opencode-go/deepseek-v4.1-flash'
	});
});

test('the server decides which agents have a model name form', () => {
	assert.equal(modelFormat(list, 'codex'), undefined);
	assert.equal(modelFormat({}, 'opencode'), undefined, 'an older server sends none: the plain placeholder stays');
	assert.equal(
		modelFormat({ model_harnesses: ['codex'], model_formats: list.model_formats }, 'opencode'),
		undefined,
		'no form is shown for a field that is off'
	);
	assert.deepEqual(
		modelFormat({ model_formats: { codex: { format: 'a:b', example: 'x:y' } } }, 'codex'),
		{ format: 'a:b', example: 'x:y' },
		'no agent is special in the UI'
	);
});
