import assert from 'node:assert/strict';
import test from 'node:test';
import { decodeHtmlEntities, htmlToPlainText } from '../../src/lib/security/plain-text.ts';
import { safeFileUrl } from '../../src/lib/security/safe-url.ts';
import { wikiFiles } from '../../src/lib/features/wiki/wiki-content.ts';
import { Attachment } from '../../src/lib/components/shared/attachment.extension.ts';

const scriptUrl = 'javascript:window.probe=1';

/** Runs `run` with a browser-like global scope that records what the helper touches. */
function withBrowserGlobals(run) {
	const calls = { parsed: [], liveElements: [] };
	const previous = { DOMParser: globalThis.DOMParser, document: globalThis.document };
	globalThis.DOMParser = class {
		parseFromString(html, type) {
			calls.parsed.push({ html, type });
			// Stands in for the inert document: tags removed, text kept.
			return { body: { textContent: html.replace(/<[^>]*>/g, '').replace(/&lt;/g, '<').replace(/&amp;/g, '&') } };
		}
	};
	globalThis.document = {
		createElement(tag) {
			calls.liveElements.push(tag);
			return { set innerHTML(_) {}, textContent: '', value: '' };
		}
	};
	try {
		run(calls);
	} finally {
		globalThis.DOMParser = previous.DOMParser;
		globalThis.document = previous.document;
	}
}

test('plain text comes from an inert parsed document, never from a live element', () => {
	withBrowserGlobals((calls) => {
		const html = '<p>Parent  summary</p>\n<img src="/probe.png" onerror="window.probe=1">';
		assert.equal(htmlToPlainText(html), 'Parent summary');
		assert.deepEqual(calls.parsed, [{ html, type: 'text/html' }]);
		assert.deepEqual(calls.liveElements, []);
	});
});

test('entity decoding keeps markup as literal text and creates no live element', () => {
	withBrowserGlobals((calls) => {
		assert.equal(decodeHtmlEntities('Fix <b> &amp; ship'), 'Fix <b> & ship');
		assert.equal(calls.parsed.length, 1);
		assert.ok(!calls.parsed[0].html.includes('<b>'), 'markup must reach the parser escaped');
		assert.deepEqual(calls.liveElements, []);
	});
});

test('plain text without a DOM strips tags and decodes entities', () => {
	assert.equal(typeof globalThis.DOMParser, 'undefined');
	assert.equal(htmlToPlainText('<p>Ship &amp; tell</p><img src=x onerror="window.probe=1"> now'), 'Ship & tell now');
	assert.equal(htmlToPlainText(null), '');
	assert.equal(htmlToPlainText(''), '');
	assert.equal(decodeHtmlEntities('Q &amp; A'), 'Q & A');
});

test('file links accept web URLs and same-origin API paths only', () => {
	assert.equal(safeFileUrl('https://example.com/a.pdf'), 'https://example.com/a.pdf');
	assert.equal(safeFileUrl('HTTP://example.com/a.pdf'), 'HTTP://example.com/a.pdf');
	assert.equal(safeFileUrl('/api/workspaces/acme/assets/1?download=1'), '/api/workspaces/acme/assets/1?download=1');
	for (const unsafe of [
		scriptUrl,
		' JavaScript:window.probe=1',
		'java\tscript:window.probe=1',
		'data:text/html,<p>x</p>',
		'vbscript:x',
		'//example.com/api/x',
		'/\\example.com/api/',
		'/apix/file',
		'file.pdf',
		'',
		null,
		undefined
	]) {
		assert.equal(safeFileUrl(unsafe), null, String(unsafe));
	}
});

test('the wiki file list drops entries whose link is not a web or API URL', () => {
	const html =
		`<p><a class="editor-attachment" data-type="attachment" data-filename="spec.pdf" data-size="10" href="${scriptUrl}">spec.pdf</a></p>` +
		'<p><a class="editor-attachment" data-type="attachment" data-filename="ok.pdf" data-size="10" href="/api/assets/1">ok.pdf</a></p>' +
		`<p><img src="${scriptUrl}" alt="bad"><img src="data:image/svg+xml,x" alt="inline"><img src="https://example.com/a.png" alt="good"></p>`;
	assert.deepEqual(wikiFiles(html), [
		{ kind: 'file', href: '/api/assets/1', name: 'ok.pdf', size: 10 },
		{ kind: 'image', href: 'https://example.com/a.png', name: 'good', size: 0 }
	]);
});

test('the attachment node neither reads nor renders an unsafe link', () => {
	const attributes = Attachment.config.addAttributes.call({});
	const element = (href) => ({ getAttribute: (name) => (name === 'href' ? href : null), textContent: 'spec.pdf' });
	assert.equal(attributes.href.parseHTML(element(scriptUrl)), null);
	assert.equal(attributes.href.parseHTML(element('/api/workspaces/acme/assets/1')), '/api/workspaces/acme/assets/1');

	const render = (href) => Attachment.config.renderHTML.call({}, { HTMLAttributes: { href, filename: 'spec.pdf', size: 10 } });
	assert.equal(render(scriptUrl)[1].href ?? null, null);
	assert.equal(render('https://example.com/spec.pdf')[1].href, 'https://example.com/spec.pdf');
});
