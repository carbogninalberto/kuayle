import { parseTestCommand } from './test-command';

// A task app needs a command (JSON argv); its port is only a preference,
// optional, since Kuayle allocates a free port when it is taken or unset.
// Both empty inherits.
export function previewSettingValid(command: string, port: string): boolean {
	if (!command.trim() && !port.trim()) return true;
	try {
		const argv = parseTestCommand(command);
		if (!argv?.length) return false;
		if (!port.trim()) return true;
		const value = Number(port);
		return Number.isInteger(value) && value >= 1024 && value <= 65535;
	} catch {
		return false;
	}
}

export function previewSettingPayload(
	command: string,
	port: string
): { preview_command?: string[]; preview_port?: number } {
	// Throws like parseTestCommand, so saving reports an incomplete preview.
	if (!previewSettingValid(command, port)) throw new Error('Invalid preview');
	if (!command.trim()) return {};
	return { preview_command: parseTestCommand(command), ...(port.trim() ? { preview_port: Number(port) } : {}) };
}
