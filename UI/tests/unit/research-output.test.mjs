import assert from 'node:assert/strict';
import test from 'node:test';
import {
	httpURL,
	machineWorktreesOutput,
	repoScanOutput,
	subagentsOutput,
	taskWorktreeOutput,
	webSearchOutput
} from '../../src/lib/features/assistant/research-output.ts';

test('only http(s) result URLs are linked', () => {
	assert.equal(httpURL('https://example.com/a?b=1'), 'https://example.com/a?b=1');
	assert.equal(httpURL('http://example.com'), 'http://example.com/');
	assert.equal(httpURL('javascript:alert(1)'), '');
	assert.equal(httpURL('data:text/html,hi'), '');
	assert.equal(httpURL('not a url'), '');
	assert.equal(httpURL(42), '');
});

test('web search output keeps only well-formed results', () => {
	const output = webSearchOutput({
		query: 'svelte flow',
		provider: 'brave',
		results: [{ title: 'Svelte Flow', url: 'https://svelteflow.dev', excerpt: 'Node-based UIs' }, 'junk', null],
		truncated: true
	});
	assert.equal(output.query, 'svelte flow');
	assert.deepEqual(output.results, [
		{ title: 'Svelte Flow', url: 'https://svelteflow.dev', excerpt: 'Node-based UIs' }
	]);
	assert.equal(output.truncated, true);
	assert.equal(webSearchOutput('oops'), undefined);
});

test('a scan step names the repository, commit and the pattern it used', () => {
	const grep = repoScanOutput(
		'repo_grep',
		{ pattern: 'ReserveMachine' },
		{
			repository: 'acme/app',
			ref: 'main',
			commit: 'cddb21dc8edf6a27160f01ed80c28485426e17d4',
			pattern: 'ReserveMachine',
			matches: [
				{
					path: 'BE/internal/repository/agent_task.go',
					line: 504,
					text: 'func (r *AgentTaskRepository) ReserveMachine('
				}
			],
			files_searched: 900,
			files_matched: 1
		}
	);
	assert.equal(grep.repository, 'acme/app');
	assert.equal(grep.commit, 'cddb21dc8edf');
	assert.equal(grep.query, 'ReserveMachine');
	assert.deepEqual(grep.lines, [
		'BE/internal/repository/agent_task.go:504  func (r *AgentTaskRepository) ReserveMachine('
	]);
	assert.equal(grep.summary, '1 / 900');

	const read = repoScanOutput(
		'repo_read_file',
		{ path: 'README.md' },
		{
			repository: 'acme/app',
			commit: 'abc',
			path: 'README.md',
			start_line: 1,
			end_line: 2,
			total_lines: 10,
			content: 'a\nb',
			truncated: false
		}
	);
	assert.equal(read.query, 'README.md:1-2');
	assert.deepEqual(read.lines, ['a', 'b']);
	assert.equal(repoScanOutput('unknown_tool', {}, {}), undefined);
});

test('a live worktree result is read from its own fields', () => {
	const read = repoScanOutput(
		'repo_read_file',
		{ path: 'BE/main.go', start_line: 1 },
		{
			source: 'task',
			repository: 'acme/app',
			branch: 'kua-141-work',
			commit: 'abc',
			file: 'BE/main.go',
			start_line: 1,
			end_line: 2,
			total_lines: 9,
			content: 'a\nb'
		}
	);
	assert.equal(read.query, 'BE/main.go:1-2', 'the task result names its file as file');
	assert.equal(read.source, 'task');
	assert.equal(read.branch, 'kua-141-work');

	const grep = repoScanOutput(
		'repo_grep',
		{ pattern: 'Serve' },
		{
			source: 'task',
			repository: 'acme/app',
			branch: 'kua-141-work',
			pattern: 'Serve',
			matches: [{ path: 'BE/main.go', line: 7, text: 'Serve()' }]
		}
	);
	assert.equal(grep.summary, '1', 'a task grep counts matches, not searched files');
	assert.equal(grep.source, 'task');

	const glob = repoScanOutput('repo_glob', { pattern: '**/*.go' }, { source: 'task', files: ['BE/main.go'] });
	assert.deepEqual(glob.lines, ['BE/main.go'], 'a task glob returns plain paths');
});

test('worktree chips read their recorded state', () => {
	assert.deepEqual(taskWorktreeOutput({ branch: 'kua-141', dirty: true, changed: [' M a.go', '?? b.go'] }), {
		branch: 'kua-141',
		commit: '',
		dirty: true,
		changed: 2
	});
	assert.equal(taskWorktreeOutput('junk'), undefined);
	assert.deepEqual(
		machineWorktreesOutput({ worktrees: [{ path: '/workspace/tasks/a' }, { path: '/workspace/tasks/b' }] }),
		{
			count: 2
		}
	);
	assert.deepEqual(machineWorktreesOutput({ worktrees: [] }), { count: 0 });
	assert.equal(machineWorktreesOutput(null), undefined);
});

test('a machine listing and a task worktree are never read as each other', () => {
	// list_machine_worktrees: each worktree carries its own branch and commit.
	const listing = {
		machine: 'dev-1',
		repository: 'acme/app',
		worktrees: [{ path: '/workspace/tasks/a', branch: 'kua-141', commit: 'abc', dirty: true }]
	};
	assert.equal(taskWorktreeOutput(listing), undefined, 'a listing has no task branch of its own');
	assert.deepEqual(machineWorktreesOutput(listing), { count: 1 });

	// get_task_worktree: one task's branch and status, with no worktrees array.
	const task = {
		machine: 'dev-1',
		repository: 'acme/app',
		branch: 'kua-141',
		commit: 'abc',
		dirty: false,
		changed: [],
		ahead: 0,
		behind: 0,
		last_subject: ''
	};
	assert.deepEqual(taskWorktreeOutput(task), { branch: 'kua-141', commit: 'abc', dirty: false, changed: 0 });
	assert.equal(machineWorktreesOutput(task), undefined, 'a task status is not a listing');
	assert.equal(
		taskWorktreeOutput({ machine: 'dev-1', repository: 'acme/app' }),
		undefined,
		'a record with neither branch, commit nor dirty is not a worktree'
	);
});

test('listed files are read from their records, with why one was left out', () => {
	const scan = repoScanOutput(
		'repo_list_files',
		{ path: 'BE' },
		{
			repository: 'acme/app',
			commit: 'abc',
			path: 'BE',
			files: [
				{ path: 'BE/main.go', size: 120, binary: false },
				{ path: 'BE/big.sql', size: 9000000, omitted: 'too_large' },
				{ path: 'BE/logo.png', size: 40, binary: true },
				'not-a-record',
				{ size: 3 }
			],
			matched: 3,
			total_files: 340
		}
	);
	assert.deepEqual(scan.lines, ['BE/main.go', 'BE/big.sql  (too_large)', 'BE/logo.png  (binary)']);
	assert.equal(scan.summary, '3 / 340');
});

test('scan listings are capped so a large result cannot flood the transcript', () => {
	// As the backend sends them: one record per file.
	const files = Array.from({ length: 500 }, (_, index) => ({ path: `f${index}.go`, size: 10 }));
	const scan = repoScanOutput(
		'repo_glob',
		{ pattern: '**/*.go' },
		{ repository: 'acme/app', commit: 'abc', files, matched: 500 }
	);
	assert.equal(scan.lines.length, 50);
	assert.equal(scan.summary, '500');
});

test('each subagent is a row; a failed one keeps its error without hiding the others', () => {
	const output = subagentsOutput({
		completed: 1,
		failed: 1,
		duration_ms: 4200,
		subagents: [
			{
				index: 0,
				title: 'Scan UI',
				status: 'completed',
				model: 'small',
				duration_ms: 3000,
				summary: 'found it',
				files: ['UI/a.svelte'],
				citations: [{ url: 'https://x.dev', title: 'X' }]
			},
			{
				index: 1,
				title: 'Search web',
				status: 'failed',
				model: 'small',
				duration_ms: 1200,
				error: 'rate limited',
				files: [],
				citations: []
			}
		]
	});
	assert.equal(output.subagents.length, 2);
	assert.equal(output.subagents[0].files[0], 'UI/a.svelte');
	assert.equal(output.subagents[1].status, 'failed');
	assert.equal(output.subagents[1].error, 'rate limited');
});
