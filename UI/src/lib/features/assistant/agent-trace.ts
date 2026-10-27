// Derives plan, reasoning and changed-file views from what an attempt actually
// recorded: the native harness's own JSON events and Kuayle's git state. Nothing
// here invents progress; unknown or malformed lines are ignored.

export type PlanStatus = 'done' | 'active' | 'pending';
export interface PlanStep {
	label: string;
	status: PlanStatus;
}
export type FileNode =
	| { type: 'file'; name: string; path: string }
	| { type: 'folder'; name: string; path: string; children: FileNode[] };

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

/** Nests repository-relative paths into folders, folders first, then by name. */
export function fileTree(paths: string[]): FileNode[] {
	const root: FileNode[] = [];
	for (const raw of paths.slice(0, 500)) {
		const parts = raw.split('/').filter((part) => part && part !== '.' && part !== '..');
		let level = root;
		parts.forEach((name, index) => {
			const path = parts.slice(0, index + 1).join('/');
			if (index === parts.length - 1) {
				if (!level.some((node) => node.path === path)) level.push({ type: 'file', name, path });
				return;
			}
			let folder = level.find((node) => node.type === 'folder' && node.path === path);
			if (!folder) {
				folder = { type: 'folder', name, path, children: [] };
				level.push(folder);
			}
			level = (folder as { children: FileNode[] }).children;
		});
	}
	const sort = (nodes: FileNode[]): FileNode[] =>
		nodes
			.sort((a, b) => (a.type === b.type ? a.name.localeCompare(b.name) : a.type === 'folder' ? -1 : 1))
			.map((node) => (node.type === 'folder' ? { ...node, children: sort(node.children) } : node));
	return sort(root);
}
