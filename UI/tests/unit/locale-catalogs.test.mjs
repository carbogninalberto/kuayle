import assert from 'node:assert/strict';
import test from 'node:test';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import {
	LOCALES,
	interpolate,
	isCommunityLocale,
	localeLabel
} from '../../src/lib/features/i18n/locales.ts';

const ui = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..');
const settings = JSON.parse(fs.readFileSync(path.join(ui, 'project.inlang/settings.json'), 'utf8'));
const read = (file) => JSON.parse(fs.readFileSync(path.join(ui, 'messages', file), 'utf8'));
const placeholders = (value) => new Set((value.match(/\{[^{}]*\}/g) ?? []).map((p) => p.slice(1, -1)));
// English-only plural suffix helpers that other languages may legitimately drop.
const OPTIONAL = new Set(['s', 'plural']);
const en = read(`${settings.sourceLanguageTag}.json`);

const full = Object.keys(LOCALES).filter((code) => LOCALES[code].support === 'full');
const community = Object.keys(LOCALES).filter((code) => LOCALES[code].support === 'community');
const communityFiles = fs
	.readdirSync(path.join(ui, 'messages/community'))
	.filter((f) => f.endsWith('.json'))
	.map((f) => f.slice(0, -5));

test('full locales are exactly the Paraglide-compiled locales', () => {
	assert.deepEqual([...settings.languageTags].sort(), [...full].sort());
});

test('community locales are exactly the lazily loaded catalogs', () => {
	assert.deepEqual([...communityFiles].sort(), [...community].sort());
	assert.equal(isCommunityLocale('de'), true);
	assert.equal(isCommunityLocale('it'), false);
	assert.equal(isCommunityLocale('xx'), false);
});

test('labels fall back to the code for unknown locales', () => {
	assert.equal(localeLabel('de'), 'Deutsch');
	assert.equal(localeLabel('xx'), 'xx');
});

test('interpolate fills placeholders and blanks missing ones', () => {
	assert.equal(interpolate('{n} gebruiker(s) geselecteerd', { n: 3 }), '3 gebruiker(s) geselecteerd');
	assert.equal(interpolate('{a} → {b}', { a: 'x', b: 0 }), 'x → 0');
	assert.equal(interpolate('Hallo {name}', {}), 'Hallo ');
	assert.equal(interpolate('Keine Platzhalter'), 'Keine Platzhalter');
});

const catalogs = [
	...full.filter((c) => c !== settings.sourceLanguageTag).map((c) => [c, `${c}.json`]),
	...community.map((c) => [c, `community/${c}.json`])
];

for (const [code, file] of catalogs) {
	test(`${code} catalog matches the English keys and placeholders`, () => {
		const tr = read(file);
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
