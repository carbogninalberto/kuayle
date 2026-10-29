export interface MachineSizeOptions {
	cpu_millis: readonly number[];
	memory_mb: readonly number[];
	disk_gb: readonly number[];
}

export interface MachineResources {
	cpu_millis: number;
	memory_mb: number;
	disk_gb: number;
}

export const MACHINE_CPU_MILLIS = [1000, 2000, 4000, 6000, 8000, 12000, 16000] as const;
export const MACHINE_MEMORY_MB = [2048, 4096, 8192, 12288, 16384, 24576, 32768, 49152, 65536] as const;
// Disks are any even size from 20 GB to 2048 GB, as the API validates.
export const MACHINE_DISK_MIN_GB = 20;
export const MACHINE_DISK_MAX_GB = 2048;
export const MACHINE_DISK_STEP_GB = 2;
export const MACHINE_DISK_GB: readonly number[] = Array.from(
	{ length: (MACHINE_DISK_MAX_GB - MACHINE_DISK_MIN_GB) / MACHINE_DISK_STEP_GB + 1 },
	(_, index) => MACHINE_DISK_MIN_GB + index * MACHINE_DISK_STEP_GB
);

export const DEFAULT_MACHINE_RESOURCES: MachineResources = {
	cpu_millis: 4000,
	memory_mb: 8192,
	disk_gb: 50
};

// Machines Kuayle creates for agent tasks, unless the workspace policy says otherwise.
export const DEFAULT_TASK_MACHINE_RESOURCES: MachineResources = {
	cpu_millis: 4000,
	memory_mb: 8192,
	disk_gb: 20
};

function normalizedOptions(values: readonly number[] | undefined, fallback: readonly number[]): number[] {
	const source = values ?? fallback;
	return [...new Set(source.filter((value) => Number.isFinite(value) && value > 0))].sort((a, b) => a - b);
}

export function resolveMachineSizeOptions(
	sizeOptions: Partial<MachineSizeOptions> | undefined,
	maxDiskGb: number
): MachineSizeOptions {
	return {
		cpu_millis: normalizedOptions(sizeOptions?.cpu_millis, MACHINE_CPU_MILLIS),
		memory_mb: normalizedOptions(sizeOptions?.memory_mb, MACHINE_MEMORY_MB),
		disk_gb: normalizedOptions(sizeOptions?.disk_gb, MACHINE_DISK_GB).filter((value) => value <= maxDiskGb)
	};
}

export function clampToOption(value: number, options: readonly number[]): number {
	if (options.length === 0) return value;
	let nearest = options[0];
	let nearestDistance = Math.abs(value - nearest);
	for (const option of options.slice(1)) {
		const distance = Math.abs(value - option);
		if (distance < nearestDistance) {
			nearest = option;
			nearestDistance = distance;
		}
	}
	return nearest;
}

// A typed disk size becomes the smallest offered size that holds it, or the
// largest one. Below the previous size (a step down with the input's arrows)
// it becomes the largest offered size not above it, so stepping down works.
export function snapDiskGb(value: number, options: readonly number[], previous?: number): number {
	if (!Number.isFinite(value)) return previous ?? options[0] ?? MACHINE_DISK_MIN_GB;
	if (previous !== undefined && value < previous)
		return options.findLast((option) => option <= value) ?? options[0] ?? value;
	return options.find((option) => option >= value) ?? options.at(-1) ?? value;
}

export function minimumMemoryIndexForCpu(cpuMillis: number, memoryOptions: readonly number[]): number {
	if (memoryOptions.length === 0) return 0;
	const requiredMemoryMb = (cpuMillis * 1024) / 1000;
	const index = memoryOptions.findIndex((memoryMb) => memoryMb >= requiredMemoryMb);
	return index === -1 ? memoryOptions.length - 1 : index;
}

export function normalizeMachineResources(resources: MachineResources, options: MachineSizeOptions): MachineResources {
	const cpuMillis = clampToOption(resources.cpu_millis, options.cpu_millis);
	let memoryMb = clampToOption(resources.memory_mb, options.memory_mb);
	const minimumMemoryMb = options.memory_mb[minimumMemoryIndexForCpu(cpuMillis, options.memory_mb)];
	if (minimumMemoryMb !== undefined && memoryMb < minimumMemoryMb) memoryMb = minimumMemoryMb;

	return {
		cpu_millis: cpuMillis,
		memory_mb: memoryMb,
		disk_gb: clampToOption(resources.disk_gb, options.disk_gb)
	};
}

export function formatMachineResource(value: number, unit: string): string {
	return `${value} ${unit}`;
}

export function memoryGb(memoryMb: number): number {
	return memoryMb / 1024;
}
