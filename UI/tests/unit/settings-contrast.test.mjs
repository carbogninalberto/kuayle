import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

const css = readFileSync(new URL('../../src/app.css', import.meta.url), 'utf8');

function luminance(hex) {
	const linear = hex.match(/\w\w/g).map((pair) => {
		const s = parseInt(pair, 16) / 255;
		return s <= 0.04045 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4;
	});
	return 0.2126 * linear[0] + 0.7152 * linear[1] + 0.0722 * linear[2];
}

for (const [, theme, declarations] of css.matchAll(/html\.([\w-]+)\s*\{([^}]*--color-bg-secondary:[^}]*)\}/g)) {
	test(`${theme} tertiary text on the secondary surface meets 4.5:1`, () => {
		const color = (token) => luminance(declarations.match(new RegExp(`--color-${token}: #([a-f0-9]{6});`, 'i'))[1]);
		const [dark, light] = [color('bg-secondary'), color('text-tertiary')].sort((a, b) => a - b);
		const ratio = (light + 0.05) / (dark + 0.05);
		assert.ok(ratio >= 4.5, `measured ${ratio.toFixed(2)}:1`);
	});
}
