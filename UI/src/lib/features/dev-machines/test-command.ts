// Blank inherits; [] is an explicit override with no configured check.
export function parseTestCommand(value: string): string[] | undefined {
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

export function formatTestCommand(value: string[] | null | undefined): string {
	return value == null ? '' : JSON.stringify(value);
}
