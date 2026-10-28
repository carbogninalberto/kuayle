import assert from 'node:assert/strict';
import test from 'node:test';
import { fileTree, latestPlan, reasoningSummaries } from '../../src/lib/features/assistant/agent-trace.ts';

const codexPlan = (items) => JSON.stringify({ type: 'item.updated', item: { id: 't', type: 'todo_list', items } });

test('derives the latest plan from Codex todo_list and Claude TodoWrite events', () => {
	assert.equal(latestPlan(['plain text', '{"type":"turn.started"}']), null);
	const codex = latestPlan([
		codexPlan([{ text: 'Old', completed: false }]),
		'noise ' + codexPlan([{ text: 'ignored: not a JSON line', completed: true }]),
		codexPlan([
			{ text: 'Locate component', completed: true },
			{ text: 'Implement fix', completed: false },
			{ text: 'Add tests', completed: false }
		])
	]);
	assert.deepEqual(codex, [
		{ label: 'Locate component', status: 'done' },
		{ label: 'Implement fix', status: 'active' },
		{ label: 'Add tests', status: 'pending' }
	]);
	const claude = latestPlan([
		JSON.stringify({
			type: 'assistant',
			message: {
				content: [
					{ type: 'text', text: 'Planning' },
					{
						type: 'tool_use',
						name: 'TodoWrite',
						input: {
							todos: [
								{ content: 'Write test', status: 'completed' },
								{ content: 'Fix bug', status: 'in_progress' },
								{ content: '', status: 'pending' },
								{ content: 'Run checks', status: 'pending' }
							]
						}
					}
				]
			}
		})
	]);
	assert.deepEqual(claude, [
		{ label: 'Write test', status: 'done' },
		{ label: 'Fix bug', status: 'active' },
		{ label: 'Run checks', status: 'pending' }
	]);
});

test('shows only provider-supplied reasoning, bounded', () => {
	const lines = [
		JSON.stringify({ type: 'item.completed', item: { type: 'reasoning', text: 'Checking the router' } }),
		JSON.stringify({ type: 'item.completed', item: { type: 'agent_message', text: 'Not reasoning' } }),
		JSON.stringify({ type: 'assistant', message: { content: [{ type: 'thinking', thinking: 'x'.repeat(5000) }] } })
	];
	const summaries = reasoningSummaries(lines);
	assert.equal(summaries.length, 2);
	assert.equal(summaries[0], 'Checking the router');
	assert.equal(summaries[1].length, 2001);
	assert.deepEqual(reasoningSummaries(['{"type":"item.completed","item":{"type":"reasoning","text":"  "}}']), []);
});

test('nests changed files into folders without escaping the repository', () => {
	assert.deepEqual(fileTree(['src/b.ts', 'README.md', 'src/lib/a.ts', '../etc/passwd', 'src/b.ts']), [
		{ type: 'folder', name: 'etc', path: 'etc', children: [{ type: 'file', name: 'passwd', path: 'etc/passwd' }] },
		{
			type: 'folder',
			name: 'src',
			path: 'src',
			children: [
				{
					type: 'folder',
					name: 'lib',
					path: 'src/lib',
					children: [{ type: 'file', name: 'a.ts', path: 'src/lib/a.ts' }]
				},
				{ type: 'file', name: 'b.ts', path: 'src/b.ts' }
			]
		},
		{ type: 'file', name: 'README.md', path: 'README.md' }
	]);
});
