-- Instance backups (Settings -> Backups, sysadmins only). backup_settings is a
-- singleton row; its lease columns let only one backend replica run a backup
-- at a time, and next_run_at is claimed with compare-and-set by the scheduler.
-- S3 credentials and the passphrase are AES-GCM encrypted by the backend and
-- never returned by the API.
CREATE TABLE backup_settings (
    id SMALLINT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    schedule_kind TEXT NOT NULL DEFAULT 'daily' CHECK (schedule_kind IN ('daily', 'weekly', 'cron')),
    schedule_time TEXT NOT NULL DEFAULT '03:00' CHECK (schedule_time ~ '^([01][0-9]|2[0-3]):[0-5][0-9]$'),
    schedule_weekday SMALLINT NOT NULL DEFAULT 0 CHECK (schedule_weekday BETWEEN 0 AND 6),
    schedule_cron TEXT NOT NULL DEFAULT '',
    retention_kind TEXT NOT NULL DEFAULT 'count' CHECK (retention_kind IN ('count', 'days')),
    retention_value INT NOT NULL DEFAULT 7 CHECK (retention_value > 0),
    s3_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    s3_endpoint TEXT NOT NULL DEFAULT '',
    s3_bucket TEXT NOT NULL DEFAULT '',
    s3_region TEXT NOT NULL DEFAULT '',
    s3_prefix TEXT NOT NULL DEFAULT '',
    s3_access_key_encrypted TEXT NOT NULL DEFAULT '',
    s3_secret_key_encrypted TEXT NOT NULL DEFAULT '',
    encryption_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    passphrase_encrypted TEXT NOT NULL DEFAULT '',
    next_run_at TIMESTAMPTZ NULL,
    lease_token UUID NULL,
    lease_expires_at TIMESTAMPTZ NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_by UUID NULL REFERENCES users(id) ON DELETE SET NULL,
    CHECK ((lease_token IS NULL) = (lease_expires_at IS NULL))
);

INSERT INTO backup_settings (id) VALUES (1);

-- One row per backup run, including the updater's pre-upgrade dump, which
-- selfhosting/release_update.py records after migrations succeed. error and
-- verify_error are stored already sanitized (no secrets).
CREATE TABLE backup_runs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    kind TEXT NOT NULL CHECK (kind IN ('scheduled', 'manual', 'pre_upgrade')),
    status TEXT NOT NULL CHECK (status IN ('running', 'succeeded', 'failed')),
    upload_status TEXT NOT NULL DEFAULT 'none' CHECK (upload_status IN ('none', 'uploading', 'uploaded', 'failed')),
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at TIMESTAMPTZ NULL,
    duration_ms BIGINT NULL,
    size_bytes BIGINT NULL,
    checksum_sha256 TEXT NULL,
    local_path TEXT NOT NULL DEFAULT '',
    s3_key TEXT NOT NULL DEFAULT '',
    -- Where the bucket copy went, so retention and Verify never act on a
    -- copy through a bucket or endpoint that has since changed.
    s3_endpoint TEXT NOT NULL DEFAULT '',
    s3_bucket TEXT NOT NULL DEFAULT '',
    destination TEXT NOT NULL DEFAULT '',
    encrypted BOOLEAN NOT NULL DEFAULT FALSE,
    error TEXT NULL,
    verified_at TIMESTAMPTZ NULL,
    verify_error TEXT NULL,
    deleted_at TIMESTAMPTZ NULL,
    created_by UUID NULL REFERENCES users(id) ON DELETE SET NULL
);

CREATE INDEX backup_runs_started_at ON backup_runs (started_at DESC);
