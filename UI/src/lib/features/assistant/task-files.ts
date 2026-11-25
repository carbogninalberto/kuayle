// The rules of a task's files panel (KUA-285, KUA-319): which folders are
// open, the tree built from the directories read so far, what an error means
// for the reader, paging through a long file, and a changed file's part of
// the stored diff.
import type { TaskFileEntry } from '$lib/api/agent-tasks';
import type { DiffFile } from './diff';
import type { FileNode } from './agent-trace';

/** One directory of the commit as the panel knows it: being read, failed, or read. */
export type DirState =
	| { status: 'loading' }
	| { status: 'error'; error: FileError }
	| { status: 'ready'; entries: TaskFileEntry[]; truncated: boolean };

/**
 * The tree of the directories read so far, from `path` (the root by
 * default). A folder that was not read yet, or is being read, has no
 * children; one that failed or is loading says so, and one the server cut
 * says how many entries it lists.
 */
export function treeNodes(dirs: Record<string, DirState>, path = ''): FileNode[] {
	const dir = dirs[path];
	if (dir?.status !== 'ready') return [];
	return dir.entries.map((entry): FileNode => {
		if (entry.kind !== 'tree')
			return entry.changed
				? { type: 'file', name: entry.name, path: entry.path, changed: true }
				: { type: 'file', name: entry.name, path: entry.path };
		const child = dirs[entry.path];
		const folder: Extract<FileNode, { type: 'folder' }> = {
			type: 'folder',
			name: entry.name,
			path: entry.path,
			children: treeNodes(dirs, entry.path)
		};
		if (entry.changed) folder.changed = true;
		if (child?.status === 'loading' || child?.status === 'error') folder.state = child.status;
		if (child?.status === 'ready' && child.truncated) folder.truncated = child.entries.length;
		return folder;
	});
}

/** The entry at `path` among the directories read so far. */
export function findEntry(dirs: Record<string, DirState>, path: string): TaskFileEntry | undefined {
	const dir = dirs[path.includes('/') ? path.slice(0, path.lastIndexOf('/')) : ''];
	return dir?.status === 'ready' ? dir.entries.find((entry) => entry.path === path) : undefined;
}

/**
 * Whether a folder is open: the reader's own toggle wins; otherwise folders
 * start closed (reading one is a request) and, while a filter is active, the
 * folders of its matches are open so the matches show.
 */
export function folderOpen(path: string, toggled: Record<string, boolean>, filtering: boolean): boolean {
	return toggled[path] ?? filtering;
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
