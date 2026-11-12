import assert from 'node:assert/strict';
import test from 'node:test';
import { missedNotebook, notebookDocument } from '../../src/lib/features/assistant/task-notebook.ts';

const at = (minute) => `2026-11-01T10:${String(minute).padStart(2, '0')}:00Z`;
const notebook = (over = {}) => ({
	plan: { text: '', steps: null },
	decisions: null,
	done: null,
	remaining: null,
	verify: null,
	findings: null,
	instructions: [{ source: 'request', author_id: 'user-1', text: 'Add search', at: at(0) }],
	events: null,
	...over
});
const view = (over = {}, history = []) => ({ task_id: 'task-1', revision: 3, notebook: notebook(over), history });

test('a notebook nobody wrote in yet is empty, and the request in the header is not repeated', () => {
	const document = notebookDocument(view(), 'Add search');
	assert.equal(document.empty, true);
	assert.deepEqual(document.people, []);
	assert.equal(document.progress, undefined);
	assert.equal(notebookDocument(null, 'Add search').empty, true);
});

test('the plan is a checklist that counts what is done; without steps the plan text stands in', () => {
	const document = notebookDocument(
		view({
			plan: {
				text: 'The whole plan',
				steps: [
					{ title: 'Endpoint', files: ['api/search.go'], commands: ['go test ./api'], acceptance: 'Tests pass', done: true },
					{ title: 'Search box' },
					{ title: 'Docs' }
				]
			}
		}),
		'Add search'
	);
	assert.deepEqual(document.progress, { done: 1, total: 3 });
	assert.equal(document.steps[0].done, true);
	assert.equal(document.planText, '');
	assert.equal(document.empty, false);
	const textOnly = notebookDocument(view({ plan: { text: 'The whole plan', steps: [] } }), 'Add search');
	assert.equal(textOnly.planText, 'The whole plan');
	assert.equal(textOnly.progress, undefined);
});

test("people's instructions stay apart from what agents wrote, and a different request is kept", () => {
	const document = notebookDocument(
		view({
			done: ['Endpoint added'],
			instructions: [
				{ source: 'request', author_id: 'user-1', text: 'Add search', at: at(0) },
				{ source: 'live_message', author_id: 'user-1', text: 'Keep the old route', at: at(5) }
			]
		}),
		'Add search'
	);
	assert.deepEqual(
		document.people.map((entry) => entry.text),
		['Keep the old route']
	);
	assert.deepEqual(document.done, [{ text: 'Endpoint added', recorded: false }]);
	const edited = notebookDocument(view(), 'Another request');
	assert.deepEqual(
		edited.people.map((entry) => entry.text),
		['Add search']
	);
});

test('what Kuayle recorded for an agent is marked, without its marker text', () => {
	const document = notebookDocument(
		view({
			done: [
				"[Recorded by Kuayle from the agent's final message; the agent did not update the notebook] (Implement) Search is added."
			]
		}),
		'Add search'
	);
	assert.deepEqual(document.done, [{ text: '(Implement) Search is added.', recorded: true }]);
});

test('open findings come before resolved ones', () => {
	const document = notebookDocument(
		view({
			findings: [
				{ id: 1, source: 'review', text: 'Rename q', resolved: true, at: at(1) },
				{ id: 2, source: 'ci', text: 'lint failed', at: at(2) },
				{ id: 3, source: 'agent', text: 'Flaky test', at: at(3) }
			]
		}),
		'Add search'
	);
	assert.deepEqual(
		document.findings.map((finding) => finding.id),
		[2, 3, 1]
	);
});

test('the history merges changes and events, newest first', () => {
	const document = notebookDocument(
		view({ events: [{ text: 'Paused by a person', at: at(6) }] }, [
			{ revision: 3, author: 'agent', step_label: 'Implement', step_sequence: 3, sections: ['done', 'remaining'], summary: '', created_at: at(8) },
			{ revision: 2, author: 'person', user_id: 'user-1', step_label: 'Plan', sections: ['instructions'], summary: '', created_at: at(5) }
		]),
		'Add search'
	);
	assert.deepEqual(
		document.history.map((entry) => (entry.kind === 'event' ? entry.text : `${entry.author}:${entry.step}`)),
		['agent:Implement', 'Paused by a person', 'person:Plan']
	);
	assert.deepEqual(document.history[0].sections, ['done', 'remaining']);
});

test('a run whose final message Kuayle recorded did not update its notebook', () => {
	const missed = missedNotebook(
		view({}, [
			{ revision: 4, author: 'kuayle', run_id: 'run-2', step_label: 'Implement', sections: ['done'], summary: '', created_at: at(9) },
			{ revision: 3, author: 'kuayle', run_id: 'run-1', step_label: 'Plan', sections: ['findings'], summary: '', created_at: at(8) },
			{ revision: 2, author: 'agent', run_id: 'run-1', step_label: 'Plan', sections: ['done'], summary: '', created_at: at(7) }
		])
	);
	assert.deepEqual([...missed], ['run-2']);
	assert.equal(missedNotebook(null).size, 0);
});
