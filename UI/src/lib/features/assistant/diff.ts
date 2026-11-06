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

export function diffTotals(files: DiffFile[]): { files: number; additions: number; deletions: number } {
	return files.reduce(
		(sum, file) => ({
			files: sum.files + 1,
			additions: sum.additions + file.additions,
			deletions: sum.deletions + file.deletions
		}),
		{ files: 0, additions: 0, deletions: 0 }
	);
}

// Case-insensitive match against a line's text (never a regex: the query is user input).
export function lineMatches(text: string, query: string): boolean {
	const needle = query.trim().toLowerCase();
	return needle !== '' && text.toLowerCase().includes(needle);
}

// Files whose path or any line matches the query; a blank query matches all.
export function searchDiff(files: DiffFile[], query: string): DiffFile[] {
	if (!query.trim()) return files;
	return files.filter((file) => lineMatches(file.path, query) || file.lines.some((line) => lineMatches(line.text, query)));
}
