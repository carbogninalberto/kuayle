import type { MachineResources, MachineSizeOptions } from './machine-size.ts';

export interface ResizableMachine {
	status: string;
	desired_status: string;
	delete_requested_at?: string;
	pending_cpu_millis?: number;
	pending_memory_mb?: number;
	pending_disk_gb?: number;
}

/**
 * The sizes a machine can be resized to: the workspace options for CPU and
 * memory, and disks up to the policy limit plus the machine's current disk
 * (which a lowered policy limit does not take away). Hard disk quotas are
 * fixed when the volume is created, so the disk then stays as it is.
 */
export function resizeSizeOptions(
	sizeOptions: MachineSizeOptions,
	maxDiskGb: number,
	current: MachineResources,
	diskResizable: boolean
): MachineSizeOptions {
	const disks = diskResizable ? sizeOptions.disk_gb.filter((disk) => disk <= maxDiskGb) : [];
	return {
		cpu_millis: [...sizeOptions.cpu_millis],
		memory_mb: [...sizeOptions.memory_mb],
		disk_gb: [...new Set([...disks, current.disk_gb])].sort((a, b) => a - b)
	};
}

export function pendingResize(machine: Omit<ResizableMachine, 'status' | 'desired_status'>): MachineResources | null {
	if (machine.pending_cpu_millis == null || machine.pending_memory_mb == null || machine.pending_disk_gb == null)
		return null;
	return {
		cpu_millis: machine.pending_cpu_millis,
		memory_mb: machine.pending_memory_mb,
		disk_gb: machine.pending_disk_gb
	};
}

/** Mirrors the API: settled running, paused or stopped machines without a pending size. */
export function canResizeMachine(machine: ResizableMachine): boolean {
	return (
		['running', 'paused', 'stopped'].includes(machine.status) &&
		machine.desired_status === machine.status &&
		!machine.delete_requested_at &&
		pendingResize(machine) === null
	);
}
