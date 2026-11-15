import type {
	BackupConnectionTest,
	BackupRetentionKind,
	BackupRun,
	BackupScheduleKind,
	BackupSettings,
	BackupSettingsUpdate
} from '../../api/backups';

export const MIN_PASSPHRASE_LENGTH = 12;
export const MIN_RETENTION = 1;
export const MAX_RETENTION = 1000;

/** Editable state of the settings form. Secret inputs always start empty. */
export interface BackupForm {
	enabled: boolean;
	schedule_kind: BackupScheduleKind;
	schedule_time: string;
	schedule_weekday: number;
	schedule_cron: string;
	retention_kind: BackupRetentionKind;
	retention_value: number;
	s3_enabled: boolean;
	s3_endpoint: string;
	s3_bucket: string;
	s3_region: string;
	s3_prefix: string;
	s3_access_key: string;
	s3_secret_key: string;
	clear_s3_credentials: boolean;
	encryption_enabled: boolean;
	passphrase: string;
	clear_passphrase: boolean;
}

export type BackupFormError =
	| 'invalid_time'
	| 'missing_cron'
	| 'invalid_retention'
	| 'short_passphrase'
	| 'missing_passphrase';

export type VerifyBlock = 'pre_upgrade' | 'deleted' | 'not_succeeded';

/** Form state for saved settings; stored secrets are never put back into inputs. */
export function formFromSettings(settings: BackupSettings): BackupForm {
	return {
		enabled: settings.enabled,
		schedule_kind: settings.schedule_kind,
		schedule_time: settings.schedule_time,
		schedule_weekday: settings.schedule_weekday,
		schedule_cron: settings.schedule_cron,
		retention_kind: settings.retention_kind,
		retention_value: settings.retention_value,
		s3_enabled: settings.s3_enabled,
		s3_endpoint: settings.s3_endpoint,
		s3_bucket: settings.s3_bucket,
		s3_region: settings.s3_region,
		s3_prefix: settings.s3_prefix,
		s3_access_key: '',
		s3_secret_key: '',
		clear_s3_credentials: false,
		encryption_enabled: settings.encryption_enabled,
		passphrase: '',
		clear_passphrase: false
	};
}

/** A 24-hour HH:MM time. */
export function isValidTime(value: string): boolean {
	return /^([01]\d|2[0-3]):[0-5]\d$/.test(value.trim());
}

export function validateBackupForm(form: BackupForm, settings: Pick<BackupSettings, 'has_passphrase'>): BackupFormError | null {
	if (form.schedule_kind === 'cron') {
		if (!form.schedule_cron.trim()) return 'missing_cron';
	} else if (!isValidTime(form.schedule_time)) {
		return 'invalid_time';
	}
	const retention = Number(form.retention_value);
	if (!Number.isInteger(retention) || retention < MIN_RETENTION || retention > MAX_RETENTION) return 'invalid_retention';
	const passphrase = form.clear_passphrase ? '' : form.passphrase;
	if (passphrase && passphrase.length < MIN_PASSPHRASE_LENGTH) return 'short_passphrase';
	if (form.encryption_enabled && !passphrase && (form.clear_passphrase || !settings.has_passphrase)) {
		return 'missing_passphrase';
	}
	return null;
}

/**
 * The PUT body. Empty secrets are omitted so the stored ones are kept; a
 * requested clear wins over a value typed into the same field group.
 */
export function buildSettingsPayload(form: BackupForm): BackupSettingsUpdate {
	const payload: BackupSettingsUpdate = {
		enabled: form.enabled,
		schedule_kind: form.schedule_kind,
		schedule_time: form.schedule_time.trim(),
		schedule_weekday: Number(form.schedule_weekday),
		schedule_cron: form.schedule_cron.trim(),
		retention_kind: form.retention_kind,
		retention_value: Number(form.retention_value),
		s3_enabled: form.s3_enabled,
		s3_endpoint: form.s3_endpoint.trim(),
		s3_bucket: form.s3_bucket.trim(),
		s3_region: form.s3_region.trim(),
		s3_prefix: form.s3_prefix.trim(),
		encryption_enabled: form.encryption_enabled
	};
	if (form.clear_s3_credentials) {
		payload.clear_s3_credentials = true;
	} else {
		if (form.s3_access_key.trim()) payload.s3_access_key = form.s3_access_key.trim();
		if (form.s3_secret_key) payload.s3_secret_key = form.s3_secret_key;
	}
	if (form.clear_passphrase) payload.clear_passphrase = true;
	else if (form.passphrase) payload.passphrase = form.passphrase;
	return payload;
}

/** The test-connection body from unsaved form values; empty keys use the stored ones. */
export function buildConnectionTest(form: BackupForm): BackupConnectionTest {
	const body: BackupConnectionTest = {
		s3_endpoint: form.s3_endpoint.trim(),
		s3_bucket: form.s3_bucket.trim(),
		s3_region: form.s3_region.trim(),
		s3_prefix: form.s3_prefix.trim()
	};
	if (form.s3_access_key.trim()) body.s3_access_key = form.s3_access_key.trim();
	if (form.s3_secret_key) body.s3_secret_key = form.s3_secret_key;
	return body;
}

const BYTE_UNITS = ['B', 'KB', 'MB', 'GB', 'TB'];

/** Human-readable size in binary units; empty for an unknown size. */
export function formatBytes(bytes: number | null | undefined): string {
	if (bytes == null || !Number.isFinite(bytes) || bytes < 0) return '';
	let value = bytes;
	let unit = 0;
	while (value >= 1024 && unit < BYTE_UNITS.length - 1) {
		value /= 1024;
		unit++;
	}
	if (unit === 0) return `${value} B`;
	return `${value < 10 ? value.toFixed(1) : Math.round(value)} ${BYTE_UNITS[unit]}`;
}

/** Compact duration; empty for an unknown one. */
export function formatDuration(ms: number | null | undefined): string {
	if (ms == null || !Number.isFinite(ms) || ms < 0) return '';
	if (ms < 1000) return `${Math.round(ms)} ms`;
	const seconds = ms / 1000;
	if (seconds < 60) return `${seconds < 10 ? seconds.toFixed(1) : Math.round(seconds)} s`;
	const total = Math.round(seconds);
	const hours = Math.floor(total / 3600);
	const minutes = Math.floor((total % 3600) / 60);
	const rest = total % 60;
	return hours > 0 ? `${hours}h ${minutes}m` : `${minutes}m ${rest}s`;
}

export function shortChecksum(hash: string | null | undefined): string {
	return hash ? hash.slice(0, 12) : '';
}

/** History keeps refreshing while a backup or its upload is still in progress. */
export function shouldPoll(runs: readonly Pick<BackupRun, 'status' | 'upload_status'>[]): boolean {
	return runs.some((run) => run.status === 'running' || run.upload_status === 'uploading');
}

/** Why a run cannot be verified from the UI, or null when it can. */
export function verifyBlock(run: Pick<BackupRun, 'kind' | 'status' | 'deleted_at'>): VerifyBlock | null {
	if (run.kind === 'pre_upgrade') return 'pre_upgrade';
	if (run.deleted_at) return 'deleted';
	if (run.status !== 'succeeded') return 'not_succeeded';
	return null;
}
