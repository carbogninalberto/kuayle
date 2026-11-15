// The size of the machine an assistant command runs on, as the approval cards
// show it: vCPUs and GB, without trailing zeros.
export interface MachineSize {
	cpu_millis: number;
	memory_mb: number;
	disk_gb: number;
}

const trim = (value: number) => String(Number(value.toFixed(2)));

export function commandMachineSize(machine: MachineSize): { cpu: string; memory: string; disk: string } {
	return { cpu: trim(machine.cpu_millis / 1000), memory: trim(machine.memory_mb / 1024), disk: trim(machine.disk_gb) };
}

/** Whether a command run may still change: polling stops otherwise. */
export function commandRunPending(status: string | undefined): boolean {
	return status === undefined || status === 'queued' || status === 'running';
}
