import assert from 'node:assert/strict';
import test from 'node:test';
import {
	BOT_NAME_MAX,
	DEFAULT_MENTION_HARNESS,
	mentionHarnessFromChoice,
	mentionHarnessChoice,
	validateBotAvatarUrl,
	validateBotName
} from '../../src/lib/features/github/bot.ts';

test('bot names match what the API accepts', () => {
	assert.equal(validateBotName('  Acme Reviewer '), null);
	assert.equal(validateBotName(''), null, 'empty restores the default name');
	assert.equal(validateBotName('a'.repeat(BOT_NAME_MAX)), null);
	assert.equal(validateBotName('a'.repeat(BOT_NAME_MAX + 1)), 'length');
	for (const name of ['@everyone', '<b>bot</b>', 'two\nlines', 'bot*', '[link]']) {
		assert.equal(validateBotName(name), 'characters', name);
	}
});

test('linked bot pictures must be https image addresses', () => {
	assert.equal(validateBotAvatarUrl(' https://example.com/bot.png '), null);
	assert.equal(validateBotAvatarUrl('http://example.com/bot.png'), 'url');
	assert.equal(validateBotAvatarUrl('javascript:alert(1)'), 'url');
	assert.equal(validateBotAvatarUrl('https://user:pw@example.com/bot.png'), 'url');
	assert.equal(validateBotAvatarUrl('https://example.com/a.png" onerror="x'), 'url');
	assert.equal(validateBotAvatarUrl('not a url'), 'url');
});

test('the mention harness choice maps the default to an empty value', () => {
	assert.equal(mentionHarnessChoice(''), DEFAULT_MENTION_HARNESS);
	assert.equal(mentionHarnessChoice('codex'), 'codex');
	assert.equal(mentionHarnessFromChoice(DEFAULT_MENTION_HARNESS), '');
	assert.equal(mentionHarnessFromChoice('claude-code'), 'claude-code');
});
