import assert from 'node:assert/strict';
import test from 'node:test';
import { mergedWork, safePullRequestUrl } from '../../src/lib/features/assistant/merged-work.ts';

// KUA-193: a retry or a new iteration on merged work answers 409 MERGED_WORK
// with the pull request; anything else is an ordinary error.
const refusal = (pull_request, extra = {}) => ({
	status: 409,
	error: { code: 'MERGED_WORK', message: "This issue's work was merged in #12. Retry anyway?", pull_request },
	...extra
});

test('the merged pull request is read from a MERGED_WORK refusal', () => {
	const pr = { number: 12, url: 'https://github.com/acme/app/pull/12', repository: 'acme/app' };
	assert.deepEqual(mergedWork(refusal(pr)), pr);
});

test('other errors and malformed refusals are not merged work', () => {
	const pr = { number: 12, url: 'https://github.com/acme/app/pull/12', repository: 'acme/app' };
	for (const error of [
		null,
		undefined,
		'MERGED_WORK',
		new Error('network'),
		{ status: 409, error: { code: 'TASK_CONFLICT', message: 'conflict' } },
		{ ...refusal(pr), status: 400 },
		refusal(undefined),
		refusal({ ...pr, number: 0 }),
		refusal({ ...pr, number: '12' })
	])
		assert.equal(mergedWork(error), null, JSON.stringify(error));
});

test('only web links are linked; any other address is dropped', () => {
	const pr = (url) => mergedWork(refusal({ number: 3, url, repository: 'acme/api' }));
	assert.equal(
		pr('https://gitlab.example.test/acme/api/-/merge_requests/3').url,
		'https://gitlab.example.test/acme/api/-/merge_requests/3'
	);
	assert.equal(pr('http://git.local/acme/api/pulls/3').url, 'http://git.local/acme/api/pulls/3');
	for (const url of ['javascript:alert(1)', 'data:text/html,x', '/relative/path', 'not a url', ''])
		assert.equal(pr(url).url, '', url);
	assert.equal(safePullRequestUrl(' https://github.com/acme/app/pull/1 '), 'https://github.com/acme/app/pull/1');
	assert.equal(safePullRequestUrl(undefined), '');
});
