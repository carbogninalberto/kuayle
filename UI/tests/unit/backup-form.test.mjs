import assert from 'node:assert/strict';
import test from 'node:test';
import {
	buildConnectionTest,
	buildSettingsPayload,
	formFromSettings,
	formatBytes,
	formatDuration,
	isValidTime,
	shortChecksum,
	shouldPoll,
	validateBackupForm,
	verifyBlock
} from '../../src/lib/features/backups/backup-form.ts';

const settings = {
	enabled: true,
	schedule_kind: 'daily',
	schedule_time: '02:30',
	schedule_weekday: 0,
	schedule_cron: '',
	retention_kind: 'count',
	retention_value: 7,
	s3_enabled: true,
	s3_endpoint: 'https://s3.example.com',
	s3_bucket: 'bucket',
	s3_region: 'eu-west-1',
	s3_prefix: 'kuayle',
	has_access_key: true,
	has_secret_key: true,
	encryption_enabled: true,
	has_passphrase: true,
	next_run_at: null,
	updated_at: '2026-11-15T00:00:00Z',
	backup_dir: '/backups'
};

test('a form from saved settings never carries secrets', () => {
	const form = formFromSettings(settings);
	assert.equal(form.s3_access_key, '');
	assert.equal(form.s3_secret_key, '');
	assert.equal(form.passphrase, '');
	assert.equal(form.clear_s3_credentials, false);
	assert.equal(form.clear_passphrase, false);
});

test('the save payload omits empty secrets so stored ones are kept', () => {
	const payload = buildSettingsPayload(formFromSettings(settings));
	assert.equal('s3_access_key' in payload, false);
	assert.equal('s3_secret_key' in payload, false);
	assert.equal('passphrase' in payload, false);
	assert.equal('clear_s3_credentials' in payload, false);
	assert.equal('clear_passphrase' in payload, false);
	assert.equal(payload.retention_value, 7);
});

test('the save payload sends typed secrets and lets a clear win', () => {
	const form = { ...formFromSettings(settings), s3_access_key: ' AKIA ', s3_secret_key: 'secret', passphrase: 'long enough phrase' };
	const payload = buildSettingsPayload(form);
	assert.equal(payload.s3_access_key, 'AKIA');
	assert.equal(payload.s3_secret_key, 'secret');
	assert.equal(payload.passphrase, 'long enough phrase');

	const cleared = buildSettingsPayload({ ...form, clear_s3_credentials: true, clear_passphrase: true });
	assert.equal(cleared.clear_s3_credentials, true);
	assert.equal(cleared.clear_passphrase, true);
	assert.equal('s3_access_key' in cleared, false);
	assert.equal('s3_secret_key' in cleared, false);
	assert.equal('passphrase' in cleared, false);
});

test('the connection test sends unsaved destination values and only typed keys', () => {
	const form = { ...formFromSettings(settings), s3_bucket: ' other ' };
	assert.deepEqual(buildConnectionTest(form), {
		s3_endpoint: 'https://s3.example.com',
		s3_bucket: 'other',
		s3_region: 'eu-west-1',
		s3_prefix: 'kuayle'
	});
	assert.equal(buildConnectionTest({ ...form, s3_secret_key: 'x' }).s3_secret_key, 'x');
});

test('validates HH:MM times', () => {
	for (const ok of ['00:00', '09:05', '23:59']) assert.equal(isValidTime(ok), true, ok);
	for (const bad of ['24:00', '9:05', '12:60', '', '12-30', '1230']) assert.equal(isValidTime(bad), false, bad);
});

test('form validation covers schedule, retention and passphrase rules', () => {
	const base = formFromSettings(settings);
	assert.equal(validateBackupForm(base, settings), null);
	assert.equal(validateBackupForm({ ...base, schedule_time: '25:00' }, settings), 'invalid_time');
	assert.equal(validateBackupForm({ ...base, schedule_kind: 'cron', schedule_time: 'x', schedule_cron: ' ' }, settings), 'missing_cron');
	assert.equal(validateBackupForm({ ...base, schedule_kind: 'cron', schedule_time: 'x', schedule_cron: '0 3 * * *' }, settings), null);
	assert.equal(validateBackupForm({ ...base, retention_value: 0 }, settings), 'invalid_retention');
	assert.equal(validateBackupForm({ ...base, retention_value: 1001 }, settings), 'invalid_retention');
	assert.equal(validateBackupForm({ ...base, retention_value: 2.5 }, settings), 'invalid_retention');
	assert.equal(validateBackupForm({ ...base, passphrase: 'short' }, settings), 'short_passphrase');
	assert.equal(validateBackupForm(base, { has_passphrase: false }), 'missing_passphrase');
	assert.equal(validateBackupForm({ ...base, clear_passphrase: true }, settings), 'missing_passphrase');
	assert.equal(validateBackupForm({ ...base, encryption_enabled: false, clear_passphrase: true }, settings), null);
});

test('formats sizes and durations', () => {
	assert.equal(formatBytes(null), '');
	assert.equal(formatBytes(512), '512 B');
	assert.equal(formatBytes(1536), '1.5 KB');
	assert.equal(formatBytes(50 * 1024 * 1024), '50 MB');
	assert.equal(formatBytes(3 * 1024 ** 3), '3.0 GB');
	assert.equal(formatDuration(null), '');
	assert.equal(formatDuration(250), '250 ms');
	assert.equal(formatDuration(4200), '4.2 s');
	assert.equal(formatDuration(42_000), '42 s');
	assert.equal(formatDuration(125_000), '2m 5s');
	assert.equal(formatDuration(3_725_000), '1h 2m');
});

test('shortens checksums to 12 characters', () => {
	assert.equal(shortChecksum('0123456789abcdef0123'), '0123456789ab');
	assert.equal(shortChecksum(null), '');
});

test('polls only while a backup or its upload is in progress', () => {
	assert.equal(shouldPoll([]), false);
	assert.equal(shouldPoll([{ status: 'succeeded', upload_status: 'uploaded' }]), false);
	assert.equal(shouldPoll([{ status: 'running', upload_status: 'none' }]), true);
	assert.equal(shouldPoll([{ status: 'succeeded', upload_status: 'uploading' }]), true);
});

test('verification is blocked for updater dumps, removed and unsuccessful runs', () => {
	assert.equal(verifyBlock({ kind: 'manual', status: 'succeeded', deleted_at: null }), null);
	assert.equal(verifyBlock({ kind: 'pre_upgrade', status: 'succeeded', deleted_at: null }), 'pre_upgrade');
	assert.equal(verifyBlock({ kind: 'scheduled', status: 'succeeded', deleted_at: '2026-11-01T00:00:00Z' }), 'deleted');
	assert.equal(verifyBlock({ kind: 'scheduled', status: 'failed', deleted_at: null }), 'not_succeeded');
	assert.equal(verifyBlock({ kind: 'manual', status: 'running', deleted_at: null }), 'not_succeeded');
});
