import assert from 'node:assert/strict';
import test from 'node:test';
import { formatSize, promptSections } from '../../src/lib/features/assistant/run-prompt.ts';

test('sections keep the order the prompt was assembled in, with their size', () => {
	const sections = promptSections({
		run_id: 'run-1',
		recorded: true,
		worktree_read_only: true,
		sources: [
			{ source: 'workflow_step', channel: 'prompt', text: 'List the changes, then implement them.' },
			{ source: 'profile', channel: 'instructions_file', text: 'Do not implement the steps yourself.' },
			{ source: 'plan', channel: 'prompt', text: '' },
			{ source: 'task', channel: 'prompt', text: 'è'.repeat(600), truncated: true }
		]
	});
	assert.deepEqual(
		sections.map((section) => section.source),
		['workflow_step', 'profile', 'task']
	);
	assert.equal(sections[0].bytes, 38);
	assert.equal(sections[2].bytes, 1200);
	assert.equal(sections[2].truncated, true);
	assert.equal(sections[1].channel, 'instructions_file');
	assert.equal(new Set(sections.map((section) => section.key)).size, 3);
});

test('a prompt that was not recorded has no sections', () => {
	assert.deepEqual(promptSections(null), []);
	assert.deepEqual(promptSections({ run_id: 'run-1', recorded: false, worktree_read_only: false, sources: null }), []);
});

test('sizes read in bytes and kilobytes', () => {
	assert.equal(formatSize(38), '38 B');
	assert.equal(formatSize(1200), '1.2 kB');
	assert.equal(formatSize(64000), '64 kB');
});
