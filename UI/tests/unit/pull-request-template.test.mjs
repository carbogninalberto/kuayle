import assert from 'node:assert/strict';
import test from 'node:test';
import {
	DEFAULT_PULL_REQUEST_TEMPLATE,
	pullRequestTemplatePayload,
	pullRequestTemplateProblem,
	pullRequestTemplateValue
} from '../../src/lib/features/dev-machines/pull-request-template.ts';

test('the default template has the five sections', () => {
	assert.equal(
		DEFAULT_PULL_REQUEST_TEMPLATE,
		'## Description\n{{description}}\n\n## Changes\n{{changes}}\n\n## Verification\n{{verification}}\n\n## Screenshots\n{{screenshots}}\n\n## Notes\n{{notes}}\n'
	);
	assert.equal(pullRequestTemplateProblem(DEFAULT_PULL_REQUEST_TEMPLATE), null);
});

test('a workspace without a template shows the default; a narrower scope shows its own or nothing', () => {
	assert.equal(pullRequestTemplateValue(null, false), DEFAULT_PULL_REQUEST_TEMPLATE);
	assert.equal(pullRequestTemplateValue(undefined, true), '');
	assert.equal(pullRequestTemplateValue('{{issue}}', true), '{{issue}}');
	assert.equal(pullRequestTemplateValue('{{issue}}', false), '{{issue}}');
});

test('saving the default or nothing inherits; a custom template is sent trimmed', () => {
	assert.deepEqual(pullRequestTemplatePayload(DEFAULT_PULL_REQUEST_TEMPLATE, false), {});
	assert.deepEqual(pullRequestTemplatePayload('   ', true), {});
	assert.deepEqual(pullRequestTemplatePayload(`  ${DEFAULT_PULL_REQUEST_TEMPLATE}  `, true), { pull_request_template: DEFAULT_PULL_REQUEST_TEMPLATE.trim() }, 'a team can choose the default over a custom workspace template');
	assert.deepEqual(pullRequestTemplatePayload('  ## Why\n{{description}}\n', false), { pull_request_template: '## Why\n{{description}}' });
});

test('templates the API refuses are reported and never sent', () => {
	assert.deepEqual(pullRequestTemplateProblem('## Summary\n{{summary}}'), { kind: 'unknown_placeholder', name: 'summary' });
	assert.deepEqual(pullRequestTemplateProblem('界'.repeat(8001)), { kind: 'too_long' });
	assert.equal(pullRequestTemplateProblem('界'.repeat(8000)), null, 'characters, not bytes, are counted');
	assert.equal(pullRequestTemplateProblem('{{ issue }} {single}'), null, 'only {{name}} is a placeholder');
	assert.throws(() => pullRequestTemplatePayload('{{title}}', false));
});
