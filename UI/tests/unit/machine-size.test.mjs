import assert from 'node:assert/strict';
import test from 'node:test';
import {
	DEFAULT_MACHINE_RESOURCES,
	DEFAULT_TASK_MACHINE_RESOURCES,
	MACHINE_CPU_MILLIS,
	MACHINE_DISK_GB,
	MACHINE_MEMORY_MB,
	clampToOption,
	formatMachineResource,
	memoryGb,
	minimumMemoryIndexForCpu,
	normalizeMachineResources,
	resolveMachineSizeOptions,
	snapDiskGb
} from '../../src/lib/features/dev-machines/machine-size.ts';

test('uses the shared snap lists and filters disk to the workspace policy', () => {
	const options = resolveMachineSizeOptions(undefined, 75);

	assert.deepEqual(options.cpu_millis, [...MACHINE_CPU_MILLIS]);
	assert.deepEqual(options.memory_mb, [...MACHINE_MEMORY_MB]);
	assert.equal(options.disk_gb.length, 28);
	assert.deepEqual(options.disk_gb.slice(0, 3), [20, 22, 24]);
	assert.equal(options.disk_gb.at(-1), 74, 'an odd limit offers the even sizes below it');
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
	assert.equal(MACHINE_DISK_GB.length, 1015);
	assert.equal(MACHINE_DISK_GB[0], 20);
	assert.equal(MACHINE_DISK_GB.at(-1), 2048);
	assert.ok(MACHINE_DISK_GB.every((value, index) => index === 0 || value - MACHINE_DISK_GB[index - 1] === 2));
});

test('task machines default to 4 vCPU and 8 GB', () => {
	assert.deepEqual(DEFAULT_TASK_MACHINE_RESOURCES, { cpu_millis: 4000, memory_mb: 8192, disk_gb: 20 });
});

test('snaps a typed disk size to a 2 GB step within the allowed range', () => {
	const options = resolveMachineSizeOptions(undefined, 500);
	assert.equal(snapDiskGb(61, options.disk_gb), 62);
	assert.equal(snapDiskGb(60, options.disk_gb), 60);
	assert.equal(snapDiskGb(5, options.disk_gb), 20);
	assert.equal(snapDiskGb(4000, options.disk_gb), 500);
	assert.equal(snapDiskGb(Number.NaN, options.disk_gb), 20);
	assert.equal(snapDiskGb(Number.NaN, options.disk_gb, 64), 64, 'a cleared input keeps the size');
	assert.equal(snapDiskGb(21, options.disk_gb, 20), 22, 'a step up reaches the next size');
	assert.equal(snapDiskGb(21, options.disk_gb, 22), 20, 'a step down reaches the previous size');
	assert.equal(snapDiskGb(5, options.disk_gb, 22), 20);
	const legacy = [20, 30, 40, 50, 75];
	assert.equal(snapDiskGb(74, legacy, 75), 50);
	assert.equal(snapDiskGb(76, legacy, 75), 75);
});
