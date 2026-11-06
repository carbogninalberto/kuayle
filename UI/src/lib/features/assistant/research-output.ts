// Reads the recorded output of the research tools (web_search, repo_*,
// spawn_research_subagents; shapes in BE/docs/conversations.md, "Research
// tools"). Output is untrusted: every field is checked and anything unexpected
// is dropped rather than shown.

const str = (value: unknown) => (typeof value === 'string' ? value : '');
const num = (value: unknown) => (typeof value === 'number' && Number.isFinite(value) ? value : 0);
const record = (value: unknown) =>
	value && typeof value === 'object' && !Array.isArray(value) ? (value as Record<string, unknown>) : undefined;
const list = (value: unknown) => (Array.isArray(value) ? value : []);

/** The URL when it is http(s), so a result can never link to javascript: or data:. */
export function httpURL(value: unknown): string {
	const text = str(value);
	try {
		const url = new URL(text);
		return url.protocol === 'https:' || url.protocol === 'http:' ? url.href : '';
	} catch {
		return '';
	}
}

export function formatMs(ms: number): string {
	if (!ms) return '';
	return ms < 1000 ? `${Math.round(ms)}ms` : `${(ms / 1000).toFixed(ms < 10_000 ? 1 : 0)}s`;
}

export interface WebSearchOutput {
	query: string;
	results: { title: string; url: string; excerpt: string }[];
	truncated: boolean;
}

export function webSearchOutput(output: unknown): WebSearchOutput | undefined {
	const data = record(output);
	if (!data) return undefined;
	return {
		query: str(data.query),
		results: list(data.results)
			.map(record)
			.filter((item) => item !== undefined)
			.map((item) => ({ title: str(item.title), url: str(item.url), excerpt: str(item.excerpt) })),
		truncated: data.truncated === true
	};
}

export interface RepoScanOutput {
	repository: string;
	commit: string;
	/** The path or pattern the tool used, so the step is auditable. */
	query: string;
	lines: string[];
	summary: string;
}

const maxLines = 50;

/** A listed file is a record {path, size, binary?, omitted?}; its line names the path and any reason it was left out. */
function fileLine(value: unknown): string {
	const file = record(value);
	if (!file) return '';
	const path = str(file.path);
	if (!path) return '';
	const notes = [file.binary === true ? 'binary' : '', str(file.omitted)].filter(Boolean);
	return notes.length ? `${path}  (${notes.join(', ')})` : path;
}

export function repoScanOutput(name: string, input: unknown, output: unknown): RepoScanOutput | undefined {
	const data = record(output);
	if (!data) return undefined;
	const args = record(input) ?? {};
	const commit = str(data.commit).slice(0, 12);
	const scan: RepoScanOutput = { repository: str(data.repository), commit, query: '', lines: [], summary: '' };
	switch (name) {
		case 'repo_list_files':
			scan.query = str(data.path) || str(args.path);
			scan.lines = list(data.files).map(fileLine).filter(Boolean);
			scan.summary = `${num(data.matched) || scan.lines.length} / ${num(data.total_files)}`;
			break;
		case 'repo_glob':
			scan.query = str(data.pattern) || str(args.pattern);
			scan.lines = list(data.files).map(fileLine).filter(Boolean);
			scan.summary = `${num(data.matched) || scan.lines.length}`;
			break;
		case 'repo_grep':
			scan.query = str(data.pattern) || str(args.pattern);
			scan.lines = list(data.matches)
				.map(record)
				.filter((item) => item !== undefined)
				.map((item) => `${str(item.path)}:${num(item.line)}  ${str(item.text)}`);
			scan.summary = `${num(data.files_matched)} / ${num(data.files_searched)}${data.timed_out === true ? ' · timed out' : ''}`;
			break;
		case 'repo_read_file':
			scan.query = `${str(data.path) || str(args.path)}:${num(data.start_line)}-${num(data.end_line)}`;
			scan.lines = str(data.content).split('\n');
			scan.summary = `${num(data.end_line) - num(data.start_line) + 1} / ${num(data.total_lines)}${data.truncated === true ? ' · truncated' : ''}`;
			break;
		case 'repo_symbols':
			scan.query = str(data.path) || str(args.path);
			scan.lines = list(data.symbols)
				.map(record)
				.filter((item) => item !== undefined)
				.map((item) => `${str(item.path)}:${num(item.line)}  ${str(item.kind)} ${str(item.name)}`);
			break;
		default:
			return undefined;
	}
	scan.lines = scan.lines.slice(0, maxLines);
	return scan;
}

export interface SubagentRow {
	index: number;
	title: string;
	status: string;
	model: string;
	duration_ms: number;
	summary: string;
	error: string;
	files: string[];
	citations: { url: string; title: string }[];
}

export interface SubagentsOutput {
	completed: number;
	failed: number;
	duration_ms: number;
	subagents: SubagentRow[];
}

export function subagentsOutput(output: unknown): SubagentsOutput | undefined {
	const data = record(output);
	if (!data) return undefined;
	return {
		completed: num(data.completed),
		failed: num(data.failed),
		duration_ms: num(data.duration_ms),
		subagents: list(data.subagents)
			.map(record)
			.filter((item) => item !== undefined)
			.map((item, position) => ({
				index: typeof item.index === 'number' ? item.index : position,
				title: str(item.title),
				status: str(item.status),
				model: str(item.model),
				duration_ms: num(item.duration_ms),
				summary: str(item.summary),
				error: str(item.error),
				files: list(item.files).map(str).filter(Boolean).slice(0, 20),
				citations: list(item.citations)
					.map(record)
					.filter((citation) => citation !== undefined)
					.map((citation) => ({ url: str(citation.url), title: str(citation.title) }))
			}))
	};
}
