import assert from 'node:assert/strict';
import test from 'node:test';
import { SKILL_TEMPLATE, formatSkill, parseSkill } from '../../src/lib/features/workflows/skill-md.ts';

test('a standard SKILL.md splits into name, description and body', () => {
	const skill = parseSkill(
		'---\nname: write-migrations\ndescription: "How to write: and test migrations."\nlicense: MIT\n---\n\n# Migrations\n\nUse up and down files.\n'
	);
	assert.deepEqual(skill, {
		name: 'write-migrations',
		description: 'How to write: and test migrations.',
		body: '# Migrations\n\nUse up and down files.',
		problems: []
	});
});

test('the template is a valid skill', () => {
	assert.deepEqual(parseSkill(SKILL_TEMPLATE).problems, []);
});

test('Windows line endings and a byte order mark are accepted', () => {
	const skill = parseSkill('﻿---\r\nname: lint\r\ndescription: Run the linter.\r\n---\r\nRun it.\r\n');
	assert.equal(skill.name, 'lint');
	assert.equal(skill.body, 'Run it.');
	assert.deepEqual(skill.problems, []);
});

test('problems name what is missing or malformed', () => {
	assert.deepEqual(parseSkill('# Just a body').problems, ['front_matter', 'name', 'description']);
	assert.deepEqual(parseSkill('---\nname: Not Valid\ndescription: ok\n---\nbody').problems, ['name']);
	assert.deepEqual(parseSkill('---\nname: ok\n---\nbody').problems, ['description']);
	assert.deepEqual(parseSkill('---\nname: ok\ndescription: ok\n---\n   ').problems, ['body']);
	assert.deepEqual(parseSkill(`---\nname: ${'a'.repeat(65)}\ndescription: ok\n---\nbody`).problems, ['name']);
});

test('a stored skill formats back to the same skill', () => {
	const stored = {
		name: 'review-sql',
		description: 'Check queries for: injection, scoping',
		body: '# SQL\n\nScope by workspace.'
	};
	const parsed = parseSkill(formatSkill(stored));
	assert.deepEqual({ name: parsed.name, description: parsed.description, body: parsed.body }, stored);
	assert.deepEqual(parsed.problems, []);
});
