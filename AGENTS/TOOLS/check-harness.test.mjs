import assert from 'node:assert/strict';
import { mkdtempSync, mkdirSync, writeFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import test from 'node:test';
import { checkHarness } from './check-harness.mjs';

function fixture(t) {
	const root = mkdtempSync(join(tmpdir(), 'kuayle-harness-'));
	t.after(() => rmSync(root, { recursive: true, force: true }));
	const write = (path, text) => {
		mkdirSync(join(root, path, '..'), { recursive: true });
		writeFileSync(join(root, path), text);
	};
	write('AGENTS.md', '[Skills](AGENTS/SKILLS.md)\n[Tools](AGENTS/TOOLS.md)\n');
	write('AGENTS/SKILLS.md', '[Example](SKILLS/example/SKILL.md)\n');
	write('AGENTS/TOOLS.md', '[Tool](TOOLS/tool.mjs)\n');
	write('AGENTS/TOOLS/tool.mjs', '');
	write('AGENTS/SKILLS/example/SKILL.md', '---\nname: example\ndescription: Handle example tasks.\n---\n[Tools](../../TOOLS.md)\n');
	return { root, write };
}

test('accepts a connected harness, relative links, fragments and external sources', (t) => {
	const { root, write } = fixture(t);
	write('AGENTS/TOOLS.md', '[Tool](TOOLS/tool.mjs)\n[Skills](SKILLS.md#skills)\n[Docs](https://example.com/docs)\n[Here](#local)\n```md\n[Example](missing.md)\n```\n');
	assert.deepEqual(checkHarness(root), []);
});

test('reports broken links and links escaping the repository', (t) => {
	const { root, write } = fixture(t);
	write('AGENTS/TOOLS.md', '[Missing](TOOLS/missing.mjs)\n[Outside](../../outside.md)\n');
	const errors = checkHarness(root);
	assert.ok(errors.some((error) => error.includes('TOOLS/missing.mjs')));
	assert.ok(errors.some((error) => error.includes('outside repository')));
});

test('rejects missing entrypoints, unindexed skills and invalid metadata', (t) => {
	const { root, write } = fixture(t);
	rmSync(join(root, 'AGENTS.md'));
	write('AGENTS/SKILLS/unlisted/SKILL.md', '---\nname: wrong\ndescription: \n---\n');
	const errors = checkHarness(root);
	assert.ok(errors.some((error) => error.includes('AGENTS.md: missing')));
	assert.ok(errors.some((error) => error.includes('unlisted') && error.includes('name')));
	assert.ok(errors.some((error) => error.includes('unlisted') && error.includes('description')));
	assert.ok(errors.some((error) => error.includes('unlisted') && error.includes('index')));
});

test('reports a skill directory without SKILL.md', (t) => {
	const { root } = fixture(t);
	mkdirSync(join(root, 'AGENTS/SKILLS/incomplete'));
	assert.ok(checkHarness(root).some((error) => error.includes('incomplete/SKILL.md: missing')));
});
