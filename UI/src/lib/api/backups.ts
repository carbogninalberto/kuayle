import { api } from './client';

export type BackupScheduleKind = 'daily' | 'weekly' | 'cron';
export type BackupRetentionKind = 'count' | 'days';
export type BackupRunKind = 'scheduled' | 'manual' | 'pre_upgrade';
export type BackupRunStatus = 'running' | 'succeeded' | 'failed';
export type BackupUploadStatus = 'none' | 'uploading' | 'uploaded' | 'failed';

/** Instance backup configuration. Secrets are write-only: only `has_*` flags come back. */
export interface BackupSettings {
	enabled: boolean;
	schedule_kind: BackupScheduleKind;
	/** HH:MM, UTC. */
	schedule_time: string;
	/** 0 = Sunday .. 6 = Saturday. */
	schedule_weekday: number;
	schedule_cron: string;
	retention_kind: BackupRetentionKind;
	retention_value: number;
	s3_enabled: boolean;
	s3_endpoint: string;
	s3_bucket: string;
	s3_region: string;
	s3_prefix: string;
	has_access_key: boolean;
	has_secret_key: boolean;
	encryption_enabled: boolean;
	has_passphrase: boolean;
	next_run_at: string | null;
	updated_at: string;
	backup_dir: string;
}

/** Save body. An omitted secret keeps the stored value; the clear flags remove it. */
export interface BackupSettingsUpdate {
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
	s3_access_key?: string;
	s3_secret_key?: string;
	clear_s3_credentials?: boolean;
	encryption_enabled: boolean;
	passphrase?: string;
	clear_passphrase?: boolean;
}

/** An unsaved destination; empty keys fall back to the stored ones. */
export interface BackupConnectionTest {
	s3_endpoint: string;
	s3_bucket: string;
	s3_region: string;
	s3_prefix: string;
	s3_access_key?: string;
	s3_secret_key?: string;
}

export interface BackupRun {
	id: string;
	kind: BackupRunKind;
	status: BackupRunStatus;
	upload_status: BackupUploadStatus;
	started_at: string;
	finished_at: string | null;
	duration_ms: number | null;
	size_bytes: number | null;
	checksum_sha256: string | null;
	file_name: string;
	s3_key: string;
	destination: string;
	encrypted: boolean;
	error: string | null;
	verified_at: string | null;
	verify_error: string | null;
	deleted_at: string | null;
}

export function getBackupSettings(): Promise<BackupSettings> {
	return api.get<BackupSettings>('/api/system/backups/settings');
}

export function updateBackupSettings(body: BackupSettingsUpdate): Promise<BackupSettings> {
	return api.put<BackupSettings>('/api/system/backups/settings', body);
}

export function testBackupConnection(body: BackupConnectionTest): Promise<{ ok: boolean }> {
	return api.post<{ ok: boolean }>('/api/system/backups/test-connection', body);
}

export function runBackupNow(): Promise<BackupRun> {
	return api.post<BackupRun>('/api/system/backups/run');
}

export async function listBackupRuns(limit = 50): Promise<BackupRun[]> {
	const res = await api.get<{ runs: BackupRun[] }>(`/api/system/backups/runs?limit=${limit}`);
	return res?.runs ?? [];
}

export function verifyBackupRun(id: string): Promise<BackupRun> {
	return api.post<BackupRun>(`/api/system/backups/runs/${encodeURIComponent(id)}/verify`);
}
