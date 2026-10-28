import type { AppPort } from '$lib/types/dev-machine';

// Kept free of runtime imports so the node unit tests load it directly.

export function validAppPort(port: number): boolean {
	return Number.isInteger(port) && port >= 1024 && port <= 65535;
}

export function portName(port: Pick<AppPort, 'port' | 'label'>): string {
	return port.label || `:${port.port}`;
}

// Listening ports that are not exposed yet, offered as one-click chips.
export function detectedPorts(listening: number[], ports: Pick<AppPort, 'port'>[]): number[] {
	const exposed = new Set(ports.map((p) => p.port));
	return [...new Set(listening)].filter((port) => !exposed.has(port)).sort((a, b) => a - b);
}

// A task's app ports: the one Kuayle started for it, then those its agent exposed.
export function taskAppPorts<T extends Pick<AppPort, 'source' | 'agent_task_id' | 'port'>>(
	ports: T[],
	taskId: string
): T[] {
	return ports
		.filter((p) => p.agent_task_id === taskId && (p.source === 'task' || p.source === 'agent'))
		.sort((a, b) => (a.source === b.source ? a.port - b.port : a.source === 'task' ? -1 : 1));
}
