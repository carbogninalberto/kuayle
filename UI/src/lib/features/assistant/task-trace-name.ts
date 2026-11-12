// The trace download's fallback filename when the server does not send one:
// the task's identifier, reduced to a safe file name.
export function taskTraceFilename(identifier: string | null | undefined, id: string): string {
	const base = (identifier?.trim() || id).replace(/[^A-Za-z0-9._-]/g, '_');
	return `kuayle-trace-${base || id}.json`;
}
