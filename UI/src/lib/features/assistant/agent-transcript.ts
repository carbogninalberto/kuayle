// Turns a coding agent's recorded native output (the log chunks of one run)
// into a readable transcript: the agent's messages, its tool calls paired with
// their results, thinking markers and a compact session header. It reads
// Claude Code stream-json and Codex `exec --json` events; anything else is
// kept as plain text or a generic event so nothing recorded disappears.
//
// Everything here is untrusted agent output. The parser only extracts
// strings; the view renders them as escaped text (agent messages go through
// the sanitizing Markdown component). Nothing is invented: a tool without a
// recorded result has no output, and only provider-supplied fields are read.

export interface SessionChunk {
	id?: number;
	stream?: string;
	content: string;
	truncated?: boolean;
	created_at?: string;
}

export type ToolStatus = 'running' | 'done' | 'failed';
export interface SessionTool {
	id: string;
	name: string;
	/** One line for the chip: the agent's own description, else name + argument. */
	title: string;
	/** The command or input, as text. */
	input: string;
	output?: string;
	status: ToolStatus;
	/** A shell command (Claude Bash, Codex command_execution). */
	command: boolean;
	started?: string;
	ended?: string;
}

export type SessionEntry =
	| { kind: 'header'; key: string; model?: string; cwd?: string; version?: string; session?: string }
	| { kind: 'text'; key: string; text: string; at?: string }
	| { kind: 'user'; key: string; text: string; at?: string }
	| { kind: 'thinking'; key: string; ms?: number; text?: string }
	| { kind: 'tool'; key: string; tool: SessionTool }
	| { kind: 'omitted'; key: string; reason: 'oversized' | 'limit' }
	| { kind: 'plain'; key: string; text: string; stream: string }
	| { kind: 'event'; key: string; type: string; raw: string }
	| { kind: 'result'; key: string; failed: boolean; ms?: number; text?: string }
	| { kind: 'error'; key: string; message: string };

type StepEntry = Extract<SessionEntry, { kind: 'tool' | 'thinking' }>;
type EventEntry = Extract<SessionEntry, { kind: 'event' }>;
export type SessionBlock =
	| Exclude<SessionEntry, EventEntry>
	| { kind: 'steps'; key: string; entries: StepEntry[]; tools: SessionTool[]; commands: boolean; failed: number }
	| { kind: 'events'; key: string; events: EventEntry[] };

const maxLineLength = 1024 * 1024;
const maxTitle = 88;

type Json = Record<string, unknown>;
const isObject = (value: unknown): value is Json => !!value && typeof value === 'object' && !Array.isArray(value);
const str = (value: unknown) => (typeof value === 'string' ? value : '');
const num = (value: unknown) => (typeof value === 'number' && Number.isFinite(value) ? value : undefined);

function truncate(value: string, max = maxTitle) {
	const line = value.replace(/\s+/g, ' ').trim();
	return line.length > max ? line.slice(0, max - 1).trimEnd() + '…' : line;
}

// The argument that best names what a tool call does, for the chip fallback.
function primaryArgument(input: Json): string {
	for (const key of ['command', 'file_path', 'path', 'pattern', 'url', 'query', 'prompt', 'description']) {
		const value = str(input[key]);
		if (value) return value;
	}
	for (const value of Object.values(input)) if (typeof value === 'string' && value) return value;
	return '';
}

function inputText(input: unknown): string {
	if (!isObject(input)) return typeof input === 'string' ? input : '';
	if (typeof input.command === 'string') return input.command;
	return JSON.stringify(input, null, 2);
}

function resultText(content: unknown): string {
	if (typeof content === 'string') return content;
	if (!Array.isArray(content)) return isObject(content) ? JSON.stringify(content, null, 2) : '';
	return content
		.map((part) => {
			if (typeof part === 'string') return part;
			if (!isObject(part)) return '';
			if (part.type === 'text') return str(part.text);
			if (part.type === 'image') return '[image]';
			return JSON.stringify(part);
		})
		.filter(Boolean)
		.join('\n');
}

// Codex runs commands through a login shell; the chip shows the command itself.
function unwrapShell(command: string) {
	const match = command.match(/^(?:\/bin\/)?(?:ba|z)?sh\s+-l?c\s+(['"])([\s\S]*)\1$/);
	return match ? match[2] : command;
}

// Splits chunks into complete lines, keeping each line's chunk metadata. A
// line that spans chunks is joined; truncated chunks stand on their own.
function lines(chunks: SessionChunk[]) {
	const result: { text: string; stream: string; at?: string; truncated: boolean; key: string }[] = [];
	const pending: Record<string, { text: string; at?: string; key: string } | undefined> = {};
	chunks.forEach((chunk, index) => {
		const stream = chunk.stream ?? 'stdout';
		const base = String(chunk.id ?? index);
		if (chunk.truncated) {
			pending[stream] = undefined;
			result.push({ text: chunk.content.trim(), stream, at: chunk.created_at, truncated: true, key: base });
			return;
		}
		const parts = chunk.content.split('\n');
		parts.forEach((part, i) => {
			const last = i === parts.length - 1;
			const carried = pending[stream];
			if (last) {
				if (part)
					pending[stream] = {
						text: (carried?.text ?? '') + part,
						at: carried?.at ?? chunk.created_at,
						key: carried?.key ?? `${base}.${i}`
					};
				return;
			}
			pending[stream] = undefined;
			const text = (carried?.text ?? '') + part;
			if (text.trim())
				result.push({
					text,
					stream,
					at: carried?.at ?? chunk.created_at,
					truncated: false,
					key: carried?.key ?? `${base}.${i}`
				});
		});
	});
	for (const [stream, carried] of Object.entries(pending))
		if (carried?.text.trim())
			result.push({ text: carried.text, stream, at: carried.at, truncated: false, key: carried.key });
	return result;
}

function parseLine(text: string): Json | undefined {
	const trimmed = text.trim();
	if (!trimmed.startsWith('{') || trimmed.length > maxLineLength) return undefined;
	try {
		const value = JSON.parse(trimmed);
		return isObject(value) ? value : undefined;
	} catch {
		return undefined;
	}
}

// Events that carry nothing a reader needs (usage, rate limits, turn markers).
const quiet = new Set(['rate_limit_event', 'stream_event', 'turn.started', 'turn.completed', 'keep_alive']);

export function parseAgentSession(chunks: SessionChunk[]): SessionEntry[] {
	const entries: SessionEntry[] = [];
	const tools = new Map<string, SessionTool>();
	const push = (entry: SessionEntry) => entries.push(entry);
	for (const line of lines(chunks)) {
		if (line.truncated) {
			push({ kind: 'omitted', key: line.key, reason: /limit/i.test(line.text) ? 'limit' : 'oversized' });
			continue;
		}
		const event = parseLine(line.text);
		if (!event || typeof event.type !== 'string') {
			if (event) {
				push({ kind: 'event', key: line.key, type: 'event', raw: line.text.trim() });
				continue;
			}
			const previous = entries.at(-1);
			if (previous?.kind === 'plain' && previous.stream === line.stream) previous.text += '\n' + line.text;
			else push({ kind: 'plain', key: line.key, text: line.text, stream: line.stream });
			continue;
		}
		const type = event.type;
		if (quiet.has(type)) continue;
		const before = entries.length;
		// ── Claude Code stream-json
		if (type === 'system' && event.subtype === 'init') {
			const version = str(event.claude_code_version);
			push({
				kind: 'header',
				key: line.key,
				model: str(event.model) || undefined,
				cwd: str(event.cwd) || undefined,
				version: version ? `Claude Code ${version}` : undefined
			});
		} else if ((type === 'assistant' || type === 'user') && isObject(event.message)) {
			const content = event.message.content;
			const parts =
				typeof content === 'string' ? [{ type: 'text', text: content }] : Array.isArray(content) ? content : [];
			parts.forEach((part, index) => {
				if (!isObject(part)) return;
				const key = `${line.key}:${index}`;
				if (part.type === 'text') {
					const text = str(part.text).trim();
					if (!text) return;
					const previous = entries.at(-1);
					const kind = type === 'assistant' ? 'text' : 'user';
					if (previous?.kind === kind && previous.key.startsWith(`${line.key}:`)) previous.text += '\n\n' + text;
					else push({ kind, key, text, at: line.at });
				} else if (part.type === 'thinking' || part.type === 'redacted_thinking') {
					// Only the duration and any provider-supplied text; never the signature.
					push({
						kind: 'thinking',
						key,
						ms: num(event.thinking_duration_ms),
						text: str(part.thinking).trim() || undefined
					});
				} else if (part.type === 'tool_use') {
					const name = str(part.name) || 'tool';
					const input = isObject(part.input) ? part.input : {};
					const description = str(input.description).trim();
					const argument = primaryArgument(input);
					const tool: SessionTool = {
						id: str(part.id) || key,
						name,
						title: truncate(description || (argument ? `${name}: ${argument}` : name)),
						input: inputText(part.input),
						status: 'running',
						command: name === 'Bash',
						started: line.at
					};
					tools.set(tool.id, tool);
					push({ kind: 'tool', key, tool });
				} else if (part.type === 'tool_result') {
					const tool = tools.get(str(part.tool_use_id));
					if (!tool) return;
					tool.output = resultText(part.content);
					tool.status = part.is_error === true ? 'failed' : 'done';
					tool.ended = line.at;
				}
			});
		} else if (type === 'result') {
			const failed = event.is_error === true || (typeof event.subtype === 'string' && event.subtype !== 'success');
			push({
				kind: 'result',
				key: line.key,
				failed,
				ms: num(event.duration_ms),
				...(failed && str(event.result) ? { text: str(event.result) } : {})
			});
		}
		// ── Codex exec --json
		else if (type === 'thread.started') {
			push({ kind: 'header', key: line.key, session: str(event.thread_id) || undefined });
		} else if (type.startsWith('item.') && isObject(event.item)) {
			codexItem(event.item, type, line, entries, tools);
		} else if (type === 'turn.failed' || type === 'error') {
			const error = isObject(event.error) ? event.error : event;
			push({ kind: 'error', key: line.key, message: str(error.message) || type });
		}
		// Anything else is shown as a generic event; it can be inspected raw.
		if (entries.length === before && !handled(type, event))
			push({
				kind: 'event',
				key: line.key,
				type: typeof event.subtype === 'string' ? `${type}.${event.subtype}` : type,
				raw: line.text.trim()
			});
	}
	return entries;
}

// Recognized events that legitimately add nothing (a result paired to an
// earlier call, an empty text block, a todo list shown as the plan).
function handled(type: string, event: Json) {
	if (type === 'assistant' || type === 'user' || type === 'result' || type === 'thread.started') return true;
	if (type === 'system' && event.subtype === 'init') return true;
	return type.startsWith('item.') && isObject(event.item);
}

function codexItem(
	item: Json,
	type: string,
	line: { key: string; at?: string },
	entries: SessionEntry[],
	tools: Map<string, SessionTool>
) {
	const id = str(item.id) || line.key;
	const completed = type === 'item.completed';
	const status = str(item.status);
	const existing = tools.get(id);
	const tool = (fields: Omit<SessionTool, 'id' | 'status' | 'started' | 'ended'>, failed: boolean) => {
		const next: ToolStatus = failed ? 'failed' : completed || status === 'completed' ? 'done' : 'running';
		if (existing) {
			Object.assign(existing, fields, { status: next, ended: next === 'running' ? undefined : line.at });
			return;
		}
		const created: SessionTool = {
			id,
			...fields,
			status: next,
			started: line.at,
			ended: next === 'running' ? undefined : line.at
		};
		tools.set(id, created);
		entries.push({ kind: 'tool', key: line.key, tool: created });
	};
	switch (item.type) {
		case 'agent_message': {
			const text = str(item.text).trim();
			if (completed && text) entries.push({ kind: 'text', key: line.key, text, at: line.at });
			return;
		}
		case 'reasoning': {
			if (completed) entries.push({ kind: 'thinking', key: line.key, text: str(item.text).trim() || undefined });
			return;
		}
		case 'command_execution': {
			const command = str(item.command);
			const exit = num(item.exit_code);
			tool(
				{
					name: 'command',
					title: truncate(unwrapShell(command)) || 'command',
					input: command,
					output: str(item.aggregated_output) || existing?.output,
					command: true
				},
				status === 'failed' || (exit !== undefined && exit !== 0)
			);
			return;
		}
		case 'file_change': {
			const changes = Array.isArray(item.changes) ? item.changes.filter(isObject) : [];
			tool(
				{
					name: 'file_change',
					title:
						truncate(
							changes
								.map((change) => str(change.path))
								.filter(Boolean)
								.join(', ')
						) || 'file_change',
					input: changes.map((change) => `${str(change.kind)} ${str(change.path)}`.trim()).join('\n'),
					command: false
				},
				status === 'failed'
			);
			return;
		}
		case 'mcp_tool_call': {
			const name = [str(item.server), str(item.tool)].filter(Boolean).join('.') || 'mcp';
			const result = item.result ?? item.error;
			tool(
				{
					name,
					title: truncate(name),
					input: item.arguments === undefined ? '' : JSON.stringify(item.arguments, null, 2),
					output:
						result === undefined
							? existing?.output
							: typeof result === 'string'
								? result
								: JSON.stringify(result, null, 2),
					command: false
				},
				status === 'failed' || item.error !== undefined
			);
			return;
		}
		case 'web_search': {
			const query = str(item.query);
			tool({ name: 'web_search', title: truncate(query || 'web_search'), input: query, command: false }, false);
			return;
		}
		case 'error': {
			if (completed) entries.push({ kind: 'error', key: line.key, message: str(item.message) || 'error' });
			return;
		}
		case 'todo_list':
			// The plan is shown from the same events in the task's details.
			return;
		default:
			if (completed)
				entries.push({
					kind: 'event',
					key: line.key,
					type: `item.${str(item.type) || 'unknown'}`,
					raw: JSON.stringify(item)
				});
	}
}

/** Folds consecutive tool calls (and the thinking between them) into one group. */
export function groupSession(entries: SessionEntry[]): SessionBlock[] {
	const blocks: SessionBlock[] = [];
	let run: StepEntry[] = [];
	const flush = () => {
		const tools = run.flatMap((entry) => (entry.kind === 'tool' ? [entry.tool] : []));
		// Thinking before the first call stays visible as its own marker.
		while (tools.length > 1 && run[0].kind === 'thinking') blocks.push(run.shift()!);
		if (tools.length > 1)
			blocks.push({
				kind: 'steps',
				key: run[0].key,
				entries: run,
				tools,
				commands: tools.every((tool) => tool.command),
				failed: tools.filter((tool) => tool.status === 'failed').length
			});
		else blocks.push(...run);
		run = [];
	};
	for (const entry of entries) {
		if (entry.kind === 'tool' || entry.kind === 'thinking') {
			run.push(entry);
			continue;
		}
		if (run.length) flush();
		const previous = blocks.at(-1);
		if (entry.kind === 'event') {
			if (previous?.kind === 'events') previous.events.push(entry);
			else blocks.push({ kind: 'events', key: entry.key, events: [entry] });
			continue;
		}
		blocks.push(entry);
	}
	if (run.length) flush();
	return blocks;
}

/** The start of a long tool output and how many lines are behind "show more". */
export function outputPreview(text: string, maxLines = 12, maxChars = 2000) {
	const all = text.split('\n');
	let preview = all.slice(0, maxLines).join('\n');
	if (preview.length > maxChars) preview = preview.slice(0, maxChars);
	if (preview.length >= text.length) return { preview: text, hidden: 0 };
	return { preview, hidden: Math.max(1, all.length - preview.split('\n').length) };
}
