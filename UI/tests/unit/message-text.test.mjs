import assert from 'node:assert/strict';
import test from 'node:test';
import { copyText, markdownToPlainText } from '../../src/lib/features/assistant/message-text.ts';

test('readable text drops Markdown markers and keeps the words', () => {
	assert.equal(
		markdownToPlainText('## Result\n\nThe **build** is _green_ and `npm test` ~~failed~~ passed.'),
		'Result\n\nThe build is green and npm test failed passed.'
	);
});

test('code blocks keep their exact lines, indentation and blank lines', () => {
	const code = 'func main() {\n\tfmt.Println("**not bold**")\n\n\treturn\n}';
	assert.equal(
		markdownToPlainText(`Run this:\n\n\`\`\`go\n${code}\n\`\`\`\n\nThen deploy.`),
		`Run this:\n\n${code}\n\nThen deploy.`
	);
	assert.equal(markdownToPlainText('    indented <code>\n'), 'indented <code>');
});

test('line breaks inside a paragraph are preserved', () => {
	assert.equal(markdownToPlainText('first line\nsecond line  \nthird line'), 'first line\nsecond line\nthird line');
});

test('links keep their destination when the text does not show it', () => {
	assert.equal(
		markdownToPlainText('See [the docs](https://example.com/docs?a=1&b=2) or https://example.com/raw.'),
		'See the docs (https://example.com/docs?a=1&b=2) or https://example.com/raw.'
	);
	assert.equal(markdownToPlainText('<https://example.com/x>'), 'https://example.com/x');
	// Images are never loaded in chat: they read as a link, and copy as one.
	assert.equal(markdownToPlainText('![diagram](https://example.com/a.png)'), '!diagram (https://example.com/a.png)');
});

test('lists, quotes, rules and tables stay readable', () => {
	assert.equal(
		markdownToPlainText('- one\n- two\n  - nested\n    more\n\n3. three\n4. four'),
		'- one\n- two\n  - nested\n    more\n\n3. three\n4. four'
	);
	assert.equal(markdownToPlainText('> quoted **text**\n> next'), '> quoted text\n> next');
	assert.equal(markdownToPlainText('a\n\n---\n\nb'), 'a\n\n---\n\nb');
	assert.equal(markdownToPlainText('| Name | Value |\n| --- | --- |\n| a | `1` |\n| b | 2 |'), 'Name\tValue\na\t1\nb\t2');
});

test('a list item keeps its code block', () => {
	assert.equal(markdownToPlainText('1. Install:\n\n   ```sh\n   npm ci\n   ```\n2. Done'), '1. Install:\n\n   npm ci\n\n2. Done');
});

test('raw HTML and entities are copied as the reader sees them', () => {
	assert.equal(markdownToPlainText('<img src=x onerror="evil()"> &amp; 1 &lt; 2'), '<img src=x onerror="evil()"> & 1 < 2');
});

test('rich chart and html fences are not copied as code', () => {
	const chart = JSON.stringify({
		type: 'bar',
		title: 'Issues this quarter',
		categories: ['Q1', 'Q2'],
		series: [{ name: 'Created', data: [4, 7] }]
	});
	const source = `Quarterly summary\n\n\`\`\`chart\n${chart}\n\`\`\`\n\nWidget:\n\n\`\`\`html\n<script>document.body.dataset.ran = "1"</script>\n\`\`\`\n\nDone.`;
	const text = markdownToPlainText(source);
	assert.equal(text, 'Quarterly summary\n\nWidget:\n\nDone.');
	assert.ok(!text.includes('"series"'));
	assert.ok(!text.includes('<script'));
	assert.ok(!text.includes('<iframe'));
});

test('a chart fence that cannot render is copied as code', () => {
	const source = 'Before\n\n```chart\n{"type":"bar","series":[]}\n```\n\nAfter';
	assert.equal(markdownToPlainText(source), 'Before\n\n{"type":"bar","series":[]}\n\nAfter');
});

test('copy keeps a fifth rich fence that the per-message block cap turns into code', () => {
	const fences = Array.from({ length: 5 }, (_, index) =>
		[
			'```chart',
			JSON.stringify({
				type: 'bar',
				title: `Chart ${index}`,
				categories: ['Q1'],
				series: [{ name: 'Created', data: [index] }]
			}),
			'```'
		].join('\n')
	);
	const text = markdownToPlainText(fences.join('\n\n'));
	assert.ok(text.includes('"title":"Chart 4"'));
	for (let index = 0; index < 4; index++) assert.ok(!text.includes(`"title":"Chart ${index}"`));
});

test('copy keeps fences the per-message byte cap turns into code', () => {
	const part = 'x'.repeat(60 * 1024);
	const html = (id) => `\`\`\`html\n<p id="${id}">${part}</p>\n\`\`\``;
	const text = markdownToPlainText([html('a'), html('b'), html('c')].join('\n\n'));
	assert.ok(text.includes('id="c"'));
	assert.ok(!text.includes('id="a"'));
	assert.ok(!text.includes('id="b"'));
});

test('long messages are not truncated', () => {
	const paragraphs = Array.from({ length: 4000 }, (_, index) => `Paragraph ${index} with **bold** text.`);
	const text = markdownToPlainText(paragraphs.join('\n\n'));
	assert.equal(text.split('\n\n').length, 4000);
	assert.ok(text.endsWith('Paragraph 3999 with bold text.'));
	assert.equal(markdownToPlainText(''), '');
});

function withGlobals(globals, run) {
	const names = ['navigator', 'document'];
	const previous = names.map((name) => Object.getOwnPropertyDescriptor(globalThis, name));
	for (const name of names) Object.defineProperty(globalThis, name, { value: globals[name], configurable: true });
	return Promise.resolve(run()).finally(() => {
		names.forEach((name, index) => {
			if (previous[index]) Object.defineProperty(globalThis, name, previous[index]);
			else delete globalThis[name];
		});
	});
}

function fakeDocument(copies) {
	const state = { appended: 0, removed: 0, refocused: 0, written: [] };
	const document = {
		activeElement: { focus: () => state.refocused++ },
		body: { appendChild: () => state.appended++ },
		createElement: () => ({
			value: '',
			style: {},
			setAttribute() {},
			select() {},
			setSelectionRange() {},
			remove: () => state.removed++
		}),
		execCommand(command) {
			state.written.push(command);
			return copies;
		}
	};
	return { document, state };
}

test('copyText writes the whole text through the async clipboard', async () => {
	const written = [];
	const long = 'x'.repeat(200_000) + '\nend';
	await withGlobals({ navigator: { clipboard: { writeText: async (text) => void written.push(text) } } }, async () => {
		assert.equal(await copyText(long), true);
	});
	assert.deepEqual(written, [long]);
});

test('copyText falls back to a selection copy when the clipboard API is missing or refuses', async () => {
	for (const navigator of [{}, { clipboard: { writeText: async () => Promise.reject(new Error('denied')) } }]) {
		const { document, state } = fakeDocument(true);
		await withGlobals({ navigator, document }, async () => assert.equal(await copyText('hello'), true));
		assert.deepEqual(state, { appended: 1, removed: 1, refocused: 1, written: ['copy'] });
	}
});

test('copyText reports failure when nothing could copy', async () => {
	const { document, state } = fakeDocument(false);
	await withGlobals({ navigator: {}, document }, async () => assert.equal(await copyText('hello'), false));
	assert.equal(state.removed, 1, 'the helper element is always removed');
});
