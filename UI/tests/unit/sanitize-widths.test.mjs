import assert from 'node:assert/strict';
import test from 'node:test';
import { COLWIDTH, COL_WIDTH, columnStyleWidth, filterColumnWidthStyles } from '../../src/lib/security/column-widths.ts';

// The Go sanitizer lowercases property names and values before matching, so
// CSS case rules must not make the two layers disagree on what is stored.
test('only pixel width and min-width declarations survive, case-insensitively', () => {
	const cases = [
		['width: 340px', 'width: 340px'],
		['min-width: 50px', 'min-width: 50px'],
		['WIDTH: 340px', 'WIDTH: 340px'],
		['width: 340PX', 'width: 340PX'],
		['width:340px', 'width: 340px'],
		['width: 340px !important', 'width: 340px'],
		['width: 180px; position: fixed', 'width: 180px'],
		['position: fixed; width: 180px', 'width: 180px'],
		['width: 180px; min-width: 50px', 'width: 180px; min-width: 50px'],
		['background: url(https://evil.example/x)', ''],
		['width: 99999px', ''],
		['width: 25%', ''],
		['min-width: 50px; width: 25%', 'min-width: 50px'],
		['color: red', ''],
		['width:', '']
	];
	for (const [input, output] of cases) {
		assert.equal(filterColumnWidthStyles(input), output, input);
	}
});

// The editor reads the col width attribute with parseInt, so only a bare
// integer may be stored: anything else would load as a corrupted pixel width.
test('only bare integers are valid width attributes', () => {
	for (const value of ['180', '180px', '340']) assert.ok(COL_WIDTH.test(value), value);
	for (const value of ['25%', '200em', '0', '99999', '180.5', 'abc', '']) assert.ok(!COL_WIDTH.test(value), value);
});

test('cell colwidth lists integers separated by commas', () => {
	// A 0 entry is the editor's placeholder for the columns a merged cell
	// spans: Tiptap writes it for a resized <th colspan="2">, and stripping
	// the attribute would lose the whole column layout on reload.
	for (const value of ['180', '180,240', '180,0', '0,180', '180,0,240']) assert.ok(COLWIDTH.test(value), value);
	for (const value of ['abc', '12,,34', '180,', ',180', '99999', '180,99999', '']) assert.ok(!COLWIDTH.test(value), value);
});

test('a col style width parses to the pixels the editor imports', () => {
	assert.equal(columnStyleWidth('width: 180px'), 180);
	assert.equal(columnStyleWidth('WIDTH: 180.4px'), 180);
	assert.equal(columnStyleWidth('min-width: 50px; width: 180px'), 180);
	assert.equal(columnStyleWidth('width: 180px !important'), 180);
	// A width the editor cannot honour: percentages, other units, none at all.
	assert.equal(columnStyleWidth('width: 25%'), null);
	assert.equal(columnStyleWidth('width: 180em'), null);
	assert.equal(columnStyleWidth('min-width: 50px'), null);
	assert.equal(columnStyleWidth(''), null);
});
