import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

import { MOVED_COMPARE_PAGES } from '../../src/lib/data/moved-pages.ts';

const caddyfile = readFileSync(new URL('../../deploy/Caddyfile', import.meta.url), 'utf8');

test('the web server permanently redirects exactly the moved pages', () => {
	const redirects = [...caddyfile.matchAll(/^\s*redir\s+(\S+)\s+(\S+)\s+permanent\s*$/gm)].map((match) => [match[1], match[2]]);
	const expected = Object.entries(MOVED_COMPARE_PAGES).map(([slug, moved]) => [`/compare/${slug}`, moved.to]);
	assert.deepEqual(redirects.sort(), expected.sort());
});

test('unknown paths return the 404 page instead of falling back to the homepage', () => {
	assert.doesNotMatch(caddyfile, /try_files[^\n]*\s\/index\.html\b/, 'no catch-all to /index.html');
	assert.match(caddyfile, /handle_errors 404 \{[\s\S]*rewrite \* \/404\.html/);
});
