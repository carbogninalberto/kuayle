// A mocked files API for browser tests (KUA-319): the answers the server gives
// for one directory of a commit's tracked files (?path=), or for a search of their
// paths (?query=), computed from a flat list of the commit's files.
export interface TrackedFile {
	path: string;
	changed: boolean;
}

const entry = (path: string, kind: 'tree' | 'blob', changed: boolean) => ({
	name: path.slice(path.lastIndexOf('/') + 1),
	path,
	kind,
	sha: (kind === 'tree' ? 'a' : 'b').repeat(40),
	...(kind === 'blob' ? { size: 120 } : {}),
	changed
});

export function filesAnswer(files: TrackedFile[], commit: string, url: URL, searchCap = 500) {
	const query = (url.searchParams.get('query') ?? '').toLowerCase();
	if (query) {
		const found = files.filter((file) => file.path.toLowerCase().includes(query));
		return {
			status: 200,
			json: {
				commit,
				path: '',
				query,
				entries: found.slice(0, searchCap).map((file) => entry(file.path, 'blob', file.changed)),
				matches: found.length,
				truncated: found.length > searchCap
			}
		};
	}
	const dir = (url.searchParams.get('path') ?? '').replace(/\/$/, '');
	const prefix = dir ? `${dir}/` : '';
	const below = files.filter((file) => file.path.startsWith(prefix));
	if (dir && !below.length) return { status: 404, json: { error: { code: 'FILE_NOT_FOUND', message: 'not found' } } };
	const folders = new Map<string, boolean>();
	const blobs: ReturnType<typeof entry>[] = [];
	for (const file of below) {
		const rest = file.path.slice(prefix.length);
		const slash = rest.indexOf('/');
		if (slash < 0) blobs.push(entry(file.path, 'blob', file.changed));
		else {
			const path = prefix + rest.slice(0, slash);
			folders.set(path, (folders.get(path) ?? false) || file.changed);
		}
	}
	const entries = [
		...[...folders].map(([path, changed]) => entry(path, 'tree', changed)).sort((a, b) => a.name.localeCompare(b.name)),
		...blobs.sort((a, b) => a.name.localeCompare(b.name))
	];
	return { status: 200, json: { commit, path: dir, tree: 'c'.repeat(40), entries, truncated: false } };
}
