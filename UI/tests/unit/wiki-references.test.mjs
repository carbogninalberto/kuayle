import assert from 'node:assert/strict';
import test from 'node:test';
import {
	chatReferenceParts,
	chatReferenceToken,
	createReferenceResolver,
	mentionQueryAt,
	mentionQueryMayMatch,
	renderReaderReferences,
	wikiMentionHtml,
	wikiMentionIds,
	wikiPageDragData,
	wikiReferenceHref
} from '../../src/lib/features/wiki/wiki-references.ts';
import { WIKI_PAGE_DRAG_TYPE } from '../../src/lib/features/wiki/wiki-tree.ts';

const GUIDE = '0b0e7a52-6a0f-4a56-9d3e-0d2b3f6a9c11';
const SECRET = '7c2b1d4e-9f3a-4b6c-8d5e-1a2b3c4d5e6f';
const OTHER = '11111111-2222-4333-8444-555555555555';

test('a reference opens the page in its own team or project wiki', () => {
	assert.equal(
		wikiReferenceHref('acme', { id: GUIDE, accessible: true, title: 'Guide', team_id: 't1' }),
		`/acme/teams/t1/wiki?page=${GUIDE}`
	);
	assert.equal(
		wikiReferenceHref('acme', { id: GUIDE, accessible: true, title: 'Plan', project_id: 'p1' }),
		`/acme/projects/p1/wiki?page=${GUIDE}`
	);
	assert.equal(wikiReferenceHref('acme', { id: SECRET, accessible: false }), null);
});

test('the resolver asks once for the chips of a render and caches the answers', async () => {
	const calls = [];
	const resolver = createReferenceResolver(async (slug, ids) => {
		calls.push([slug, [...ids]]);
		return ids.map((id) => (id === GUIDE ? { id, accessible: true, title: 'Guide', team_id: 't1' } : { id, accessible: false }));
	});
	const [guide, secret, again] = await Promise.all([
		resolver.resolve('acme', GUIDE),
		resolver.resolve('acme', SECRET),
		resolver.resolve('acme', GUIDE)
	]);
	assert.equal(guide.title, 'Guide');
	assert.deepEqual(secret, { id: SECRET, accessible: false });
	assert.equal(again, guide);
	assert.deepEqual(calls, [['acme', [GUIDE, SECRET]]]);

	await resolver.resolve('acme', GUIDE);
	assert.equal(calls.length, 1, 'a cached page is not asked for again');
	await resolver.resolve('other', GUIDE);
	assert.deepEqual(calls[1], ['other', [GUIDE]], 'answers are kept per workspace');
});

test('the resolver forgets answers after its lifetime and never caches a failure', async () => {
	let now = 0;
	let fail = true;
	let calls = 0;
	const resolver = createReferenceResolver(
		async (_slug, ids) => {
			calls++;
			if (fail) throw new Error('offline');
			return ids.map((id) => ({ id, accessible: true, title: 'Guide', team_id: 't1' }));
		},
		{ ttlMs: 1000, now: () => now }
	);
	// A failed lookup shows the placeholder rather than a stale or guessed title.
	assert.deepEqual(await resolver.resolve('acme', GUIDE), { id: GUIDE, accessible: false });
	fail = false;
	assert.equal((await resolver.resolve('acme', GUIDE)).title, 'Guide');
	assert.equal(calls, 2);
	now = 999;
	await resolver.resolve('acme', GUIDE);
	assert.equal(calls, 2);
	now = 1001;
	await resolver.resolve('acme', GUIDE);
	assert.equal(calls, 3, 'access is checked again once the answer is old');
});

test('a page the server does not answer for is inaccessible', async () => {
	const resolver = createReferenceResolver(async () => []);
	assert.deepEqual(await resolver.resolve('acme', GUIDE), { id: GUIDE, accessible: false });
});

test('mentions carry only the page id', () => {
	const html = wikiMentionHtml(GUIDE);
	assert.equal(html, `<span data-type="mention" data-kind="wiki" data-id="${GUIDE}" class="mention mention-wiki"></span>`);
	assert.deepEqual(wikiMentionIds(`<p>${html} and ${wikiMentionHtml(SECRET)} and ${html}</p>`), [GUIDE, SECRET]);
	assert.deepEqual(wikiMentionIds('<p><span data-type="mention" data-kind="issue" data-id="x">KUA-1</span></p>'), []);
});

test('a reader links published pages, prints known unpublished ones as text and hides the rest', () => {
	const html = `<p>See ${wikiMentionHtml(GUIDE)}, ${wikiMentionHtml(OTHER)} and ${wikiMentionHtml(SECRET)}.</p>`;
	const rendered = renderReaderReferences(html, {
		published: new Map([[GUIDE, 'Guide <1>']]),
		named: new Map([[OTHER, 'Roadmap & "plans"']]),
		hrefFor: (id) => `?page=${id}`,
		unavailable: 'Restricted page'
	});
	assert.equal(
		rendered,
		`<p>See <a class="mention mention-wiki" href="?page=${GUIDE}">Guide &lt;1&gt;</a>, Roadmap &amp; &quot;plans&quot; and ` +
			`<span class="mention mention-wiki mention-unavailable">Restricted page</span>.</p>`
	);
	// A title sent inside a chip is never trusted: only resolved titles print.
	const forged = `<p><span data-type="mention" data-kind="wiki" data-id="${SECRET}" data-label="Leak">Leak</span></p>`;
	assert.ok(
		!renderReaderReferences(forged, { published: new Map(), named: new Map(), hrefFor: () => '', unavailable: 'Restricted page' }).includes(
			'Leak'
		)
	);
});

test('a dragged page is recognised by its drag type and id', () => {
	const data = new Map([[WIKI_PAGE_DRAG_TYPE, GUIDE]]);
	const transfer = { types: [...data.keys()], getData: (type) => data.get(type) ?? '' };
	assert.equal(wikiPageDragData(transfer), GUIDE);
	assert.equal(wikiPageDragData({ types: ['Files'], getData: () => '' }), null);
	assert.equal(wikiPageDragData({ types: [WIKI_PAGE_DRAG_TYPE], getData: () => 'not-an-id' }), null);
	assert.equal(wikiPageDragData(null), null);
});

test('typing @ at the start of a word asks for suggestions', () => {
	assert.deepEqual(mentionQueryAt('see @dep', 8), { from: 4, query: 'dep' });
	assert.deepEqual(mentionQueryAt('@', 1), { from: 0, query: '' });
	assert.deepEqual(mentionQueryAt('line\n@deploy guide', 18), { from: 5, query: 'deploy guide' });
	assert.equal(mentionQueryAt('mail me@example', 15), null, 'an @ inside a word is not a mention');
	assert.equal(mentionQueryAt('see @dep\nnext', 13), null, 'a mention does not span lines');
	assert.equal(mentionQueryAt('no mention', 5), null);
});

test('a chat reference round-trips through message text', () => {
	const token = chatReferenceToken({ id: GUIDE, accessible: true, title: 'Deploy [guide]', team_id: 't1', scope_name: 'Docs' });
	assert.equal(token, `@[Deploy (guide)](wiki:${GUIDE} team Docs)`);
	assert.deepEqual(chatReferenceParts(`Read ${token} first`), [
		{ text: 'Read ' },
		{ text: token, pageId: GUIDE },
		{ text: ' first' }
	]);
	assert.deepEqual(chatReferenceParts('plain text'), [{ text: 'plain text' }]);
	assert.equal(
		chatReferenceToken({ id: GUIDE, accessible: true, title: 'Plan', project_id: 'p1', scope_name: 'Launch (2027)' }),
		`@[Plan](wiki:${GUIDE} project Launch 2027)`
	);
});

test('a chat @-query stops searching once a shorter one found no page', () => {
	assert.equal(mentionQueryMayMatch('dep', []), true);
	assert.equal(mentionQueryMayMatch('deploy guide', ['bob']), true);
	assert.equal(mentionQueryMayMatch('bob how are you', ['bob']), false);
	assert.equal(mentionQueryMayMatch('Bob', ['bob']), false);
	assert.equal(mentionQueryMayMatch('anything', ['']), false, 'a workspace without pages has nothing to suggest');
});
