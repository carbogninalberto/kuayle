import assert from 'node:assert/strict';
import { after, before, test } from 'node:test';
import { JSDOM } from 'jsdom';

let dom, renderMarkdown;
before(async () => {
	// DOMPurify binds to the window when the Markdown module is imported.
	dom = new JSDOM();
	globalThis.window = dom.window;
	({ renderMarkdown } = await import('../../src/lib/markdown.ts'));
});
after(() => {
	delete globalThis.window;
	dom.window.close();
});

function renderPost(source) {
	return JSDOM.fragment(renderMarkdown(source, undefined, { allowImages: false }));
}

test('agent Markdown renders a table, list and fenced code block', () => {
	const post = renderPost([
		'| Screen | Before | Proposed fix |',
		'|---|---|---|',
		'| Chat | Raw text | Render Markdown |',
		'',
		'- Check the table.',
		'',
		'```sh',
		'npm test',
		'```'
	].join('\n'));
	assert.deepEqual([...post.querySelectorAll('table th')].map(th => th.textContent), ['Screen', 'Before', 'Proposed fix']);
	assert.equal(post.querySelector('ul > li')?.textContent, 'Check the table.');
	assert.equal(post.querySelector('pre > code')?.textContent.trim(), 'npm test');
});

test('agent Markdown preserves safe links and neutralizes raw HTML and unsafe URLs', () => {
	const post = renderPost([
		'<script>alert(1)</script>',
		'[x](javascript:alert(1))',
		'[y](JaVaScRiPt:alert(1))',
		'[z](data:text/html,<b>x</b>)',
		// Markdown accepts relative links; the sanitizer removes their href.
		'[relative](./settings)',
		'[audit](https://example.com/audit)'
	].join('\n\n'));
	assert.equal(post.querySelector('script'), null);
	assert.ok(post.textContent.includes('<script>alert(1)</script>'));
	assert.equal(post.querySelector('a[href^="javascript:" i], a[href^="data:" i]'), null);
	assert.deepEqual([...post.querySelectorAll('a[href]')].map(a => a.getAttribute('href')), ['https://example.com/audit']);
});
