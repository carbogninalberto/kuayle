import assert from 'node:assert/strict';
import test from 'node:test';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { LOCALES, localeLabel } from '../../src/lib/features/i18n/locales.ts';

const ui = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..');
const settings = JSON.parse(fs.readFileSync(path.join(ui, 'project.inlang/settings.json'), 'utf8'));
const catalog = (code) => JSON.parse(fs.readFileSync(path.join(ui, 'messages', `${code}.json`), 'utf8'));
const placeholders = (value) => new Set((value.match(/\{[^{}]*\}/g) ?? []).map((p) => p.slice(1, -1)));
// English-only plural suffix helpers that other languages may legitimately drop.
const OPTIONAL = new Set(['s', 'plural']);
const en = catalog(settings.sourceLanguageTag);

test('every configured locale has a registry entry and every entry is configured', () => {
	assert.deepEqual([...settings.languageTags].sort(), Object.keys(LOCALES).sort());
	assert.equal(localeLabel('de'), 'Deutsch');
	assert.equal(localeLabel('xx'), 'xx');
});

for (const code of settings.languageTags.filter((c) => c !== settings.sourceLanguageTag)) {
	test(`${code} catalog matches the English keys and placeholders`, () => {
		const tr = catalog(code);
		assert.deepEqual(Object.keys(tr), Object.keys(en), 'keys differ from en.json');
		const problems = [];
		for (const [key, source] of Object.entries(en)) {
			const value = tr[key];
			if (typeof value !== 'string' || (source.trim() && !value.trim())) {
				problems.push(`${key}: empty`);
				continue;
			}
			const want = placeholders(source);
			const got = placeholders(value);
			for (const p of want) if (!got.has(p) && !OPTIONAL.has(p)) problems.push(`${key}: missing {${p}}`);
			for (const p of got) if (!want.has(p)) problems.push(`${key}: unknown {${p}}`);
		}
		assert.deepEqual(problems, []);
	});
}
