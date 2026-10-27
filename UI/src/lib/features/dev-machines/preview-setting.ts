import { parseTestCommand } from './test-command';

// A preview needs both a command (JSON argv) and a port, or neither (inherit).
export function previewSettingValid(command: string, port: string): boolean {
	if (!command.trim() && !port.trim()) return true;
	try {
		const argv = parseTestCommand(command);
		const value = Number(port);
		return !!argv?.length && Number.isInteger(value) && value >= 1024 && value <= 65535;
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
	return { preview_command: parseTestCommand(command), preview_port: Number(port) };
}
