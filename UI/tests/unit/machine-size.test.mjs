import assert from 'node:assert/strict';
import test from 'node:test';
import {
	DEFAULT_MACHINE_RESOURCES,
	MACHINE_CPU_MILLIS,
	MACHINE_DISK_GB,
	MACHINE_MEMORY_MB,
	clampToOption,
	formatMachineResource,
	memoryGb,
	minimumMemoryIndexForCpu,
	normalizeMachineResources,
	resolveMachineSizeOptions
} from '../../src/lib/features/dev-machines/machine-size.ts';

test('uses the shared snap lists and filters disk to the workspace policy', () => {
	const options = resolveMachineSizeOptions(undefined, 75);

	assert.deepEqual(options.cpu_millis, [...MACHINE_CPU_MILLIS]);
	assert.deepEqual(options.memory_mb, [...MACHINE_MEMORY_MB]);
	assert.deepEqual(options.disk_gb, [20, 30, 40, 50, 75]);
	assert.deepEqual(resolveMachineSizeOptions(undefined, 10).disk_gb, []);
	assert.deepEqual(DEFAULT_MACHINE_RESOURCES, {
		cpu_millis: 4000,
		memory_mb: 8192,
		disk_gb: 50
	});
});

test('prefers policy options and keeps defaults on valid snapped positions', () => {
	const options = resolveMachineSizeOptions(
		{
			cpu_millis: [2000, 4000, 8000],
			memory_mb: [4096, 8192, 16384],
			disk_gb: [20, 50, 100, 150]
		},
		75
	);

	assert.deepEqual(options, {
		cpu_millis: [2000, 4000, 8000],
		memory_mb: [4096, 8192, 16384],
		disk_gb: [20, 50]
	});
	assert.deepEqual(normalizeMachineResources(DEFAULT_MACHINE_RESOURCES, options), DEFAULT_MACHINE_RESOURCES);
	assert.deepEqual(normalizeMachineResources({ cpu_millis: 3000, memory_mb: 1000, disk_gb: 80 }, options), {
		cpu_millis: 2000,
		memory_mb: 4096,
		disk_gb: 50
	});
	assert.deepEqual(normalizeMachineResources(DEFAULT_MACHINE_RESOURCES, resolveMachineSizeOptions(undefined, 40)), {
		cpu_millis: 4000,
		memory_mb: 8192,
		disk_gb: 40
	});
	assert.equal(clampToOption(999, []), 999);
});

test('preserves an explicit empty disk option list', () => {
	const options = resolveMachineSizeOptions(
		{
			cpu_millis: [2000, 4000],
			memory_mb: [4096, 8192],
			disk_gb: []
		},
		100
	);

	assert.deepEqual(options.disk_gb, []);
});

test('raises memory to the smallest option that supports at least 1 GB per vCPU', () => {
	const options = resolveMachineSizeOptions(undefined, 100);

	assert.equal(minimumMemoryIndexForCpu(4000, options.memory_mb), 1);
	assert.equal(minimumMemoryIndexForCpu(6000, options.memory_mb), 2);
	assert.equal(minimumMemoryIndexForCpu(12000, options.memory_mb), 3);
	assert.deepEqual(normalizeMachineResources({ cpu_millis: 12000, memory_mb: 8192, disk_gb: 50 }, options), {
		cpu_millis: 12000,
		memory_mb: 12288,
		disk_gb: 50
	});
	assert.deepEqual(normalizeMachineResources({ cpu_millis: 6000, memory_mb: 4096, disk_gb: 50 }, options), {
		cpu_millis: 6000,
		memory_mb: 8192,
		disk_gb: 50
	});
});

test('formats resource values with their displayed units', () => {
	assert.equal(formatMachineResource(12000 / 1000, 'vCPU'), '12 vCPU');
	assert.equal(formatMachineResource(memoryGb(12288), 'GB'), '12 GB');
	assert.equal(formatMachineResource(75, 'GB'), '75 GB');
});

test('converts memory from MiB to GiB for display', () => {
	assert.equal(memoryGb(8192), 8);
	assert.equal(memoryGb(12288), 12);
	assert.equal(memoryGb(65536), 64);
});

test('the snap lists match the API contract', () => {
	assert.deepEqual([...MACHINE_CPU_MILLIS], [1000, 2000, 4000, 6000, 8000, 12000, 16000]);
	assert.deepEqual([...MACHINE_MEMORY_MB], [2048, 4096, 8192, 12288, 16384, 24576, 32768, 49152, 65536]);
	assert.deepEqual([...MACHINE_DISK_GB], [20, 30, 40, 50, 75, 100, 150, 200, 300, 500, 750, 1000, 1500, 2048]);
});
