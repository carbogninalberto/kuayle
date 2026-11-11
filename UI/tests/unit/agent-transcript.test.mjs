import assert from 'node:assert/strict';
import test from 'node:test';
import {
	groupSession,
	isUsageLimitText,
	outputPreview,
	parseAgentSession
} from '../../src/lib/features/assistant/agent-transcript.ts';

// One stored log chunk per native output line, as the manager records them.
let id = 0;
const chunk = (value, at = '2026-11-01T10:00:00Z', extra = {}) => ({
	id: ++id,
	stream: 'stdout',
	content: (typeof value === 'string' ? value : JSON.stringify(value)) + '\n',
	truncated: false,
	created_at: at,
	...extra
});

const claudeStream = () => [
	chunk({
		type: 'system',
		subtype: 'init',
		cwd: '/workspace/repo',
		session_id: 's-1',
		model: 'claude-opus-5-5',
		claude_code_version: '2.3.1',
		tools: ['Bash', 'Read']
	}),
	chunk({
		type: 'assistant',
		message: { content: [{ type: 'thinking', thinking: '', signature: 'SECRET-SIGNATURE' }] },
		thinking_duration_ms: 2400
	}),
	chunk(
		{
			type: 'assistant',
			message: {
				id: 'msg_1',
				usage: { input_tokens: 3, cache_read_input_tokens: 9000 },
				content: [
					{
						type: 'tool_use',
						id: 'toolu_1',
						name: 'Bash',
						input: { command: 'cat AGENTS/SKILLS.md', description: 'Read skills index and tools' }
					}
				]
			},
			request_id: 'req_1'
		},
		'2026-11-01T10:00:01Z'
	),
	chunk(
		{
			type: 'assistant',
			message: {
				content: [
					{
						type: 'tool_use',
						id: 'toolu_2',
						name: 'Bash',
						input: {
							command: 'npm test -- --grep "very long command that keeps going and going beyond the limit of a chip"'
						}
					}
				]
			}
		},
		'2026-11-01T10:00:01Z'
	),
	chunk(
		{
			type: 'user',
			message: {
				content: [
					{ tool_use_id: 'toolu_1', type: 'tool_result', content: '# Skill index\n<b>bold</b>', is_error: false }
				]
			},
			tool_use_result: { stdout: '# Skill index', stderr: '', interrupted: false }
		},
		'2026-11-01T10:00:04Z'
	),
	chunk(
		{
			type: 'user',
			message: {
				content: [
					{
						tool_use_id: 'toolu_2',
						type: 'tool_result',
						content: [{ type: 'text', text: '1 failing' }],
						is_error: true
					}
				]
			}
		},
		'2026-11-01T10:00:09Z'
	),
	chunk('[Oversized output line omitted]', '2026-11-01T10:00:10Z', { truncated: true }),
	chunk({
		type: 'assistant',
		message: { content: [{ type: 'text', text: 'Up to date with main; next migration is **000116**.' }] }
	}),
	chunk({ type: 'rate_limit_event', rate_limit_info: { status: 'allowed' } }),
	chunk({ type: 'result', subtype: 'success', is_error: false, duration_ms: 65000, result: 'Done.' })
];

test('turns a Claude Code stream into a readable transcript', () => {
	const stream = claudeStream();
	const entries = parseAgentSession(stream);
	assert.deepEqual(
		entries.map((entry) => entry.kind),
		['header', 'thinking', 'tool', 'tool', 'omitted', 'text', 'result']
	);
	assert.deepEqual(
		{ ...entries[0], key: undefined },
		{ kind: 'header', key: undefined, model: 'claude-opus-5-5', cwd: '/workspace/repo', version: 'Claude Code 2.3.1' }
	);
	// Thinking shows only its duration; the signature never reaches the view.
	assert.equal(entries[1].ms, 2400);
	assert.ok(!JSON.stringify(entries).includes('SECRET-SIGNATURE'));

	const [first, second] = [entries[2].tool, entries[3].tool];
	// The description is the chip; results are paired by tool_use_id.
	assert.equal(first.title, 'Read skills index and tools');
	assert.equal(first.input, 'cat AGENTS/SKILLS.md');
	assert.equal(first.output, '# Skill index\n<b>bold</b>');
	assert.equal(first.status, 'done');
	assert.equal(first.started, '2026-11-01T10:00:01Z');
	assert.equal(first.ended, '2026-11-01T10:00:04Z');
	assert.equal(first.command, true);
	// Without a description: the tool name and the truncated command.
	assert.match(second.title, /^Bash: npm test -- --grep "very long/);
	assert.ok(second.title.length <= 90, second.title);
	assert.ok(second.title.endsWith('…'));
	assert.equal(second.status, 'failed');
	assert.equal(second.output, '1 failing');

	assert.equal(entries[4].reason, 'oversized');
	assert.equal(entries[5].text, 'Up to date with main; next migration is **000116**.');
	assert.deepEqual({ ...entries[6], key: undefined }, { kind: 'result', key: undefined, failed: false, ms: 65000 });
	// Keys are stable across re-parses so the live view keeps its open chips.
	assert.deepEqual(
		parseAgentSession([...stream, chunk('more output')])
			.slice(0, -1)
			.map((entry) => entry.key),
		entries.map((entry) => entry.key)
	);
});

test('a tool still waiting for its result is running; a missing one never invents output', () => {
	const entries = parseAgentSession([
		chunk({
			type: 'assistant',
			message: { content: [{ type: 'tool_use', id: 't9', name: 'Read', input: { file_path: 'src/app.ts' } }] }
		})
	]);
	assert.equal(entries[0].tool.status, 'running');
	assert.equal(entries[0].tool.output, undefined);
	assert.equal(entries[0].tool.title, 'Read: src/app.ts');
	assert.equal(entries[0].tool.command, false);
});

test('groups consecutive tool calls; leading thinking stays a visible marker', () => {
	const blocks = groupSession(parseAgentSession(claudeStream()));
	assert.deepEqual(
		blocks.map((block) => block.kind),
		['header', 'thinking', 'steps', 'omitted', 'text', 'result']
	);
	const steps = blocks[2];
	assert.equal(steps.tools.length, 2);
	assert.equal(steps.commands, true);
	assert.equal(steps.failed, 1);
	assert.deepEqual(
		steps.entries.map((entry) => entry.kind),
		['tool', 'tool']
	);
	// Thinking between calls stays inside the group.
	const between = groupSession(
		parseAgentSession([
			chunk({
				type: 'assistant',
				message: { content: [{ type: 'tool_use', id: 'a', name: 'Bash', input: { command: 'ls' } }] }
			}),
			chunk({
				type: 'assistant',
				message: { content: [{ type: 'thinking', thinking: '' }] },
				thinking_duration_ms: 10
			}),
			chunk({
				type: 'assistant',
				message: { content: [{ type: 'tool_use', id: 'b', name: 'Bash', input: { command: 'pwd' } }] }
			})
		])
	);
	assert.deepEqual(
		between.map((block) => block.kind),
		['steps']
	);
	assert.deepEqual(
		between[0].entries.map((entry) => entry.kind),
		['tool', 'thinking', 'tool']
	);
	// A single tool call stays a chip of its own; lone thinking stays a marker.
	const single = groupSession(
		parseAgentSession([
			chunk({
				type: 'assistant',
				message: { content: [{ type: 'thinking', thinking: '' }] },
				thinking_duration_ms: 10
			}),
			chunk({ type: 'assistant', message: { content: [{ type: 'text', text: 'Hi' }] } }),
			chunk({
				type: 'assistant',
				message: { content: [{ type: 'tool_use', id: 'a', name: 'Grep', input: { pattern: 'x' } }] }
			})
		])
	);
	assert.deepEqual(
		single.map((block) => block.kind),
		['thinking', 'text', 'tool']
	);
});

test('reads Codex JSON events with the same entries', () => {
	const entries = parseAgentSession([
		chunk({ type: 'thread.started', thread_id: 'thread-1' }),
		chunk({ type: 'turn.started' }),
		chunk({ type: 'item.completed', item: { id: 'item_0', type: 'reasoning', text: '**Inspecting** the router' } }),
		chunk(
			{
				type: 'item.started',
				item: {
					id: 'item_1',
					type: 'command_execution',
					command: "bash -lc 'rg -n search src'",
					aggregated_output: '',
					status: 'in_progress'
				}
			},
			'2026-11-01T10:00:00Z'
		),
		chunk(
			{
				type: 'item.completed',
				item: {
					id: 'item_1',
					type: 'command_execution',
					command: "bash -lc 'rg -n search src'",
					aggregated_output: 'src/a.ts:1:search',
					exit_code: 0,
					status: 'completed'
				}
			},
			'2026-11-01T10:00:03Z'
		),
		chunk({
			type: 'item.completed',
			item: {
				id: 'item_2',
				type: 'command_execution',
				command: 'npm test',
				aggregated_output: 'FAIL',
				exit_code: 1,
				status: 'failed'
			}
		}),
		chunk({
			type: 'item.completed',
			item: {
				id: 'item_3',
				type: 'file_change',
				changes: [
					{ path: 'src/a.ts', kind: 'update' },
					{ path: 'src/b.ts', kind: 'add' }
				],
				status: 'completed'
			}
		}),
		chunk({ type: 'item.completed', item: { id: 'item_4', type: 'agent_message', text: 'Added the search.' } }),
		chunk({ type: 'item.updated', item: { id: 'item_5', type: 'todo_list', items: [] } }),
		chunk({ type: 'turn.completed', usage: { input_tokens: 10 } }),
		chunk({ type: 'turn.failed', error: { message: 'stream disconnected' } })
	]);
	assert.deepEqual(
		entries.map((entry) => entry.kind),
		['header', 'thinking', 'tool', 'tool', 'tool', 'text', 'error']
	);
	assert.equal(entries[0].session, 'thread-1');
	assert.equal(entries[1].text, '**Inspecting** the router');
	const command = entries[2].tool;
	assert.equal(command.title, 'rg -n search src');
	assert.equal(command.input, "bash -lc 'rg -n search src'");
	assert.equal(command.output, 'src/a.ts:1:search');
	assert.equal(command.status, 'done');
	assert.equal(command.started, '2026-11-01T10:00:00Z');
	assert.equal(command.ended, '2026-11-01T10:00:03Z');
	assert.equal(entries[3].tool.status, 'failed');
	assert.equal(entries[4].tool.title, 'src/a.ts, src/b.ts');
	assert.equal(entries[4].tool.input, 'update src/a.ts\nadd src/b.ts');
	assert.equal(entries[5].text, 'Added the search.');
	assert.equal(entries[6].message, 'stream disconnected');
});

test('unknown events and plain output fall back to escaped text, never dropped silently', () => {
	const entries = parseAgentSession([
		chunk('Starting harness <script>alert(1)</script>'),
		chunk('second line'),
		chunk({ type: 'system', subtype: 'hook_response', output: 'ok' }),
		chunk({ type: 'mystery', payload: 1 }),
		chunk('{"type": broken json'),
		chunk('warn: something', undefined, { stream: 'stderr' }),
		chunk('[Output limit reached]', undefined, { truncated: true })
	]);
	assert.deepEqual(
		entries.map((entry) => entry.kind),
		['plain', 'event', 'event', 'plain', 'plain', 'omitted']
	);
	assert.equal(entries[0].text, 'Starting harness <script>alert(1)</script>\nsecond line');
	assert.equal(entries[1].type, 'system.hook_response');
	assert.equal(entries[2].type, 'mystery');
	assert.match(entries[2].raw, /"payload":1/);
	assert.equal(entries[3].text, '{"type": broken json');
	assert.equal(entries[4].stream, 'stderr');
	assert.equal(entries[5].reason, 'limit');
	// Consecutive unknown events collapse into one block.
	const blocks = groupSession(entries);
	assert.deepEqual(
		blocks.map((block) => block.kind),
		['plain', 'events', 'plain', 'plain', 'omitted']
	);
	assert.equal(blocks[1].events.length, 2);
});

test('a line split across chunks is joined before parsing', () => {
	const event = JSON.stringify({ type: 'assistant', message: { content: [{ type: 'text', text: 'Joined' }] } });
	const entries = parseAgentSession([
		{ id: 1, stream: 'stdout', content: event.slice(0, 20), truncated: false, created_at: '2026-11-01T10:00:00Z' },
		{ id: 2, stream: 'stdout', content: event.slice(20) + '\n', truncated: false, created_at: '2026-11-01T10:00:00Z' }
	]);
	assert.deepEqual(
		entries.map((entry) => entry.kind),
		['text']
	);
	assert.equal(entries[0].text, 'Joined');
});

test('long output is previewed with the rest behind "show more"', () => {
	const short = outputPreview('a\nb');
	assert.deepEqual(short, { preview: 'a\nb', hidden: 0 });
	const long = outputPreview(Array.from({ length: 40 }, (_, i) => `line ${i + 1}`).join('\n'));
	assert.equal(long.preview.split('\n').length, 12);
	assert.equal(long.hidden, 28);
	const wide = outputPreview('x'.repeat(5000));
	assert.equal(wide.preview.length, 2000);
	assert.ok(wide.hidden > 0);
});

// Production (KUA-98): Claude Code stopped at its 5-hour limit. The chat
// shows a pause at the usage limit, with the provider's message, rather than
// "Stopped with an error".
test('a result refused at the usage limit is a pause, not an error', () => {
	const message = "You've hit your 5-hour limit · resets 4:10am (UTC)";
	const [limited] = parseAgentSession([
		chunk({ type: 'result', subtype: 'success', is_error: true, duration_ms: 7563208, result: message })
	]);
	assert.equal(limited.kind, 'result');
	assert.equal(limited.failed, true);
	assert.equal(limited.usageLimit, true);
	assert.equal(limited.text, message);
	const [failed] = parseAgentSession([
		chunk({ type: 'result', subtype: 'error_during_execution', is_error: true, result: '2 tests failed' })
	]);
	assert.equal(failed.usageLimit, undefined);
	for (const text of [
		message,
		"You've hit your weekly limit · resets Thu 9am (UTC)",
		"You've hit your usage limit. Upgrade to Pro or try again in 2 hours 15 minutes.",
		'Claude AI usage limit reached|1794283800'
	])
		assert.equal(isUsageLimitText(text), true, text);
	assert.equal(isUsageLimitText('the rate limiter test expects 429'), false);
});

// OpenCode `run --format json`: one event per line, each with an epoch
// `timestamp`, the `sessionID` and usually a `part`. Hand-built with the
// field names of recorded runs and invented content.
const ocStart = 1794300000000;
const ocIds = { sessionID: 'ses_example0001', messageID: 'msg_example0001' };
const ocEvent = (type, offset, part) =>
	chunk({
		type,
		timestamp: ocStart + offset,
		sessionID: ocIds.sessionID,
		...(part ? { part: { id: `prt_${offset}`, ...ocIds, ...part } } : {})
	});
const ocStep = (offset) => ocEvent('step_start', offset, { snapshot: 'a'.repeat(40), type: 'step-start' });
const ocStepEnd = (offset, reason) =>
	ocEvent('step_finish', offset, {
		reason,
		snapshot: 'a'.repeat(40),
		type: 'step-finish',
		tokens: { total: 1200, input: 1000, output: 150, reasoning: 50, cache: { write: 0, read: 800 } },
		cost: 0.0012
	});
const ocTool = (offset, tool, callID, state) => ocEvent('tool_use', offset, { type: 'tool', tool, callID, state });
const openCodeRun = () => [
	ocStep(0),
	ocEvent('reasoning', 900, {
		type: 'reasoning',
		text: 'The list needs a filter first.',
		time: { start: ocStart + 100, end: ocStart + 900 }
	}),
	ocEvent('text', 1000, {
		type: 'text',
		text: 'I will add the **search** filter.',
		time: { start: ocStart + 900, end: ocStart + 1000 }
	}),
	ocTool(4500, 'bash', 'call_1', {
		status: 'completed',
		input: { command: 'npm test', description: 'Run the unit tests' },
		output: '12 passing\n',
		metadata: { output: '12 passing\n', exit: 0, truncated: false },
		title: 'Run the unit tests',
		time: { start: ocStart + 2000, end: ocStart + 4500 }
	}),
	ocTool(5200, 'edit', 'call_2', {
		status: 'completed',
		input: { filePath: '/workspace/repo/src/search.ts', oldString: 'a', newString: 'b' },
		output: 'Edit applied successfully.',
		metadata: { diagnostics: {}, diff: '-a\n+b\n', filediff: {}, truncated: false },
		title: 'src/search.ts',
		time: { start: ocStart + 5000, end: ocStart + 5200 }
	}),
	ocTool(5300, 'bash', 'call_3', {
		status: 'error',
		input: { command: 'git push' },
		error: 'The user rejected permission to use this specific tool call.',
		time: { start: ocStart + 5250, end: ocStart + 5300 }
	}),
	ocStepEnd(5400, 'tool-calls'),
	ocStep(5500),
	ocTool(6500, 'bash', 'call_4', {
		status: 'completed',
		input: { command: 'npm run lint' },
		output: '2 problems\n',
		metadata: { output: '2 problems\n', exit: 2, truncated: false },
		title: 'npm run lint',
		time: { start: ocStart + 6000, end: ocStart + 6500 }
	}),
	ocStepEnd(6600, 'tool-calls'),
	ocStep(6700),
	ocEvent('text', 8000, {
		type: 'text',
		text: 'Search is in place.\n\n- filter added',
		time: { start: ocStart + 7000, end: ocStart + 8000 }
	}),
	ocStepEnd(9000, 'stop')
];
const ocError = (message, extra = {}) =>
	chunk({
		type: 'error',
		timestamp: ocStart + 9000,
		sessionID: ocIds.sessionID,
		error: {
			name: 'APIError',
			data: {
				message,
				statusCode: 429,
				isRetryable: true,
				responseHeaders: { 'retry-after': '30' },
				responseBody: JSON.stringify({ error: { type: 'server_error', message } }),
				metadata: { url: 'https://provider.example/v1/messages' },
				...extra
			}
		}
	});

test('reads OpenCode JSON events with the same entries', () => {
	const entries = parseAgentSession(openCodeRun());
	assert.deepEqual(
		entries.map((entry) => entry.kind),
		['thinking', 'text', 'tool', 'tool', 'tool', 'tool', 'text', 'result']
	);
	assert.equal(entries[0].text, 'The list needs a filter first.');
	assert.equal(entries[0].ms, 800);
	assert.equal(entries[1].text, 'I will add the **search** filter.');
	assert.equal(entries[6].text, 'Search is in place.\n\n- filter added');
	// The run ends with the step that stopped; its time is the run's own clock.
	assert.deepEqual(
		{ failed: entries[7].failed, ms: entries[7].ms, text: entries[7].text },
		{ failed: false, ms: 9000, text: undefined }
	);
});

test('an OpenCode tool call carries its input, result, status and own duration', () => {
	const tools = parseAgentSession(openCodeRun()).flatMap((entry) => (entry.kind === 'tool' ? [entry.tool] : []));
	const [test_, edit, rejected, lint] = tools;
	assert.deepEqual(
		{ ...test_, started: undefined, ended: undefined },
		{
			id: 'call_1',
			name: 'bash',
			title: 'Run the unit tests',
			input: 'npm test',
			output: '12 passing\n',
			status: 'done',
			command: true,
			started: undefined,
			ended: undefined
		}
	);
	// OpenCode times the call itself; the chunk's recording time is not used.
	assert.equal(Date.parse(test_.ended) - Date.parse(test_.started), 2500);
	assert.equal(edit.title, 'edit: src/search.ts');
	assert.equal(edit.command, false);
	assert.match(edit.input, /"filePath": "\/workspace\/repo\/src\/search.ts"/);
	assert.equal(edit.output, 'Edit applied successfully.');
	// A failed call: its error is the result.
	assert.equal(rejected.status, 'failed');
	assert.equal(rejected.title, 'git push');
	assert.equal(rejected.output, 'The user rejected permission to use this specific tool call.');
	assert.equal(Date.parse(rejected.ended) - Date.parse(rejected.started), 50);
	// A command that exited non-zero failed, as for the other harnesses.
	assert.equal(lint.status, 'failed');
	assert.equal(lint.output, '2 problems\n');
	// One chip per call; consecutive calls fold like a Claude Code run.
	const blocks = groupSession(parseAgentSession(openCodeRun()));
	assert.deepEqual(
		blocks.map((block) => block.kind),
		['thinking', 'text', 'steps', 'text', 'result']
	);
	assert.equal(blocks[2].tools.length, 4);
	assert.equal(blocks[2].failed, 2);
});

test('an OpenCode call reported while running is one chip, completed in place', () => {
	const state = { input: { command: 'sleep 1' }, title: 'sleep 1', time: { start: ocStart + 100 } };
	const entries = parseAgentSession([
		ocTool(100, 'bash', 'call_1', { status: 'running', ...state }),
		ocTool(1100, 'bash', 'call_1', {
			status: 'completed',
			...state,
			output: '',
			metadata: { exit: 0 },
			time: { start: ocStart + 100, end: ocStart + 1100 }
		})
	]);
	assert.equal(entries.length, 1);
	assert.equal(entries[0].tool.status, 'done');
	assert.equal(Date.parse(entries[0].tool.ended) - Date.parse(entries[0].tool.started), 1000);
	const [running] = parseAgentSession([ocTool(100, 'bash', 'call_1', { status: 'running', ...state })]);
	assert.equal(running.tool.status, 'running');
	assert.equal(running.tool.ended, undefined);
});

test('an OpenCode error event shows the provider message', () => {
	const entries = parseAgentSession([ocStep(0), ocError('The model is busy right now.')]);
	assert.deepEqual(
		entries.map((entry) => entry.kind),
		['error']
	);
	assert.equal(entries[0].message, 'The model is busy right now.');
	// Without a message the error's name is what was recorded.
	const [named] = parseAgentSession([
		chunk({ type: 'error', timestamp: ocStart, sessionID: ocIds.sessionID, error: { name: 'UnknownError', data: {} } })
	]);
	assert.equal(named.message, 'UnknownError');
});

test('an OpenCode run stopped at the usage limit is a pause, not an error', () => {
	const message = "You've hit your usage limit. Upgrade to Pro or try again in 2 hours 15 minutes.";
	const entries = parseAgentSession([ocStep(0), ocError(message, { isRetryable: false })]);
	assert.deepEqual(
		entries.map((entry) => entry.kind),
		['result']
	);
	assert.deepEqual(
		{ ...entries[0], key: '' },
		{ kind: 'result', key: '', failed: true, ms: 9000, text: message, usageLimit: true }
	);
});

test('an OpenCode step cut short ends the run as failed; unknown OpenCode events stay generic', () => {
	const cut = parseAgentSession([ocStep(0), ocStepEnd(3000, 'length')]);
	assert.deepEqual(
		cut.map((entry) => ({ kind: entry.kind, failed: entry.failed, text: entry.text })),
		[{ kind: 'result', failed: true, text: 'length' }]
	);
	const entries = parseAgentSession([
		ocEvent('session_compacted', 10, { type: 'compaction', auto: true }),
		ocEvent('tool_use', 20),
		ocEvent('text', 30, { type: 'text', text: 'Still here.' })
	]);
	assert.deepEqual(
		entries.map((entry) => entry.kind),
		['event', 'event', 'text']
	);
	assert.equal(entries[0].type, 'session_compacted');
	assert.match(entries[0].raw, /"auto":true/);
	// A known type without the part it should carry is not guessed at.
	assert.equal(entries[1].type, 'tool_use');
});

test('OpenCode fields of the wrong shape never break the transcript', () => {
	// A clock outside what a date can hold: the call is shown without a time.
	const [untimed] = parseAgentSession([
		ocTool(100, 'bash', 'call_1', {
			status: 'completed',
			input: { command: 'ls' },
			output: 'a.txt',
			time: { start: 9e15, end: 1e300 }
		})
	]);
	assert.equal(untimed.tool.status, 'done');
	assert.equal(untimed.tool.output, 'a.txt');
	assert.equal(untimed.tool.started, undefined);
	assert.equal(untimed.tool.ended, undefined);
	const odd = [
		{ type: 'tool_use', timestamp: 'soon', part: { type: 'tool', tool: 7, callID: {}, state: 'done' } },
		{ type: 'tool_use', part: { state: { status: 3, input: [1], output: 5, title: {}, time: 'now', metadata: [] } } },
		{ type: 'tool_use', part: { state: { input: 'text', time: { start: 'a', end: null } } } },
		{ type: 'text', timestamp: -1e300, part: { text: { nested: true }, time: 4 } },
		{ type: 'reasoning', part: { text: 9, time: { start: 1e300, end: -1e300 } } },
		{ type: 'step_finish', timestamp: 1e300, part: { reason: ['stop'] } },
		{ type: 'step_start', part: [] },
		{ type: 'error', timestamp: {}, error: { name: 5, data: 'gone' } },
		{ type: 'error', error: { data: { message: { text: 'x' } } } }
	];
	assert.doesNotThrow(() => groupSession(parseAgentSession(odd.map((event) => chunk(event)))));
});

test('an OpenCode result that is not plain text is still shown', () => {
	const [listed, refused] = parseAgentSession([
		ocTool(100, 'glob', 'call_1', {
			status: 'completed',
			input: { pattern: '*.ts' },
			output: [
				{ type: 'text', text: 'a.ts' },
				{ type: 'text', text: 'b.ts' }
			],
			time: { start: ocStart, end: ocStart + 100 }
		}),
		ocTool(200, 'bash', 'call_2', {
			status: 'error',
			input: { command: 'make' },
			error: { name: 'ToolError', message: 'make is not installed' },
			time: { start: ocStart + 150, end: ocStart + 200 }
		})
	]).map((entry) => entry.tool);
	assert.equal(listed.output, 'a.ts\nb.ts');
	assert.equal(refused.status, 'failed');
	assert.match(refused.output, /"message": "make is not installed"/);
});

test('an OpenCode error without a name or message is still an error line', () => {
	const entries = parseAgentSession([
		chunk({ type: 'error', timestamp: ocStart, sessionID: ocIds.sessionID, error: { data: {} } })
	]);
	assert.deepEqual(
		entries.map((entry) => ({ kind: entry.kind, message: entry.message })),
		[{ kind: 'error', message: 'error' }]
	);
});

test('an OpenCode call is one chip without an id, and a finished chip is never reopened', () => {
	const anonymous = (offset, state) =>
		chunk({
			type: 'tool_use',
			timestamp: ocStart + offset,
			sessionID: ocIds.sessionID,
			part: { type: 'tool', tool: 'bash', ...ocIds, state }
		});
	const input = { command: 'npm test' };
	const entries = parseAgentSession([
		anonymous(0, { status: 'pending', input }),
		anonymous(100, { status: 'running', input, time: { start: ocStart + 100 } }),
		anonymous(900, { status: 'completed', input, output: 'ok', time: { start: ocStart + 100, end: ocStart + 900 } }),
		// A second, separate call of the same command after the first finished.
		anonymous(1000, { status: 'running', input, time: { start: ocStart + 1000 } })
	]);
	assert.deepEqual(
		entries.map((entry) => [entry.tool.status, entry.tool.output]),
		[
			['done', 'ok'],
			['running', undefined]
		]
	);
	// An update that arrives late leaves the finished call as it ended.
	const [late] = parseAgentSession([
		ocTool(900, 'bash', 'call_1', {
			status: 'error',
			input,
			error: 'exit 1',
			time: { start: ocStart + 100, end: ocStart + 900 }
		}),
		ocTool(950, 'bash', 'call_1', { status: 'running', input, time: { start: ocStart + 100 } })
	]);
	assert.equal(late.tool.status, 'failed');
	assert.equal(late.tool.output, 'exit 1');
	assert.equal(Date.parse(late.tool.ended) - Date.parse(late.tool.started), 800);
});

// Storage replaces any native line over 16 KB (usually a tool call with a
// large result) with a marker; the run around it reads as before.
test('an oversized line between OpenCode events is marked and the rest still parses', () => {
	const run = openCodeRun();
	const marker = chunk('[Oversized output line omitted]', undefined, { truncated: true });
	const entries = parseAgentSession([...run.slice(0, 4), marker, ...run.slice(5)]);
	assert.deepEqual(
		entries.map((entry) => entry.kind),
		['thinking', 'text', 'tool', 'omitted', 'tool', 'tool', 'text', 'result']
	);
	assert.equal(entries[3].reason, 'oversized');
	assert.equal(entries[2].tool.title, 'Run the unit tests');
	assert.equal(entries[4].tool.title, 'git push');
	assert.equal(entries[7].ms, 9000);
});
