// Turns a coding agent's recorded native output (the log chunks of one run)
// into a readable transcript: the agent's messages, its tool calls paired with
// their results, thinking markers and a compact session header. It reads
// Claude Code stream-json, Codex `exec --json` and OpenCode `run --format
// json` events; anything else is kept as plain text or a generic event so
// nothing recorded disappears.
//
// Claude Code marks what a subagent does with `parent_tool_use_id`, the id of
// the Task/Agent call that started it; those entries are kept under that call
// (`children`), at any depth. The line with that call is often too large to
// be recorded, so the group is also made from Claude Code's `task_started`
// event for the call, or from the first event naming it. Its `task_*` events
// are not listed: they say how a subagent or a background command ended.
// Codex and OpenCode output names no parent, so their runs stay flat.
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
	/** A shell command (Claude Bash, Codex command_execution, OpenCode bash). */
	command: boolean;
	started?: string;
	ended?: string;
	/** What the subagent this call started did, in order. Only on such calls. */
	children?: SessionEntry[];
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
	| { kind: 'result'; key: string; failed: boolean; ms?: number; text?: string; usageLimit?: boolean }
	| { kind: 'error'; key: string; message: string };

type StepEntry = Extract<SessionEntry, { kind: 'tool' | 'thinking' }>;
type EventEntry = Extract<SessionEntry, { kind: 'event' }>;
export type SessionBlock =
	| Exclude<SessionEntry, EventEntry>
	| { kind: 'steps'; key: string; entries: StepEntry[]; tools: SessionTool[]; commands: boolean; failed: number }
	| { kind: 'subagent'; key: string; tool: SessionTool; status: ToolStatus; blocks: SessionBlock[] }
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
// OpenCode events read here; each carries its content in `part`.
const openCodeParts = new Set(['step_start', 'step_finish', 'text', 'reasoning', 'tool_use']);
// Claude Code tools that start a subagent.
const subagentTools = new Set(['Task', 'Agent']);

export function parseAgentSession(chunks: SessionChunk[]): SessionEntry[] {
	const root: SessionEntry[] = [];
	// Where the current line's entries go: the transcript, or a subagent's call.
	let entries = root;
	const tools = new Map<string, SessionTool>();
	const push = (entry: SessionEntry) => entries.push(entry);
	// Claude Code tasks (subagents, background commands) by task id.
	const tasks = new Map<string, SessionTool>();
	// Subagents that keep working after their call returned, until they end.
	const background = new Set<SessionTool>();
	// Groups made without their call: named when Claude Code says what was asked.
	const unnamed = new Set<SessionTool>();
	// Those groups, until the line with their call arrives after all.
	const callless = new Set<SessionTool>();
	// A finished subagent has nothing still running in it: a call left without
	// a result (its line was too large to record) is finished without one.
	const settle = (tool: SessionTool) => {
		for (const entry of tool.children ?? []) {
			if (entry.kind !== 'tool') continue;
			if (entry.tool.status === 'running') entry.tool.status = 'done';
			background.delete(entry.tool);
			settle(entry.tool);
		}
	};
	const subagent = (id: string, line: { key: string; at?: string }, list: SessionEntry[]) => {
		const tool: SessionTool = {
			id,
			name: 'Agent',
			title: 'Agent',
			input: '',
			status: 'running',
			command: false,
			started: line.at,
			children: []
		};
		tools.set(id, tool);
		unnamed.add(tool);
		callless.add(tool);
		list.push({ kind: 'tool', key: `${line.key}:agent`, tool });
		return tool;
	};
	// Reads a `task_*` event into the call it is about; false when there is none.
	const taskEvent = (event: Json, line: { key: string; at?: string }) => {
		const id = str(event.tool_use_id);
		const isAgent = event.task_type === 'local_agent' || !!str(event.subagent_type);
		let tool = tools.get(id) ?? tasks.get(str(event.task_id));
		if (!tool && isAgent && id)
			tool = subagent(
				id,
				line,
				(tools.get(str(event.parent_tool_use_id)) ?? tasks.get(str(event.parent_task_id)))?.children ?? root
			);
		if (!tool) return false;
		if (str(event.task_id)) tasks.set(str(event.task_id), tool);
		if (isAgent) {
			tool.children ??= [];
			// Only the start says what was asked; progress describes the moment.
			if (unnamed.has(tool) && event.subtype === 'task_started') {
				const description = truncate(str(event.description));
				if (description) {
					tool.title = description;
					unnamed.delete(tool);
				}
				tool.input ||= str(event.prompt);
			}
		}
		if (event.subtype === 'task_started' && isAgent && event.is_backgrounded === true && tool.status !== 'failed') {
			background.add(tool);
			tool.status = 'running';
			tool.ended = undefined;
		} else if (event.subtype === 'task_notification') {
			// The call's own result decides when there is one; a subagent stopped
			// with the run is not a failure.
			if (tool.status === 'running') {
				tool.status = event.status === 'failed' ? 'failed' : 'done';
				tool.ended = line.at;
			}
			background.delete(tool);
			settle(tool);
		}
		return true;
	};
	// OpenCode stamps every event; the first one is where its run started.
	let started: number | undefined;
	for (const line of lines(chunks)) {
		entries = root;
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
		if (type === 'system' && str(event.subtype).startsWith('task_') && taskEvent(event, line)) continue;
		// A subagent's event goes under the call that started it; when that call
		// was never recorded, its group starts here.
		const parentId = str(event.parent_tool_use_id);
		const parent = parentId ? (tools.get(parentId) ?? subagent(parentId, line, root)) : undefined;
		if (parent) {
			entries = parent.children ??= [];
			// A subagent left running after its call returned is still at work.
			if (parent.status !== 'running' && line.at) parent.ended = line.at;
		}
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
					// The group was made before this line arrived: it stays where it
					// is, with what it holds, and takes what the call says.
					const made = tools.get(str(part.id));
					if (made && callless.delete(made)) {
						unnamed.delete(made);
						made.name = name;
						made.title = truncate(description || (argument ? `${name}: ${argument}` : name));
						made.input = inputText(part.input);
						return;
					}
					const tool: SessionTool = {
						id: str(part.id) || key,
						name,
						title: truncate(description || (argument ? `${name}: ${argument}` : name)),
						input: inputText(part.input),
						status: 'running',
						command: name === 'Bash',
						started: line.at,
						...(subagentTools.has(name) ? { children: [] } : {})
					};
					tools.set(tool.id, tool);
					push({ kind: 'tool', key, tool });
				} else if (part.type === 'tool_result') {
					const tool = tools.get(str(part.tool_use_id));
					if (!tool) return;
					tool.output = resultText(part.content);
					// A background subagent's call returns at once; it ends later.
					if (background.has(tool) && part.is_error !== true) return;
					tool.status = part.is_error === true ? 'failed' : 'done';
					tool.ended = line.at;
					settle(tool);
				}
			});
		} else if (type === 'result') {
			const failed = event.is_error === true || (typeof event.subtype === 'string' && event.subtype !== 'success');
			// The run is over: a call whose result was never recorded is finished
			// too, without one. A subagent's time ends with the run.
			for (const tool of tools.values()) {
				if (tool.status !== 'running') continue;
				tool.status = 'done';
				if (tool.children) tool.ended = line.at;
			}
			background.clear();
			push({
				kind: 'result',
				key: line.key,
				failed,
				ms: num(event.duration_ms),
				...(failed && str(event.result) ? { text: str(event.result) } : {}),
				...(failed && isUsageLimitText(str(event.result)) ? { usageLimit: true } : {})
			});
		}
		// ── OpenCode run --format json
		else if (openCodeParts.has(type) && isObject(event.part)) {
			started ??= num(event.timestamp);
			openCodePart(event.part, type, { key: line.key, at: line.at, ms: elapsedSince(started, event) }, entries, tools);
		} else if (type === 'error' && isObject(event.error) && (isObject(event.error.data) || str(event.error.name))) {
			const data = isObject(event.error.data) ? event.error.data : {};
			const message = str(data.message) || str(event.error.message) || str(event.error.name) || type;
			// The provider refusing work until a quota resets ends the run as a pause.
			if (isUsageLimitText(message))
				push({
					kind: 'result',
					key: line.key,
					failed: true,
					ms: elapsedSince(started, event),
					text: message,
					usageLimit: true
				});
			else push({ kind: 'error', key: line.key, message });
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
	return root;
}

// Recognized events that legitimately add nothing (a result paired to an
// earlier call, an empty text block, a todo list shown as the plan).
function handled(type: string, event: Json) {
	if (type === 'assistant' || type === 'user' || type === 'result' || type === 'thread.started') return true;
	if (type === 'system' && event.subtype === 'init') return true;
	if (openCodeParts.has(type) && isObject(event.part)) return true;
	return type.startsWith('item.') && isObject(event.item);
}

const elapsedSince = (started: number | undefined, event: Json) => {
	const now = num(event.timestamp);
	return started !== undefined && now !== undefined && now >= started ? now - started : undefined;
};
// OpenCode's own clock (epoch milliseconds) for a call, when it recorded one.
const instant = (value: unknown) => {
	const ms = num(value);
	// Outside what a date can hold there is no time to show.
	return ms === undefined || Number.isNaN(new Date(ms).getTime()) ? undefined : new Date(ms).toISOString();
};
// A clock value OpenCode recorded that is not a usable time.
const badInstant = (value: unknown) => value !== undefined && value !== null && instant(value) === undefined;
// Calls OpenCode reported without an id, matched by what they run instead.
const anonymousCalls = new WeakSet<SessionTool>();

function openCodePart(
	part: Json,
	type: string,
	line: { key: string; at?: string; ms?: number },
	entries: SessionEntry[],
	tools: Map<string, SessionTool>
) {
	switch (type) {
		case 'text': {
			const text = str(part.text).trim();
			if (text) entries.push({ kind: 'text', key: line.key, text, at: line.at });
			return;
		}
		case 'reasoning': {
			const time = isObject(part.time) ? part.time : {};
			const from = num(time.start);
			const to = num(time.end);
			entries.push({
				kind: 'thinking',
				key: line.key,
				ms: from !== undefined && to !== undefined && to >= from ? to - from : undefined,
				text: str(part.text).trim() || undefined
			});
			return;
		}
		case 'tool_use': {
			const state = isObject(part.state) ? part.state : {};
			const name = str(part.tool) || 'tool';
			const command = name === 'bash';
			const input = isObject(state.input) ? state.input : {};
			const title = str(state.title).trim();
			const argument = primaryArgument(input);
			const status = str(state.status);
			const exit = isObject(state.metadata) ? num(state.metadata.exit) : undefined;
			const failed = status === 'error' || (exit !== undefined && exit !== 0);
			const next: ToolStatus = failed ? 'failed' : status === 'completed' ? 'done' : 'running';
			const clock = isObject(state.time) ? state.time : {};
			const known = str(part.callID) || str(part.id);
			const id = known || line.key;
			const text = inputText(state.input);
			// Without an id, an update belongs to the latest unfinished call of
			// the same tool and input (a pending call may not have its input yet).
			const existing = known
				? tools.get(known)
				: entries.findLast(
						(entry): entry is Extract<SessionEntry, { kind: 'tool' }> =>
							entry.kind === 'tool' &&
							anonymousCalls.has(entry.tool) &&
							entry.tool.status === 'running' &&
							entry.tool.name === name &&
							(entry.tool.input === text || !entry.tool.input || entry.tool.input === '{}')
					)?.tool;
			// A finished call stays as it ended, whatever arrives after it.
			if (existing && existing.status !== 'running' && next === 'running') return;
			const untimed = badInstant(clock.start) || badInstant(clock.end);
			const fields = {
				name,
				// OpenCode titles a call itself: the command's description, the file.
				title: truncate(command ? title || argument : title || argument ? `${name}: ${title || argument}` : '') || name,
				input: text,
				output: resultText(state.output) || resultText(state.error) || existing?.output,
				status: next,
				command,
				started: untimed ? undefined : (instant(clock.start) ?? existing?.started ?? line.at),
				ended: untimed || next === 'running' ? undefined : (instant(clock.end) ?? line.at)
			};
			if (existing) {
				Object.assign(existing, fields);
				return;
			}
			const created: SessionTool = { id, ...fields };
			if (known) tools.set(id, created);
			else anonymousCalls.add(created);
			entries.push({ kind: 'tool', key: line.key, tool: created });
			return;
		}
		case 'step_finish': {
			// A step that ends in tool calls continues; the one that stops is the
			// end of the run, and one cut short (length, content-filter) failed.
			const reason = str(part.reason);
			if (reason === 'stop') entries.push({ kind: 'result', key: line.key, failed: false, ms: line.ms });
			else if (reason === 'length' || reason === 'content-filter' || reason === 'error')
				entries.push({ kind: 'result', key: line.key, failed: true, ms: line.ms, text: reason });
			return;
		}
	}
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

// A subagent failed when its call did. Once it has answered or ended, the
// calls it had are settled, so it is running only while its call is, or
// while it makes new calls after returning (one left in the background).
function subagentStatus(tool: SessionTool): ToolStatus {
	if (tool.status === 'failed') return 'failed';
	const busy = (entries: SessionEntry[]): boolean =>
		entries.some(
			(entry) => entry.kind === 'tool' && (entry.tool.status === 'running' || busy(entry.tool.children ?? []))
		);
	return tool.status === 'running' || busy(tool.children ?? []) ? 'running' : 'done';
}

/**
 * Folds consecutive tool calls (and the thinking between them) into one group;
 * a call that started a subagent is its own group holding what the subagent did.
 */
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
		if (entry.kind === 'tool' && entry.tool.children) {
			if (run.length) flush();
			blocks.push({
				kind: 'subagent',
				key: entry.key,
				tool: entry.tool,
				status: subagentStatus(entry.tool),
				blocks: groupSession(entry.tool.children)
			});
			continue;
		}
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

// The provider's usage limit (Claude Code's 5-hour or weekly limit, Codex's
// usage limit, an OpenCode provider's quota): the task pauses and resumes after the reset, so the result is
// shown as a pause rather than as an error. Mirrors BE domain/usage_limit.go.
const usageLimitText =
	/you've hit your [\w-]*\s*(?:usage |weekly |session )?limit|usage limit reached|usage_limit_reached|limit will reset at/i;
export function isUsageLimitText(text: string): boolean {
	return usageLimitText.test(text);
}
