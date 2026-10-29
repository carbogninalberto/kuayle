import assert from 'node:assert/strict';
import test from 'node:test';
import {
	canResizeMachine,
	pendingResize,
	resizeSizeOptions
} from '../../src/lib/features/dev-machines/machine-resize.ts';

const policyOptions = {
	cpu_millis: [1000, 2000, 4000],
	memory_mb: [2048, 4096, 8192],
	disk_gb: [20, 30, 40, 50, 75, 100]
};
const current = { cpu_millis: 2000, memory_mb: 4096, disk_gb: 40 };

test('offers the policy options and keeps the current disk selectable', () => {
	const options = resizeSizeOptions(policyOptions, 50, current, true);
	assert.deepEqual(options.cpu_millis, [1000, 2000, 4000]);
	assert.deepEqual(options.disk_gb, [20, 30, 40, 50]);
	// A machine created before the policy was lowered keeps its larger disk.
	assert.deepEqual(resizeSizeOptions(policyOptions, 30, current, true).disk_gb, [20, 30, 40]);
});

test('fixes the disk when the server enforces hard quotas', () => {
	assert.deepEqual(resizeSizeOptions(policyOptions, 100, current, false).disk_gb, [40]);
});

test('resizes only settled running, paused or stopped machines without a pending size', () => {
	const machine = { status: 'running', desired_status: 'running' };
	assert.equal(canResizeMachine(machine), true);
	assert.equal(canResizeMachine({ status: 'stopped', desired_status: 'stopped' }), true);
	assert.equal(canResizeMachine({ status: 'paused', desired_status: 'paused' }), true);
	assert.equal(canResizeMachine({ status: 'running', desired_status: 'stopped' }), false);
	assert.equal(canResizeMachine({ status: 'failed', desired_status: 'failed' }), false);
	assert.equal(
		canResizeMachine({ ...machine, pending_cpu_millis: 4000, pending_memory_mb: 8192, pending_disk_gb: 40 }),
		false
	);
	assert.equal(canResizeMachine({ ...machine, delete_requested_at: '2026-10-29T00:00:00Z' }), false);
});

test('reads the pending size only when all resources are set', () => {
	assert.equal(pendingResize({}), null);
	assert.deepEqual(pendingResize({ pending_cpu_millis: 4000, pending_memory_mb: 8192, pending_disk_gb: 40 }), {
		cpu_millis: 4000,
		memory_mb: 8192,
		disk_gb: 40
	});
});
