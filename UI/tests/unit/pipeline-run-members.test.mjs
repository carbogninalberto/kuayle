import assert from 'node:assert/strict';
import test from 'node:test';
import { pipelineRunMembers } from '../../src/lib/features/workflows/pipeline-run-members.ts';

const pipeline = {
	kind: 'pipeline',
	roster: [{ role: 'writer' }, { role: 'reviewer' }, { role: 'specialist', on_call: true }]
};

test('the roster includes idle roles and marks only members with a running turn', () => {
	const members = pipelineRunMembers(pipeline, [
		{ role: 'writer', status: 'succeeded' },
		{ role: 'reviewer', status: 'running' },
		{ role: 'specialist', status: 'queued' },
		{ role: null, status: 'running' }
	]);
	assert.deepEqual(members, [
		{ role: 'writer', working: false },
		{ role: 'reviewer', working: true },
		{ role: 'specialist', working: false }
	]);
});

test('the configured lead and writer can work at the same time', () => {
	const led = { ...pipeline, lead: { profile_id: 'lead-profile' } };
	assert.deepEqual(pipelineRunMembers(led, [
		{ role: 'lead', status: 'running' },
		{ role: 'writer', status: 'running' }
	]).filter((member) => member.working).map((member) => member.role), ['lead', 'writer']);
	assert.equal(pipelineRunMembers(led, [])[0].working, false);
});
