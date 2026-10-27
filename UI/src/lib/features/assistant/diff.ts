// Splits a unified git diff into files and typed lines for display. The text
// is untrusted repository content; it is only ever rendered as text.
export type DiffLineKind = 'add' | 'del' | 'hunk' | 'context' | 'meta';
export interface DiffFile {
	path: string;
	additions: number;
	deletions: number;
	lines: { kind: DiffLineKind; text: string }[];
}

export function parseDiff(diff: string): DiffFile[] {
	const files: DiffFile[] = [];
	let current: DiffFile | null = null;
	for (const line of diff.split('\n')) {
		const header = /^diff --git a\/(.+?) b\/(.+)$/.exec(line);
		if (header) {
			current = { path: header[2], additions: 0, deletions: 0, lines: [] };
			files.push(current);
			continue;
		}
		if (!current) continue;
		if (line.startsWith('@@')) current.lines.push({ kind: 'hunk', text: line });
		else if (line.startsWith('+++') || line.startsWith('---')) continue;
		else if (/^(index |new file|deleted file|similarity|rename |old mode|new mode|Binary files)/.test(line))
			current.lines.push({ kind: 'meta', text: line });
		else if (line.startsWith('+')) {
			current.additions++;
			current.lines.push({ kind: 'add', text: line });
		} else if (line.startsWith('-')) {
			current.deletions++;
			current.lines.push({ kind: 'del', text: line });
		} else if (line !== '' || current.lines.length) current.lines.push({ kind: 'context', text: line });
	}
	for (const file of files) while (file.lines.at(-1)?.kind === 'context' && !file.lines.at(-1)?.text) file.lines.pop();
	return files;
}
