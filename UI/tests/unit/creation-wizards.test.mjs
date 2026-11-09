import assert from 'node:assert/strict';
import test from 'node:test';
import {
	importedSkill,
	parseToolDraft,
	skillInstruction,
	skillSlug,
	skillTemplate,
	toolInstruction,
	toolNameFrom
} from '../../src/lib/features/assistant/creation-wizards.ts';
import { parseSkill } from '../../src/lib/features/workflows/skill-md.ts';

test('a skill name is a short slug of its purpose', () => {
	assert.equal(skillSlug('Review SQL migrations before they ship!'), 'review-sql-migrations-before-they-ship');
	assert.equal(skillSlug('Écrire les notes de version'), 'ecrire-les-notes-de-version');
	assert.equal(skillSlug('部署'), 'my-skill');
	assert.ok(skillSlug('word '.repeat(40)).length <= 64);
});

test('without AI the template is a valid SKILL.md made from the answers', () => {
	const text = skillTemplate({
		purpose: 'Review SQL migrations',
		whenToUse: 'When a change adds a migration\nunder BE/migrations.',
		examples: 'Check the down file.'
	});
	const skill = parseSkill(text);
	assert.deepEqual(skill.problems, []);
	assert.equal(skill.name, 'review-sql-migrations');
	assert.equal(skill.description, 'When a change adds a migration under BE/migrations.');
	assert.match(skill.body, /Review SQL migrations/);
	assert.match(skill.body, /Check the down file\./);
	// No "when" answer: the description falls back to the purpose, bounded.
	const bare = parseSkill(skillTemplate({ purpose: 'x'.repeat(900), whenToUse: '', examples: '' }));
	assert.deepEqual(bare.problems, []);
	assert.ok(bare.description.length <= 500);
});

test('the AI request fits the API limit and names every answer', () => {
	const instruction = skillInstruction({ purpose: 'p'.repeat(3000), whenToUse: 'on releases', examples: '' });
	assert.ok(instruction.length <= 2000);
	assert.match(instruction, /When to use: on releases/);
	const tool = toolInstruction({ what: 'List open PRs', service: 'https://api.github.com', inputs: 'repo' });
	assert.match(tool, /List open PRs/);
	assert.match(tool, /Service or URL: https:\/\/api\.github\.com/);
	assert.match(tool, /Inputs: repo/);
	assert.ok(toolInstruction({ what: 'w'.repeat(5000), service: '', inputs: '' }).length <= 2000);
	assert.doesNotMatch(toolInstruction({ what: 'x', service: '', inputs: '' }), /Service or URL/);
});

test('an imported .md that is a SKILL.md is used as the draft', () => {
	assert.equal(importedSkill('---\nname: a\ndescription: b\n---\n\nBody'), '---\nname: a\ndescription: b\n---\n\nBody');
	assert.equal(importedSkill('# Just notes'), null);
});

test('an AI tool draft fills only the tool fields and is checked like the form', () => {
	const draft = parseToolDraft(
		JSON.stringify({
			name: 'open_prs',
			description: 'List open pull requests.',
			interpreter: 'bash',
			script: 'gh pr list --repo "$KUAYLE_ARG_REPO"',
			parameters: {
				type: 'object',
				properties: { repo: { type: 'string', description: 'owner/name', maxLength: 200 } },
				required: ['repo'],
				additionalProperties: false
			},
			read_only: true,
			timeout_seconds: 120,
			api_key: 'dropped-by-kuayle',
			run_mode: 'auto'
		}),
		['sh', 'bash']
	);
	assert.ok(draft);
	assert.equal(draft.name, 'open_prs');
	assert.equal(draft.interpreter, 'bash');
	assert.equal(draft.readOnly, true);
	assert.equal(draft.timeout, '120');
	assert.deepEqual(
		draft.params.map((p) => [p.name, p.type, p.required, p.maxLength]),
		[['repo', 'string', true, '200']]
	);
	assert.ok(!JSON.stringify(draft).includes('dropped-by-kuayle'), 'unknown fields such as credentials are dropped');
	assert.ok(!('runMode' in draft), 'the run mode stays the user’s choice');

	// Fenced or wrapped answers still parse; odd values fall back to safe defaults.
	const loose = parseToolDraft(
		'```json\n{"name":"x","description":"d","interpreter":"ruby","script":"echo hi","parameters":"nope"}\n```',
		['sh', 'node']
	);
	assert.equal(loose?.interpreter, 'sh');
	assert.deepEqual(loose?.params, []);
	assert.equal(loose?.timeout, '600');

	assert.equal(parseToolDraft('not json', ['sh']), null);
	assert.equal(parseToolDraft('{"name": 3}', ['sh']), null);
});

test('without AI a tool name is a snake_case slug of what it does', () => {
	assert.equal(toolNameFrom('List the open PRs of a repo'), 'list_the_open_prs_of');
	assert.equal(toolNameFrom('2 fast'), 'tool_2_fast');
	assert.equal(toolNameFrom('部署'), '');
	assert.match(toolNameFrom('a'.repeat(100)), /^[a-z][a-z0-9_]{0,47}$/);
});
