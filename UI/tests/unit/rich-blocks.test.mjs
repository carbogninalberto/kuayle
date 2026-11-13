import assert from 'node:assert/strict';
import test from 'node:test';
import MarkdownIt from 'markdown-it';
import {
	CHART_MAX_BYTES,
	HTML_MAX_BYTES,
	MAX_RICH_BLOCKS,
	MAX_RICH_BYTES_PER_MESSAGE,
	isRichFence,
	parseChartSpec,
	richSegments,
	stripRichBlocks,
	widgetDocument
} from '../../src/lib/features/assistant/rich-blocks.ts';

const spec = {
	type: 'bar',
	title: 'Issues this quarter',
	unit: 'issues',
	categories: ['Q1', 'Q2', 'Q3', 'Q4'],
	series: [{ name: 'Created', data: [4, 7, 3, 9] }]
};
const chart = (value) => `\`\`\`chart\n${typeof value === 'string' ? value : JSON.stringify(value)}\n\`\`\``;
const html = (body) => `\`\`\`html\n${body}\n\`\`\``;

test('a valid chart spec is accepted and kept with its surrounding markdown', () => {
	assert.deepEqual(parseChartSpec(JSON.stringify(spec)), spec);
	const source = `Intro\n\n${chart(spec)}\n\nOutro`;
	const segments = richSegments(source);
	assert.deepEqual(
		segments.map((segment) => segment.kind),
		['markdown', 'chart', 'markdown']
	);
	assert.equal(segments[0].source, 'Intro');
	assert.deepEqual(segments[1].spec, spec);
	assert.equal(segments[1].fence, chart(spec));
	assert.equal(segments[2].source, 'Outro');
});

test('invalid chart specs fall back to markdown', () => {
	const invalid = [
		'{not json',
		JSON.stringify({ ...spec, extra: true }),
		JSON.stringify({ ...spec, type: 'radar' }),
		JSON.stringify({ ...spec, categories: ['Q1'], series: [{ name: 'Created', data: [1, 2] }] }),
		JSON.stringify({ ...spec, series: [{ name: 'Created', data: ['1', 2, 3, 4] }] }),
		JSON.stringify({
			...spec,
			series: Array.from({ length: 6 }, (_, index) => ({ name: `S${index}`, data: [1, 2, 3, 4] }))
		}),
		JSON.stringify({
			...spec,
			type: 'pie',
			series: [
				{ name: 'A', data: [1, 2, 3, 4] },
				{ name: 'B', data: [1, 2, 3, 4] }
			]
		}),
		JSON.stringify({ ...spec, series: [{ name: 'Created', data: [1, 2, 3, 4], extra: 1 }] }),
		JSON.stringify({ ...spec, categories: Array.from({ length: 101 }, (_, index) => `C${index}`), series: [{ name: 'Created', data: Array(101).fill(1) }] }),
		JSON.stringify({ ...spec, title: 'x'.repeat(201) })
	];
	for (const value of invalid) {
		assert.equal(parseChartSpec(value), null, value);
		const segments = richSegments(chart(value));
		assert.deepEqual(
			segments.map((segment) => segment.kind),
			['markdown'],
			value
		);
	}
});

test('a pie chart keeps one series', () => {
	const pie = { type: 'pie', categories: ['Done', 'Open'], series: [{ name: 'Issues', data: [3, 1] }] };
	const segments = richSegments(chart(pie));
	assert.equal(segments[0].kind, 'chart');
	assert.equal(segments[0].spec.type, 'pie');
});

test('size caps and the per-message block cap leave the rest as code', () => {
	const title = 'x'.repeat(180);
	const categories = Array.from({ length: 100 }, (_, index) => `${index}-${title}`);
	const largeChart = { ...spec, categories, series: [{ name: 'Created', data: Array(100).fill(1) }] };
	assert.ok(Buffer.byteLength(chart(largeChart)) > CHART_MAX_BYTES);
	assert.deepEqual(
		richSegments(chart(largeChart)).map((segment) => segment.kind),
		['markdown']
	);

	const largeHtml = html(`<p>${'x'.repeat(HTML_MAX_BYTES)}</p>`);
	assert.ok(Buffer.byteLength(largeHtml) > HTML_MAX_BYTES);
	assert.deepEqual(
		richSegments(largeHtml).map((segment) => segment.kind),
		['markdown']
	);

	const blocks = Array.from({ length: MAX_RICH_BLOCKS + 1 }, () => chart(spec)).join('\n\n');
	const segments = richSegments(blocks);
	assert.deepEqual(
		segments.map((segment) => segment.kind),
		[...Array(MAX_RICH_BLOCKS).fill('chart'), 'markdown']
	);
	assert.match(segments.at(-1).source, /```chart/);
});

test('the per-message byte cap leaves an over-budget fence as code but keeps scanning', () => {
	const part = 'x'.repeat(60 * 1024);
	const big = (id) => html(`<p id="${id}">${part}</p>`);
	const source = [big('a'), big('b'), big('c'), chart(spec)].join('\n\n');
	assert.ok(Buffer.byteLength(big('a')) <= HTML_MAX_BYTES);
	assert.ok(3 * Buffer.byteLength(big('a')) > MAX_RICH_BYTES_PER_MESSAGE);
	const segments = richSegments(source);
	assert.deepEqual(
		segments.map((segment) => segment.kind),
		['html', 'html', 'markdown', 'chart']
	);
	assert.match(segments[2].source, /```html/);
	assert.match(segments[2].source, /id="c"/);
});

test('stripRichBlocks keeps fences the per-message caps turn into code', () => {
	const five = Array.from({ length: MAX_RICH_BLOCKS + 1 }, (_, index) =>
		chart({ ...spec, title: `Chart ${index}` })
	).join('\n\n');
	const stripped = stripRichBlocks(five);
	assert.equal((stripped.match(/```chart/g) ?? []).length, 1);
	assert.match(stripped, /"title":"Chart 4"/);

	const part = 'x'.repeat(60 * 1024);
	const total = [html(`<p id="a">${part}</p>`), html(`<p id="b">${part}</p>`), html(`<p id="c">${part}</p>`)].join(
		'\n\n'
	);
	const kept = stripRichBlocks(total);
	assert.equal((kept.match(/```html/g) ?? []).length, 1);
	assert.match(kept, /id="c"/);
});

test('a fence nested in a list stays markdown', () => {
	const nested = ['- Item', '', '  ```chart', `  ${JSON.stringify(spec)}`, '  ```'].join('\n');
	assert.deepEqual(
		richSegments(nested).map((segment) => segment.kind),
		['markdown']
	);
});

test('an unclosed fence at the end is still parsed', () => {
	const source = `Intro\n\n\`\`\`chart\n${JSON.stringify(spec)}`;
	const segments = richSegments(source);
	assert.deepEqual(
		segments.map((segment) => segment.kind),
		['markdown', 'chart']
	);
	assert.deepEqual(segments[1].spec, spec);
});

test('stripRichBlocks removes only accepted fences', () => {
	const source = `Intro\n\n${chart(spec)}\n\nMiddle\n\n${html('<p>hi</p>')}\n\nOutro`;
	assert.equal(stripRichBlocks(source), 'Intro\n\nMiddle\n\nOutro');
	const invalid = `Intro\n\n${chart('{bad json')}\n\nOutro`;
	assert.equal(stripRichBlocks(invalid), invalid);
});

test('isRichFence rejects non-rich fences and applies the byte caps', () => {
	const md = new MarkdownIt({ html: false });
	const tokens = md.parse('```js\nconsole.log(1)\n```', {});
	assert.equal(isRichFence(tokens[0]), false);
	const large = md.parse(
		chart({
			...spec,
			categories: Array.from({ length: 100 }, (_, index) => `${index}-${'x'.repeat(180)}`),
			series: [{ name: 'Created', data: Array(100).fill(1) }]
		}),
		{}
	);
	assert.equal(isRichFence(large[0]), false);
});

test('the widget document declares a strict CSP before the prelude and the widget body', () => {
	const document = widgetDocument('<script>document.body.dataset.ran = "1"</script>');
	const csp = document.indexOf('Content-Security-Policy');
	const prelude = document.indexOf('RTCPeerConnection');
	const body = document.indexOf('<body>');
	assert.ok(csp !== -1 && prelude > csp && body > prelude, 'CSP, then prelude, then model markup');
	assert.match(document, /connect-src 'none'/);
	assert.match(document, /frame-src 'none'/);
	assert.match(document, /worker-src 'none'/);
	assert.match(document, /webrtc 'block'/);
	assert.match(document, /x-dns-prefetch-control/);
	assert.ok(!document.includes('allow-same-origin'));
	assert.ok(document.endsWith('</body></html>'));
});
