import assert from 'node:assert/strict';
import test from 'node:test';
import { commandMachineSize, commandRunPending } from '../../src/lib/features/assistant/command-machine.ts';

test('names the temporary preset as 1 vCPU, 2 GB memory and 20 GB disk', () => {
	assert.deepEqual(commandMachineSize({ cpu_millis: 1000, memory_mb: 2048, disk_gb: 20 }), {
		cpu: '1',
		memory: '2',
		disk: '20'
	});
	assert.deepEqual(commandMachineSize({ cpu_millis: 1500, memory_mb: 3072, disk_gb: 40 }), {
		cpu: '1.5',
		memory: '3',
		disk: '40'
	});
});

test('polls a run only until it finishes', () => {
	assert.equal(commandRunPending(undefined), true, 'not queued yet');
	assert.equal(commandRunPending('queued'), true);
	assert.equal(commandRunPending('running'), true);
	assert.equal(commandRunPending('succeeded'), false);
	assert.equal(commandRunPending('failed'), false);
});
