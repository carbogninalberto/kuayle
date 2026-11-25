import assert from 'node:assert/strict';
import test from 'node:test';
import {
	draftValue,
	isReverting,
	issuesByName,
	issuesOfError,
	pendingNames,
	proposalOf,
	runInProgress,
	withDraft,
	withRevert,
	withoutDraft
} from '../../src/lib/features/instance-config/restart.ts';

const port = { name: 'SMTP_PORT', value: '587', base_value: '587', source: 'environment', restart_editable: true };
const overridden = { name: 'SMTP_HOST', value: 'mail.example.com', base_value: '', source: 'override', restart_editable: true };

test('a draft is kept only while it differs from the value in effect', () => {
	let pending = withDraft({}, port, '2525');
	assert.deepEqual(pending, { SMTP_PORT: '2525' });
	assert.equal(draftValue(port, pending), '2525');
	pending = withDraft(pending, port, ' 587 ');
	assert.deepEqual(pending, {}, 'typing the current value back is no change');
	assert.equal(draftValue(port, pending), '587');
});

test('reverting an override is a change to apply; reverting an environment value is nothing', () => {
	let pending = withRevert({}, overridden);
	assert.deepEqual(pending, { SMTP_HOST: null });
	assert.equal(isReverting(pending, 'SMTP_HOST'), true);
	assert.equal(draftValue(overridden, pending), '', 'the input shows the .env value it returns to');
	assert.deepEqual(withRevert({ SMTP_PORT: '2525' }, port), {}, 'nothing to revert: the draft is dropped');
	assert.deepEqual(withoutDraft(pending, 'SMTP_HOST'), {});
	assert.deepEqual(pending, { SMTP_HOST: null }, 'the previous state is not mutated');
});

test('the proposal trims strings and keeps reverts as null', () => {
	const pending = { SMTP_PORT: ' 2525 ', SMTP_HOST: null, PASSKEYS_ENABLED: 'false' };
	assert.deepEqual(proposalOf(pending), { SMTP_PORT: '2525', SMTP_HOST: null, PASSKEYS_ENABLED: 'false' });
	assert.deepEqual(pendingNames(pending), ['PASSKEYS_ENABLED', 'SMTP_HOST', 'SMTP_PORT']);
});

test('a run is in progress until the API decides it', () => {
	assert.equal(runInProgress(undefined), false);
	for (const status of ['restarting', 'applying']) assert.equal(runInProgress({ status }), true, status);
	for (const status of ['confirmed', 'rolled_back']) assert.equal(runInProgress({ status }), false, status);
});

test('issues split into per-variable messages and whole-configuration ones', () => {
	const { byName, general } = issuesByName([
		{ name: 'SMTP_PORT', message: 'SMTP_PORT must be between 1 and 65535' },
		{ message: 'FORGE_WEBHOOK_BASE_URL must use http or https' }
	]);
	assert.deepEqual(byName, { SMTP_PORT: 'SMTP_PORT must be between 1 and 65535' });
	assert.deepEqual(general, ['FORGE_WEBHOOK_BASE_URL must use http or https']);
});

test('a refused apply carries its issues as error details', () => {
	const err = {
		status: 400,
		error: { code: 'INVALID_CONFIG_VALUE', message: 'x', details: [{ field: 'SMTP_PORT', message: 'bad' }, { message: 'whole' }] }
	};
	assert.deepEqual(issuesOfError(err), [{ name: 'SMTP_PORT', message: 'bad' }, { name: undefined, message: 'whole' }]);
	assert.deepEqual(issuesOfError(new Error('network')), []);
	assert.deepEqual(issuesOfError(null), []);
});
