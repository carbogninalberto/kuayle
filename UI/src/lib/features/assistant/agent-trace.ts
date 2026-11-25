// Derives plan, reasoning and changed-file views from what an attempt actually
// recorded: the native harness's own JSON events and Kuayle's git state. Nothing
// here invents progress; unknown or malformed lines are ignored.

export type PlanStatus = 'done' | 'active' | 'pending';
export interface PlanStep {
	label: string;
	status: PlanStatus;
}
// changed (set only when true): the task changed the file, or a file below the folder.
export type FileNode =
	| { type: 'file'; name: string; path: string; changed?: true }
	| { type: 'folder'; name: string; path: string; changed?: true; children: FileNode[] };
type FolderNode = Extract<FileNode, { type: 'folder' }>;

const maxLineLength = 64 * 1024;
const maxSteps = 50;
const maxReasoning = 5;
const maxReasoningLength = 2000;

function events(lines: string[]): Record<string, unknown>[] {
	const parsed: Record<string, unknown>[] = [];
	for (const raw of lines) {
		for (const line of raw.split('\n')) {
			const text = line.trim();
			if (!text.startsWith('{') || text.length > maxLineLength) continue;
			try {
				const value = JSON.parse(text);
				if (value && typeof value === 'object' && !Array.isArray(value)) parsed.push(value);
			} catch {
				// Native output can mix JSON events with plain text.
			}
		}
	}
	return parsed;
}

const text = (value: unknown) => (typeof value === 'string' ? value.trim() : '');

function claudeContent(event: Record<string, unknown>): Record<string, unknown>[] {
	const message = event.message as { content?: unknown } | undefined;
	return event.type === 'assistant' && Array.isArray(message?.content)
		? (message.content.filter((part) => part && typeof part === 'object') as Record<string, unknown>[])
		: [];
}

/** The latest plan the harness published (Codex todo_list, Claude TodoWrite). */
export function latestPlan(lines: string[]): PlanStep[] | null {
	let plan: PlanStep[] | null = null;
	for (const event of events(lines)) {
		const item = event.item as { type?: unknown; items?: unknown } | undefined;
		if (
			typeof event.type === 'string' &&
			event.type.startsWith('item.') &&
			item?.type === 'todo_list' &&
			Array.isArray(item.items)
		) {
			let activeSeen = false;
			plan = item.items.slice(0, maxSteps).flatMap((entry) => {
				const label = text((entry as { text?: unknown })?.text);
				if (!label) return [];
				const completed = (entry as { completed?: unknown }).completed === true;
				const status: PlanStatus = completed ? 'done' : activeSeen ? 'pending' : 'active';
				if (!completed) activeSeen = true;
				return [{ label, status }];
			});
			continue;
		}
		for (const part of claudeContent(event)) {
			const input = part.input as { todos?: unknown } | undefined;
			if (part.type !== 'tool_use' || part.name !== 'TodoWrite' || !Array.isArray(input?.todos)) continue;
			plan = input.todos.slice(0, maxSteps).flatMap((todo) => {
				const label = text((todo as { content?: unknown })?.content);
				const status = (todo as { status?: unknown }).status;
				if (!label) return [];
				return [{ label, status: status === 'completed' ? 'done' : status === 'in_progress' ? 'active' : 'pending' }];
			});
		}
	}
	return plan && plan.length ? plan : null;
}

/** Provider-supplied reasoning summaries, newest last. Never synthesized. */
export function reasoningSummaries(lines: string[]): string[] {
	const found: string[] = [];
	for (const event of events(lines)) {
		const item = event.item as { type?: unknown; text?: unknown } | undefined;
		if (event.type === 'item.completed' && item?.type === 'reasoning') {
			const summary = text(item.text);
			if (summary) found.push(summary);
			continue;
		}
		for (const part of claudeContent(event)) {
			const thinking = part.type === 'thinking' ? text(part.thinking) : '';
			if (thinking) found.push(thinking);
		}
	}
	return found
		.slice(-maxReasoning)
		.map((entry) => (entry.length > maxReasoningLength ? entry.slice(0, maxReasoningLength) + '…' : entry));
}

const collator = new Intl.Collator();

/**
 * Nests repository-relative paths into folders, folders first, then by name.
 * An entry may mark its file changed; its folders are marked too. Only the
 * first `limit` entries are used.
 */
export function fileTree(entries: (string | { path: string; changed?: boolean })[], limit = 500): FileNode[] {
	const root: FileNode[] = [];
	const folders = new Map<string, FolderNode>();
	const files = new Set<string>();
	for (const entry of entries.slice(0, limit)) {
		const raw = typeof entry === 'string' ? entry : entry.path;
		const changed = typeof entry !== 'string' && entry.changed === true;
		const parts = raw.split('/').filter((part) => part && part !== '.' && part !== '..');
		let level = root;
		let path = '';
		parts.forEach((name, index) => {
			path = path ? `${path}/${name}` : name;
			if (index === parts.length - 1) {
				if (files.has(path)) return;
				files.add(path);
				level.push(changed ? { type: 'file', name, path, changed } : { type: 'file', name, path });
				return;
			}
			let folder = folders.get(path);
			if (!folder) {
				folder = { type: 'folder', name, path, children: [] };
				folders.set(path, folder);
				level.push(folder);
			}
			if (changed) folder.changed = true;
			level = folder.children;
		});
	}
	const sort = (nodes: FileNode[]): FileNode[] =>
		nodes
			.sort((a, b) => (a.type === b.type ? collator.compare(a.name, b.name) : a.type === 'folder' ? -1 : 1))
			.map((node) => (node.type === 'folder' ? { ...node, children: sort(node.children) } : node));
	return sort(root);
}
