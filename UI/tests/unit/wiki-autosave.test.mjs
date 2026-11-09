import assert from 'node:assert/strict';
import test from 'node:test';
import { WikiAutosave } from '../../src/lib/features/wiki/wiki-autosave.ts';

function harness(page = { title: 'Runbook', content: '<p>v1</p>', version: 1 }, server = {}) {
	const calls = [];
	const states = [];
	const saved = [];
	let stored = { ...page };
	const autosave = new WikiAutosave(page, {
		delay: 5,
		save: async (changes, version) => {
			calls.push({ changes, version });
			if (server.fail) throw server.fail;
			if (version !== stored.version) throw { status: 409 };
			stored = { ...stored, ...changes, version: stored.version + 1 };
			return { ...stored };
		},
		onstate: (state) => states.push(state),
		onsaved: (p) => saved.push(p)
	});
	return { autosave, calls, states, saved, server, setStored: (p) => (stored = p) };
}

const wait = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

test('edits are saved once after typing pauses, with the current version', async () => {
	const h = harness();
	h.autosave.edit({ content: '<p>v2</p>' });
	h.autosave.edit({ content: '<p>v3</p>' });
	assert.equal(h.calls.length, 0);
	await wait(20);
	assert.deepEqual(h.calls, [{ changes: { content: '<p>v3</p>' }, version: 1 }]);
	assert.deepEqual(h.states, ['pending', 'pending', 'saving', 'saved']);

	h.autosave.edit({ title: ' Runbook v2 ' });
	await h.autosave.flush();
	assert.deepEqual(h.calls[1], { changes: { title: 'Runbook v2' }, version: 2 }, 'the next save uses the new version');
});

test('edits that restore the saved text are not sent', async () => {
	const h = harness();
	h.autosave.edit({ content: '<p>v1</p>', title: 'Runbook ' });
	await h.autosave.flush();
	h.autosave.edit({ content: '<p>changed</p>' });
	h.autosave.edit({ content: '<p>v1</p>' });
	await wait(20);
	assert.equal(h.calls.length, 0);
	assert.equal(h.autosave.dirty, false);
});

test('edits made while a save runs are sent after it, never in parallel', async () => {
	const h = harness();
	h.autosave.edit({ content: '<p>a</p>' });
	const first = h.autosave.flush();
	h.autosave.edit({ content: '<p>b</p>' });
	await Promise.all([first, h.autosave.flush()]);
	assert.deepEqual(
		h.calls.map((c) => c.version),
		[1, 2]
	);
	assert.equal(h.saved.at(-1).content, '<p>b</p>');
});

test('a page changed elsewhere stops autosave and keeps the local edit', async () => {
	const h = harness();
	h.setStored({ title: 'Runbook', content: '<p>theirs</p>', version: 2 });
	h.autosave.edit({ content: '<p>mine</p>' });
	await h.autosave.flush();
	assert.equal(h.states.at(-1), 'conflict');
	assert.equal(h.autosave.conflict, true);
	h.autosave.edit({ content: '<p>mine, more</p>' });
	await h.autosave.flush();
	assert.equal(h.calls.length, 1, 'nothing more is sent until the latest version is loaded');

	h.autosave.reset({ title: 'Runbook', content: '<p>theirs</p>', version: 2 });
	assert.equal(h.autosave.dirty, false);
	h.autosave.edit({ content: '<p>after reload</p>' });
	await h.autosave.flush();
	assert.deepEqual(h.calls.at(-1), { changes: { content: '<p>after reload</p>' }, version: 2 });
});

test('a failed save keeps the edit and retries with the next one', async () => {
	const h = harness();
	h.server.fail = new Error('offline');
	h.autosave.edit({ content: '<p>v2</p>' });
	await h.autosave.flush();
	assert.equal(h.states.at(-1), 'error');
	assert.equal(h.autosave.dirty, true);
	delete h.server.fail;
	h.autosave.edit({ title: 'Runbook 2' });
	await h.autosave.flush();
	assert.deepEqual(h.calls.at(-1).changes, { title: 'Runbook 2', content: '<p>v2</p>' });
	assert.equal(h.states.at(-1), 'saved');
});
