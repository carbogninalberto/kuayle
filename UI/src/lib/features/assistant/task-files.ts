// The rules of a task's files panel (KUA-285): filtering the listing, which
// folders start open, what an error means for the reader, paging through a
// long file, and a changed file's part of the stored diff.
import type { DiffFile } from './diff';

export interface FileEntry {
	path: string;
	changed: boolean;
}

/** How many filter matches the tree shows at once; a filter is refined, not scrolled. */
export const matchCap = 500;

/** Entries whose path contains the query (text, case-insensitive); a blank query matches all. */
export function matchFiles<T extends FileEntry>(entries: T[], query: string, cap = matchCap): { files: T[]; count: number } {
	const needle = query.trim().toLowerCase();
	if (!needle) return { files: entries, count: entries.length };
	const files: T[] = [];
	let count = 0;
	for (const entry of entries) {
		if (!entry.path.toLowerCase().includes(needle)) continue;
		count++;
		if (files.length < cap) files.push(entry);
	}
	return { files, count };
}

/**
 * Whether a folder is open: the reader's own toggle wins; otherwise folders
 * on the first level (depth 0) start open and deeper ones closed, and while a
 * filter is active every folder is open so the matches show.
 */
export function folderOpen(path: string, depth: number, toggled: Record<string, boolean>, filtering: boolean): boolean {
	return toggled[path] ?? (filtering || depth === 0);
}

/**
 * What a failed read means for the reader. The routes answer 404 to anyone
 * but the task's creator, so a plain not-found is denied access; a file or
 * commit not found means the head moved on since the listing was read.
 */
export type FileError = 'denied' | 'unavailable' | 'busy' | 'stale' | 'error';
export function fileError(error: unknown): FileError {
	const status = (error as { status?: number } | undefined)?.status;
	const code = (error as { error?: { code?: string } } | undefined)?.error?.code;
	if (code === 'FILE_NOT_FOUND' || code === 'INVALID_REF') return 'stale';
	if (code === 'FILES_UNAVAILABLE') return 'unavailable';
	if (code === 'FILES_BUSY') return 'busy';
	if (status === 403 || status === 404) return 'denied';
	return 'error';
}

interface Page {
	start: number;
	end: number;
	total_lines: number;
	truncated: boolean;
}

/**
 * The page after this one, read on from the line after its last, or null on
 * the last page. history holds where the earlier pages started, so going back
 * returns exactly to them whatever size the server made each page.
 */
export function nextPage(page: Page, history: number[]): { start: number; history: number[] } | null {
	if (page.end >= page.total_lines) return null;
	return { start: page.end + 1, history: [...history, page.start] };
}

export function previousPage(history: number[]): { start: number; history: number[] } | null {
	if (!history.length) return null;
	return { start: history[history.length - 1], history: history.slice(0, -1) };
}

/** One file's part of a parsed diff, by its path after the change (a renamed file by its new name). */
export function diffOf(files: DiffFile[], path: string): DiffFile | undefined {
	return files.find((file) => file.path === path);
}
