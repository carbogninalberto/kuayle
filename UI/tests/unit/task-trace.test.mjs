import assert from 'node:assert/strict';
import test from 'node:test';
import { taskTraceFilename } from '../../src/lib/features/assistant/task-trace-name.ts';

test('the trace download names the file after the task identifier', () => {
	assert.equal(taskTraceFilename('KUA-138', 'task-id'), 'kuayle-trace-KUA-138.json');
	assert.equal(taskTraceFilename('', 'task-id'), 'kuayle-trace-task-id.json');
	assert.equal(taskTraceFilename(null, 'task-id'), 'kuayle-trace-task-id.json');
	assert.equal(taskTraceFilename('A/B 1', 'task-id'), 'kuayle-trace-A_B_1.json');
});
