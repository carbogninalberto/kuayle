function parsePreviewCommand(value: string): string[] | undefined {
	if (!value.trim()) return undefined;
	const command: unknown = JSON.parse(value);
	const encoder = new TextEncoder();
	if (!Array.isArray(command) || command.length > 64) throw new Error('Invalid command');
	let total = 0;
	for (const [index, arg] of command.entries()) {
		if (typeof arg !== 'string') throw new Error('Invalid command');
		const bytes = encoder.encode(arg).length;
		total += bytes;
		if (bytes > 8192 || total > 32768 || arg.includes('\0') || (index === 0 && !arg.trim()))
			throw new Error('Invalid command');
	}
	return command;
}

// A task app needs a command (JSON argv); its port is only a preference,
// optional, since Kuayle allocates a free port when it is taken or unset.
// Both empty inherits.
export function previewSettingValid(command: string, port: string): boolean {
	if (!command.trim() && !port.trim()) return true;
	try {
		const argv = parsePreviewCommand(command);
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
	// Throws like parsePreviewCommand, so saving reports an incomplete preview.
	if (!previewSettingValid(command, port)) throw new Error('Invalid preview');
	if (!command.trim()) return {};
	return { preview_command: parsePreviewCommand(command), ...(port.trim() ? { preview_port: Number(port) } : {}) };
}
