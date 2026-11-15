package domain

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Backup schedule kinds. Times are always UTC.
const (
	BackupScheduleDaily  = "daily"
	BackupScheduleWeekly = "weekly"
	BackupScheduleCron   = "cron"
)

// Backup retention kinds: keep the last N successful backups, or the ones
// from the last N days.
const (
	BackupRetentionCount = "count"
	BackupRetentionDays  = "days"
)

// Backup run kinds, statuses and upload statuses, as stored in backup_runs.
const (
	BackupKindScheduled  = "scheduled"
	BackupKindManual     = "manual"
	BackupKindPreUpgrade = "pre_upgrade"

	BackupStatusRunning   = "running"
	BackupStatusSucceeded = "succeeded"
	BackupStatusFailed    = "failed"

	BackupUploadNone      = "none"
	BackupUploadUploading = "uploading"
	BackupUploadUploaded  = "uploaded"
	BackupUploadFailed    = "failed"
)

// BackupRetentionMax bounds the retention value accepted from the API.
const BackupRetentionMax = 1000

// ErrInvalidBackupSchedule reports a schedule the scheduler cannot run.
var ErrInvalidBackupSchedule = errors.New("invalid backup schedule")

var backupClock = regexp.MustCompile(`^([01][0-9]|2[0-3]):([0-5][0-9])$`)

// BackupSchedule is when scheduled backups run, in UTC.
type BackupSchedule struct {
	Kind    string `db:"schedule_kind"`
	Time    string `db:"schedule_time"`
	Weekday int    `db:"schedule_weekday"`
	Cron    string `db:"schedule_cron"`
}

// Validate reports whether the schedule can be run.
func (s BackupSchedule) Validate() error {
	switch s.Kind {
	case BackupScheduleDaily, BackupScheduleWeekly:
		if !backupClock.MatchString(s.Time) {
			return fmt.Errorf("%w: time must be HH:MM", ErrInvalidBackupSchedule)
		}
		if s.Kind == BackupScheduleWeekly && (s.Weekday < 0 || s.Weekday > 6) {
			return fmt.Errorf("%w: weekday must be 0-6", ErrInvalidBackupSchedule)
		}
		return nil
	case BackupScheduleCron:
		_, err := ParseBackupCron(s.Cron)
		return err
	default:
		return fmt.Errorf("%w: kind must be daily, weekly or cron", ErrInvalidBackupSchedule)
	}
}

// NextRun returns the first scheduled time strictly after after, in UTC.
func (s BackupSchedule) NextRun(after time.Time) (time.Time, error) {
	if err := s.Validate(); err != nil {
		return time.Time{}, err
	}
	after = after.UTC()
	if s.Kind == BackupScheduleCron {
		spec, _ := ParseBackupCron(s.Cron)
		return spec.Next(after)
	}
	match := backupClock.FindStringSubmatch(s.Time)
	hour, _ := strconv.Atoi(match[1])
	minute, _ := strconv.Atoi(match[2])
	at := func(dayOffset int) time.Time {
		return time.Date(after.Year(), after.Month(), after.Day()+dayOffset, hour, minute, 0, 0, time.UTC)
	}
	offset, step := 0, 1
	if s.Kind == BackupScheduleWeekly {
		offset = (s.Weekday - int(after.Weekday()) + 7) % 7
		step = 7
	}
	next := at(offset)
	if !next.After(after) {
		next = at(offset + step)
	}
	return next, nil
}

// BackupCron is a parsed five-field cron expression (minute hour
// day-of-month month day-of-week), evaluated in UTC. Each field accepts *,
// a number, a range a-b, a step */s or a-b/s, and comma-separated lists of
// those. Day of week is 0-6 with Sunday as 0 (7 is accepted as Sunday too).
// As in standard cron, when both day fields are restricted a day matches
// either of them.
type BackupCron struct {
	minute, hour, dom, month, dow uint64
	domAny, dowAny                bool
}

// ParseBackupCron parses a five-field cron expression.
func ParseBackupCron(expr string) (BackupCron, error) {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return BackupCron{}, fmt.Errorf("%w: cron needs 5 fields (minute hour day month weekday)", ErrInvalidBackupSchedule)
	}
	var spec BackupCron
	var err error
	bounds := []struct {
		name     string
		min, max int
		into     *uint64
	}{
		{"minute", 0, 59, &spec.minute},
		{"hour", 0, 23, &spec.hour},
		{"day of month", 1, 31, &spec.dom},
		{"month", 1, 12, &spec.month},
		{"day of week", 0, 7, &spec.dow},
	}
	for i, b := range bounds {
		if *b.into, err = parseCronField(fields[i], b.min, b.max); err != nil {
			return BackupCron{}, fmt.Errorf("%w: %s: %v", ErrInvalidBackupSchedule, b.name, err)
		}
	}
	if spec.dow&(1<<7) != 0 {
		spec.dow |= 1
		spec.dow &^= 1 << 7
	}
	spec.domAny = fields[2] == "*"
	spec.dowAny = fields[4] == "*"
	return spec, nil
}

func parseCronField(field string, min, max int) (uint64, error) {
	var bits uint64
	for _, part := range strings.Split(field, ",") {
		rangePart, step := part, 1
		if i := strings.IndexByte(part, '/'); i >= 0 {
			n, err := cronNumber(part[i+1:])
			if err != nil || n < 1 {
				return 0, fmt.Errorf("invalid step in %q", part)
			}
			rangePart, step = part[:i], n
		}
		lo, hi := min, max
		switch {
		case rangePart == "*":
		case strings.Contains(rangePart, "-"):
			a, b, _ := strings.Cut(rangePart, "-")
			var errA, errB error
			lo, errA = cronNumber(a)
			hi, errB = cronNumber(b)
			if errA != nil || errB != nil || lo > hi {
				return 0, fmt.Errorf("invalid range %q", rangePart)
			}
		default:
			n, err := cronNumber(rangePart)
			if err != nil {
				return 0, fmt.Errorf("invalid value %q", rangePart)
			}
			if step != 1 {
				return 0, fmt.Errorf("a step needs * or a range in %q", part)
			}
			lo, hi = n, n
		}
		if lo < min || hi > max {
			return 0, fmt.Errorf("%q is outside %d-%d", part, min, max)
		}
		for v := lo; v <= hi; v += step {
			bits |= 1 << uint(v)
		}
	}
	return bits, nil
}

func cronNumber(s string) (int, error) {
	if s == "" || len(s) > 2 {
		return 0, errors.New("not a number")
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, errors.New("not a number")
		}
	}
	return strconv.Atoi(s)
}

func (c BackupCron) dayMatches(t time.Time) bool {
	dom := c.dom&(1<<uint(t.Day())) != 0
	dow := c.dow&(1<<uint(t.Weekday())) != 0
	switch {
	case c.domAny && c.dowAny:
		return true
	case c.domAny:
		return dow
	case c.dowAny:
		return dom
	default:
		return dom || dow
	}
}

// Next returns the first matching minute strictly after after, in UTC.
// An expression that never matches (such as 30 February) is an error.
func (c BackupCron) Next(after time.Time) (time.Time, error) {
	t := after.UTC().Truncate(time.Minute).Add(time.Minute)
	limit := t.AddDate(5, 0, 0)
	for t.Before(limit) {
		switch {
		case c.month&(1<<uint(t.Month())) == 0:
			t = time.Date(t.Year(), t.Month()+1, 1, 0, 0, 0, 0, time.UTC)
		case !c.dayMatches(t):
			t = time.Date(t.Year(), t.Month(), t.Day()+1, 0, 0, 0, 0, time.UTC)
		case c.hour&(1<<uint(t.Hour())) == 0:
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour()+1, 0, 0, 0, time.UTC)
		case c.minute&(1<<uint(t.Minute())) == 0:
			t = t.Add(time.Minute)
		default:
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("%w: the cron expression never matches", ErrInvalidBackupSchedule)
}

// BackupRetention is how many successful backups are kept.
type BackupRetention struct {
	Kind  string `db:"retention_kind"`
	Value int    `db:"retention_value"`
}

// Validate reports whether the retention policy is usable.
func (r BackupRetention) Validate() error {
	if r.Kind != BackupRetentionCount && r.Kind != BackupRetentionDays {
		return errors.New("retention kind must be count or days")
	}
	if r.Value < 1 || r.Value > BackupRetentionMax {
		return fmt.Errorf("retention value must be 1-%d", BackupRetentionMax)
	}
	return nil
}

// BackupSettings is the instance's backup configuration (one row). The
// encrypted fields are AES-GCM ciphertexts and never leave the backend.
type BackupSettings struct {
	Enabled bool `db:"enabled"`
	BackupSchedule
	BackupRetention
	S3Enabled            bool       `db:"s3_enabled"`
	S3Endpoint           string     `db:"s3_endpoint"`
	S3Bucket             string     `db:"s3_bucket"`
	S3Region             string     `db:"s3_region"`
	S3Prefix             string     `db:"s3_prefix"`
	S3AccessKeyEncrypted string     `db:"s3_access_key_encrypted"`
	S3SecretKeyEncrypted string     `db:"s3_secret_key_encrypted"`
	EncryptionEnabled    bool       `db:"encryption_enabled"`
	PassphraseEncrypted  string     `db:"passphrase_encrypted"`
	NextRunAt            *time.Time `db:"next_run_at"`
	UpdatedAt            time.Time  `db:"updated_at"`
	UpdatedBy            *uuid.UUID `db:"updated_by"`
}

// BackupRun is one backup in the history.
type BackupRun struct {
	ID             uuid.UUID  `db:"id"`
	Kind           string     `db:"kind"`
	Status         string     `db:"status"`
	UploadStatus   string     `db:"upload_status"`
	StartedAt      time.Time  `db:"started_at"`
	FinishedAt     *time.Time `db:"finished_at"`
	DurationMS     *int64     `db:"duration_ms"`
	SizeBytes      *int64     `db:"size_bytes"`
	ChecksumSHA256 *string    `db:"checksum_sha256"`
	LocalPath      string     `db:"local_path"`
	S3Key          string     `db:"s3_key"`
	Destination    string     `db:"destination"`
	Encrypted      bool       `db:"encrypted"`
	Error          *string    `db:"error"`
	VerifiedAt     *time.Time `db:"verified_at"`
	VerifyError    *string    `db:"verify_error"`
	DeletedAt      *time.Time `db:"deleted_at"`
	CreatedBy      *uuid.UUID `db:"created_by"`
}

// SelectForRetention returns the backups that policy no longer keeps at now.
// It only considers successful, not yet deleted backups made by the backend
// (the updater's pre-upgrade dumps live on the host and are not managed
// here). It never returns the newest successful backup, a backup that is
// still uploading or a running one, so retention cannot leave the instance
// without a backup or delete a file that is being copied.
func SelectForRetention(runs []BackupRun, policy BackupRetention, now time.Time) []BackupRun {
	kept := make([]BackupRun, 0, len(runs))
	for _, run := range runs {
		if run.Status == BackupStatusSucceeded && run.DeletedAt == nil && run.Kind != BackupKindPreUpgrade {
			kept = append(kept, run)
		}
	}
	sort.SliceStable(kept, func(i, j int) bool { return kept[i].StartedAt.After(kept[j].StartedAt) })
	cutoff := now.AddDate(0, 0, -policy.Value)
	var expired []BackupRun
	for i, run := range kept {
		if i == 0 || run.UploadStatus == BackupUploadUploading {
			continue
		}
		switch policy.Kind {
		case BackupRetentionCount:
			if i < policy.Value {
				continue
			}
		case BackupRetentionDays:
			if !run.StartedAt.Before(cutoff) {
				continue
			}
		default:
			continue
		}
		expired = append(expired, run)
	}
	return expired
}

// Backup archive layout. An archive is a gzip-compressed tar holding
// manifest.json, database.dump (pg_dump custom format) and either the
// uploads/ directory or uploads-manifest.json when uploads live in S3.
const (
	BackupFormatVersion       = 1
	BackupManifestFile        = "manifest.json"
	BackupDatabaseFile        = "database.dump"
	BackupUploadsDir          = "uploads/"
	BackupUploadsManifestFile = "uploads-manifest.json"

	BackupUploadsModeFiles      = "files"
	BackupUploadsModeS3Manifest = "s3-manifest"
)

// BackupManifest describes an archive's contents; Verify checks the
// database dump against it.
type BackupManifest struct {
	FormatVersion int                   `json:"format_version"`
	CreatedAt     time.Time             `json:"created_at"`
	Kind          string                `json:"kind"`
	AppVersion    string                `json:"app_version"`
	Database      BackupManifestEntry   `json:"database"`
	Uploads       BackupManifestUploads `json:"uploads"`
}

// BackupManifestEntry is a file inside the archive with its size and hash.
type BackupManifestEntry struct {
	File   string `json:"file"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// BackupManifestUploads summarizes the uploads in the archive.
type BackupManifestUploads struct {
	Mode  string `json:"mode"`
	Count int    `json:"count"`
	Bytes int64  `json:"bytes"`
	// File is uploads-manifest.json in s3-manifest mode.
	File string `json:"file,omitempty"`
}

// BackupUploadObject is one object listed in uploads-manifest.json.
type BackupUploadObject struct {
	Key  string `json:"key"`
	Size int64  `json:"size"`
	ETag string `json:"etag,omitempty"`
}
