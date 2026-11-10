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
