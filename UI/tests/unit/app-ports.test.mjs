import { test } from 'node:test';
import assert from 'node:assert/strict';
import { detectedPorts, portName, taskAppPorts, validAppPort } from '../../src/lib/features/dev-machines/app-ports.ts';

const port = (over) => ({
	id: `port-${over.port}`,
	label: null,
	source: 'user',
	agent_task_id: null,
	task_identifier: null,
	host: `h-${over.port}.example.test`,
	listening: false,
	created_at: '2026-10-28T00:00:00Z',
	...over
});

test('a port is named by its label, or by its number', () => {
	assert.equal(portName(port({ port: 3000, label: 'web' })), 'web');
	assert.equal(portName(port({ port: 3000 })), ':3000');
	assert.equal(portName(port({ port: 3000, label: '' })), ':3000');
});

test('detected ports are listening ports not exposed yet, in order, once each', () => {
	const exposed = [port({ port: 3000 }), port({ port: 8080 })];
	assert.deepEqual(detectedPorts([8080, 6006, 3000, 5173, 6006], exposed), [5173, 6006]);
	assert.deepEqual(detectedPorts([], exposed), []);
});

test('only ports in the app port range can be exposed', () => {
	assert.equal(validAppPort(1024), true);
	assert.equal(validAppPort(65535), true);
	assert.equal(validAppPort(80), false);
	assert.equal(validAppPort(65536), false);
	assert.equal(validAppPort(3000.5), false);
	assert.equal(validAppPort(Number('')), false);
});

test("a task's app ports are its own task and agent ports, the started app first", () => {
	const ports = [
		port({ port: 6006, source: 'agent', agent_task_id: 'task-1' }),
		port({ port: 4100, source: 'task', agent_task_id: 'task-1' }),
		port({ port: 4101, source: 'task', agent_task_id: 'task-2' }),
		port({ port: 3000, source: 'user', agent_task_id: 'task-1' }),
		port({ port: 3001, source: 'agent' })
	];
	assert.deepEqual(
		taskAppPorts(ports, 'task-1').map((p) => p.port),
		[4100, 6006]
	);
	assert.deepEqual(taskAppPorts(ports, 'task-3'), []);
});
