package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/pkg/crypto"
	"github.com/kuayle/kuayle-backend/pkg/storage"
	log "github.com/sirupsen/logrus"
)

// Backup errors the handler maps to responses.
var (
	ErrBackupRunning     = errors.New("a backup is already running")
	ErrBackupRunNotFound = repository.ErrBackupRunNotFound
)

// BackupValidationError is a user-facing validation message.
type BackupValidationError struct{ Message string }

func (e *BackupValidationError) Error() string { return e.Message }

func backupInvalid(message string) error { return &BackupValidationError{Message: message} }

const (
	backupLeaseTTL        = 10 * time.Minute
	backupLeaseRenewEvery = 2 * time.Minute
	backupUploadAttempts  = 5 // the first try and four retries
	backupMinPassphrase   = 12
	backupNotifyType      = "backup_failed"
	// PreUpgradeVerifyMessage explains why the backend cannot verify the
	// updater's pre-upgrade dump.
	PreUpgradeVerifyMessage = "stored on the host by the updater; verify it with the commands in docs/backups.md"
)

// BackupServiceConfig holds the operator settings of the backup service.
type BackupServiceConfig struct {
	Dir                 string
	Timeout             time.Duration
	DatabaseURL         string
	AllowPrivateNetwork bool
	// SecretKey encrypts the S3 credentials and the passphrase at rest.
	SecretKey  []byte
	Sysadmins  []uuid.UUID
	AppVersion string
}

// BackupService runs scheduled and manual instance backups. One backup runs
// at a time across replicas, enforced by a lease in backup_settings.
type BackupService struct {
	repo     repository.BackupRepo
	notifier notificationCreator
	cfg      BackupServiceConfig
	uploads  BackupUploads
	dumper   backupDumper
	openS3   func(BackupS3Destination) (backupObjectStore, error)
	now      func() time.Time
	sleep    func(context.Context, time.Duration) error
	logger   log.FieldLogger
	runs     sync.WaitGroup

	// base is the context of the scheduler loop; manual runs started by
	// RunNow outlive their request but stop with the server.
	baseMu sync.Mutex
	base   context.Context
}

// NewBackupService builds the service. uploads names the upload directory,
// or a lister when uploads live in object storage.
func NewBackupService(repo repository.BackupRepo, notifier notificationCreator, cfg BackupServiceConfig, uploads BackupUploads) *BackupService {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 2 * time.Hour
	}
	s := &BackupService{
		repo:     repo,
		notifier: notifier,
		cfg:      cfg,
		uploads:  uploads,
		dumper:   pgDumper{databaseURL: cfg.DatabaseURL},
		now:      time.Now,
		sleep:    sleepContext,
		logger:   log.StandardLogger(),
	}
	s.openS3 = func(dest BackupS3Destination) (backupObjectStore, error) {
		return newBackupObjectStore(dest, cfg.AllowPrivateNetwork)
	}
	return s
}

// BackupUploadsFromDir archives the files of a local upload directory.
func BackupUploadsFromDir(dir string) BackupUploads { return BackupUploads{LocalDir: dir} }

// BackupUploadsFromBackend records the object list when the storage backend
// can list its objects, and archives localDir otherwise.
func BackupUploadsFromBackend(backend storage.Backend, localDir string) BackupUploads {
	if lister, ok := backend.(storage.Lister); ok {
		return BackupUploads{Lister: lister}
	}
	return BackupUploads{LocalDir: localDir}
}

// ---- Settings ----

// BackupSettingsInput is an update from the Settings page. A nil or empty
// secret keeps the stored one; a Clear flag removes it.
type BackupSettingsInput struct {
	Enabled           bool
	Schedule          domain.BackupSchedule
	Retention         domain.BackupRetention
	S3Enabled         bool
	S3Endpoint        string
	S3Bucket          string
	S3Region          string
	S3Prefix          string
	S3AccessKey       *string
	S3SecretKey       *string
	ClearS3Secrets    bool
	EncryptionEnabled bool
	Passphrase        *string
	ClearPassphrase   bool
}

func (s *BackupService) GetSettings(ctx context.Context) (*domain.BackupSettings, error) {
	return s.repo.GetSettings(ctx)
}

// UpdateSettings validates and saves the settings and schedules the next run.
func (s *BackupService) UpdateSettings(ctx context.Context, userID uuid.UUID, in BackupSettingsInput) (*domain.BackupSettings, error) {
	current, err := s.repo.GetSettings(ctx)
	if err != nil {
		return nil, err
	}
	if err := in.Schedule.Validate(); err != nil {
		return nil, backupInvalid(strings.TrimPrefix(err.Error(), domain.ErrInvalidBackupSchedule.Error()+": "))
	}
	if err := in.Retention.Validate(); err != nil {
		return nil, backupInvalid(err.Error())
	}
	next := *current
	next.Enabled = in.Enabled
	// Fields the chosen kind does not use keep their stored values, which
	// always satisfy the table's checks.
	next.BackupSchedule.Kind = in.Schedule.Kind
	if in.Schedule.Kind != domain.BackupScheduleCron {
		next.Time = in.Schedule.Time
	}
	if in.Schedule.Kind == domain.BackupScheduleWeekly {
		next.Weekday = in.Schedule.Weekday
	}
	if in.Schedule.Kind == domain.BackupScheduleCron {
		next.Cron = strings.Join(strings.Fields(in.Schedule.Cron), " ")
	}
	next.BackupRetention = in.Retention
	next.S3Enabled = in.S3Enabled
	next.S3Endpoint = strings.TrimRight(strings.TrimSpace(in.S3Endpoint), "/")
	next.S3Bucket = strings.TrimSpace(in.S3Bucket)
	next.S3Region = strings.TrimSpace(in.S3Region)
	next.S3Prefix = strings.Trim(strings.TrimSpace(in.S3Prefix), "/")
	next.EncryptionEnabled = in.EncryptionEnabled
	if in.ClearS3Secrets {
		next.S3AccessKeyEncrypted, next.S3SecretKeyEncrypted = "", ""
	}
	if err := s.setSecret(&next.S3AccessKeyEncrypted, in.S3AccessKey); err != nil {
		return nil, err
	}
	if err := s.setSecret(&next.S3SecretKeyEncrypted, in.S3SecretKey); err != nil {
		return nil, err
	}
	if in.ClearPassphrase {
		next.PassphraseEncrypted = ""
	}
	if in.Passphrase != nil && *in.Passphrase != "" && len(*in.Passphrase) < backupMinPassphrase {
		return nil, backupInvalid(fmt.Sprintf("the passphrase must have at least %d characters", backupMinPassphrase))
	}
	if err := s.setSecret(&next.PassphraseEncrypted, in.Passphrase); err != nil {
		return nil, err
	}
	if next.S3Enabled {
		dest, err := s.destination(&next)
		if err != nil {
			return nil, err
		}
		if err := validateBackupS3Destination(dest); err != nil {
			return nil, err
		}
	} else if next.S3Endpoint != "" {
		if err := validateBackupS3Destination(BackupS3Destination{Endpoint: next.S3Endpoint, Bucket: "-", Region: "-", AccessKey: "-", SecretKey: "-"}); err != nil {
			return nil, err
		}
	}
	if next.EncryptionEnabled && next.PassphraseEncrypted == "" {
		return nil, backupInvalid("a passphrase is required for encrypted backups")
	}
	next.NextRunAt = nil
	if next.Enabled {
		at, err := next.BackupSchedule.NextRun(s.now())
		if err != nil {
			return nil, backupInvalid(err.Error())
		}
		next.NextRunAt = &at
	}
	next.UpdatedBy = &userID
	if err := s.repo.UpdateSettings(ctx, &next); err != nil {
		return nil, err
	}
	return &next, nil
}

func (s *BackupService) setSecret(field *string, value *string) error {
	if value == nil || *value == "" {
		return nil
	}
	encrypted, err := crypto.Encrypt(*value, s.cfg.SecretKey)
	if err != nil {
		return errors.New("could not encrypt a backup secret")
	}
	*field = encrypted
	return nil
}

func (s *BackupService) decrypt(encrypted string) (string, error) {
	if encrypted == "" {
		return "", nil
	}
	plain, err := crypto.Decrypt(encrypted, s.cfg.SecretKey)
	if err != nil {
		return "", errors.New("a stored backup secret cannot be decrypted; save it again")
	}
	return plain, nil
}

func (s *BackupService) destination(settings *domain.BackupSettings) (BackupS3Destination, error) {
	access, err := s.decrypt(settings.S3AccessKeyEncrypted)
	if err != nil {
		return BackupS3Destination{}, err
	}
	secret, err := s.decrypt(settings.S3SecretKeyEncrypted)
	if err != nil {
		return BackupS3Destination{}, err
	}
	return BackupS3Destination{
		Endpoint: settings.S3Endpoint, Bucket: settings.S3Bucket, Region: settings.S3Region,
		Prefix: settings.S3Prefix, AccessKey: access, SecretKey: secret,
	}, nil
}

// TestConnection writes and deletes an object with the given destination.
// Empty credentials fall back to the stored ones, so an unsaved form can be
// tested without retyping them. The returned error is safe to show.
func (s *BackupService) TestConnection(ctx context.Context, dest BackupS3Destination) error {
	settings, err := s.repo.GetSettings(ctx)
	if err != nil {
		return err
	}
	stored, err := s.destination(settings)
	if err != nil {
		return backupInvalid(err.Error())
	}
	if dest.AccessKey == "" {
		dest.AccessKey = stored.AccessKey
	}
	if dest.SecretKey == "" {
		dest.SecretKey = stored.SecretKey
	}
	dest.Endpoint = strings.TrimRight(strings.TrimSpace(dest.Endpoint), "/")
	dest.Prefix = strings.Trim(strings.TrimSpace(dest.Prefix), "/")
	if err := validateBackupS3Destination(dest); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	store, err := s.openS3(dest)
	if err == nil {
		err = testBackupObjectStore(ctx, store, dest)
	}
	if err != nil {
		message := s.sanitize(err, dest.AccessKey, dest.SecretKey)
		s.logger.WithField("error", message).Warn("backup S3 connection test failed")
		return backupInvalid("connection test failed: " + message)
	}
	return nil
}

// ---- History ----

func (s *BackupService) ListRuns(ctx context.Context, limit, offset int) ([]domain.BackupRun, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	return s.repo.ListRuns(ctx, limit, offset)
}

// ---- Runs ----

// Run starts scheduled backups every minute until ctx ends. Each due slot is
// claimed in the database first, so with several replicas only one runs it.
func (s *BackupService) Run(ctx context.Context) {
	s.baseMu.Lock()
	s.base = ctx
	s.baseMu.Unlock()
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		s.recoverInterrupted(ctx)
		s.runDue(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *BackupService) recoverInterrupted(ctx context.Context) {
	marked, err := s.repo.MarkStaleRunning(ctx, s.now())
	if err != nil {
		s.logger.WithError(err).Warn("could not recover interrupted backup runs")
	} else if marked > 0 {
		s.logger.WithField("runs", marked).Warn("marked interrupted backup runs as failed")
	}
}

// runDue runs the scheduled backup when its slot has come and reports
// whether this replica claimed it.
func (s *BackupService) runDue(ctx context.Context) bool {
	settings, err := s.repo.GetSettings(ctx)
	if err != nil {
		s.logger.WithError(err).Warn("could not read backup settings")
		return false
	}
	if !settings.Enabled {
		return false
	}
	now := s.now()
	if settings.NextRunAt != nil && settings.NextRunAt.After(now) {
		return false
	}
	next, err := settings.BackupSchedule.NextRun(now)
	if err != nil {
		s.logger.WithError(err).Warn("backup schedule is invalid")
		return false
	}
	won, err := s.repo.ClaimSchedule(ctx, settings.NextRunAt, next)
	if err != nil || !won || settings.NextRunAt == nil {
		// A schedule without a slot only gets one; it does not run now.
		return false
	}
	token, err := s.acquire(ctx)
	if err != nil {
		if errors.Is(err, ErrBackupRunning) {
			s.logger.Warn("scheduled backup skipped: another backup is running")
		} else {
			s.logger.WithError(err).Warn("scheduled backup could not take the lease")
		}
		return true
	}
	run, err := s.startRun(ctx, domain.BackupKindScheduled, nil, settings)
	if err != nil {
		s.release(token)
		s.logger.WithField("error", s.sanitize(err)).Error("scheduled backup could not start")
		s.notifyFailure(ctx, "Scheduled backup failed: "+s.sanitize(err))
		return true
	}
	s.execute(ctx, token, run, settings)
	return true
}

// RunNow starts a manual backup in the background and returns its run.
// It returns ErrBackupRunning when another backup holds the lease.
func (s *BackupService) RunNow(ctx context.Context, userID uuid.UUID) (*domain.BackupRun, error) {
	settings, err := s.repo.GetSettings(ctx)
	if err != nil {
		return nil, err
	}
	token, err := s.acquire(ctx)
	if err != nil {
		return nil, err
	}
	run, err := s.startRun(ctx, domain.BackupKindManual, &userID, settings)
	if err != nil {
		s.release(token)
		return nil, err
	}
	s.baseMu.Lock()
	background := s.base
	s.baseMu.Unlock()
	if background == nil {
		background = context.Background()
	}
	s.runs.Add(1)
	go func() {
		defer s.runs.Done()
		s.execute(background, token, run, settings)
	}()
	return run, nil
}

// Wait blocks until background runs started by RunNow end.
func (s *BackupService) Wait() { s.runs.Wait() }

func (s *BackupService) acquire(ctx context.Context) (uuid.UUID, error) {
	token := uuid.New()
	won, err := s.repo.AcquireLease(ctx, token, backupLeaseTTL)
	if err != nil {
		return uuid.Nil, err
	}
	if !won {
		return uuid.Nil, ErrBackupRunning
	}
	return token, nil
}

func (s *BackupService) release(token uuid.UUID) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.repo.ReleaseLease(ctx, token); err != nil {
		s.logger.WithError(err).Warn("could not release the backup lease")
	}
}

func (s *BackupService) startRun(ctx context.Context, kind string, userID *uuid.UUID, settings *domain.BackupSettings) (*domain.BackupRun, error) {
	destination := "local:" + s.cfg.Dir
	if settings.S3Enabled {
		destination += " + s3://" + settings.S3Bucket
		if settings.S3Prefix != "" {
			destination += "/" + settings.S3Prefix
		}
	}
	run := &domain.BackupRun{
		ID: uuid.New(), Kind: kind, Status: domain.BackupStatusRunning, UploadStatus: domain.BackupUploadNone,
		Destination: destination, Encrypted: settings.EncryptionEnabled, CreatedBy: userID,
	}
	if err := s.repo.CreateRun(ctx, run); err != nil {
		return nil, err
	}
	return run, nil
}

// execute performs a run under the lease held by token and releases it.
func (s *BackupService) execute(parent context.Context, token uuid.UUID, run *domain.BackupRun, settings *domain.BackupSettings) {
	defer s.release(token)
	ctx, cancel := context.WithTimeout(parent, s.cfg.Timeout)
	defer cancel()
	renewDone := make(chan struct{})
	defer close(renewDone)
	go s.renewLease(ctx, cancel, token, renewDone)

	secrets := []string{}
	fail := func(err error) {
		message := s.sanitize(err, secrets...)
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			message = "timed out after " + s.cfg.Timeout.String() + ": " + message
		}
		s.finish(run, domain.BackupStatusFailed, &message)
		s.logger.WithFields(log.Fields{"run_id": run.ID, "kind": run.Kind, "error": message}).Error("backup failed")
		if run.Kind == domain.BackupKindScheduled {
			s.notifyFailure(parent, "Scheduled backup failed: "+message)
		}
	}

	passphrase := ""
	if settings.EncryptionEnabled {
		var err error
		if passphrase, err = s.decrypt(settings.PassphraseEncrypted); err != nil {
			fail(err)
			return
		}
		if passphrase == "" {
			fail(errors.New("encryption is enabled but no passphrase is stored"))
			return
		}
		secrets = append(secrets, passphrase)
	}
	var dest BackupS3Destination
	if settings.S3Enabled {
		var err error
		if dest, err = s.destination(settings); err != nil {
			fail(err)
			return
		}
		secrets = append(secrets, dest.AccessKey, dest.SecretKey)
	}

	archive, err := s.writeBackupArchive(ctx, run, passphrase)
	if err != nil {
		fail(err)
		return
	}
	run.LocalPath, run.SizeBytes, run.ChecksumSHA256 = archive.Path, &archive.Size, &archive.Checksum

	var uploadErr error
	if settings.S3Enabled {
		run.UploadStatus = domain.BackupUploadUploading
		run.S3Key = dest.Key(backupObjectName(archive.Path))
		s.save(run)
		uploadErr = s.upload(ctx, dest, run, archive)
		if uploadErr != nil {
			run.UploadStatus = domain.BackupUploadFailed
		} else {
			run.UploadStatus = domain.BackupUploadUploaded
		}
	}
	if uploadErr != nil {
		// The local backup is good; the run succeeded but the copy did not.
		message := "upload failed: " + s.sanitize(uploadErr, secrets...)
		s.finish(run, domain.BackupStatusSucceeded, &message)
		s.logger.WithFields(log.Fields{"run_id": run.ID, "error": message}).Error("backup upload failed")
		if run.Kind == domain.BackupKindScheduled {
			s.notifyFailure(parent, "Scheduled backup was not copied to S3: "+message)
		}
	} else {
		s.finish(run, domain.BackupStatusSucceeded, nil)
		s.logger.WithFields(log.Fields{"run_id": run.ID, "kind": run.Kind, "size": archive.Size}).Info("backup finished")
	}
	s.applyRetention(ctx, settings, secrets)
}

func (s *BackupService) renewLease(ctx context.Context, cancel context.CancelFunc, token uuid.UUID, done <-chan struct{}) {
	ticker := time.NewTicker(backupLeaseRenewEvery)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			held, err := s.repo.RenewLease(ctx, token, backupLeaseTTL)
			if err == nil && !held {
				s.logger.Error("backup lease was lost; stopping the run")
				cancel()
				return
			}
		}
	}
}

func (s *BackupService) finish(run *domain.BackupRun, status string, message *string) {
	finished := s.now()
	duration := finished.Sub(run.StartedAt).Milliseconds()
	if duration < 0 {
		duration = 0
	}
	run.Status, run.Error, run.FinishedAt, run.DurationMS = status, message, &finished, &duration
	s.save(run)
}

func (s *BackupService) save(run *domain.BackupRun) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.repo.UpdateRun(ctx, run); err != nil {
		s.logger.WithError(err).WithField("run_id", run.ID).Error("could not save a backup run")
	}
}

// upload copies the archive and its checksum file, retrying with
// exponential backoff (1s, 2s, 4s, 8s, with jitter).
func (s *BackupService) upload(ctx context.Context, dest BackupS3Destination, run *domain.BackupRun, archive archiveResult) error {
	store, err := s.openS3(dest)
	if err != nil {
		return err
	}
	sidecar := []byte(archive.Checksum + "  " + backupObjectName(archive.Path) + "\n")
	delay := time.Second
	for attempt := 1; ; attempt++ {
		err = store.PutFile(ctx, run.S3Key, archive.Path, archive.Checksum)
		if err == nil {
			err = store.PutBytes(ctx, run.S3Key+".sha256", sidecar)
		}
		if err == nil || attempt == backupUploadAttempts || ctx.Err() != nil {
			return err
		}
		s.logger.WithFields(log.Fields{"run_id": run.ID, "attempt": attempt, "error": s.sanitize(err, dest.AccessKey, dest.SecretKey)}).Warn("backup upload failed; retrying")
		jitter := time.Duration(rand.Int64N(int64(delay) / 5))
		if sleepErr := s.sleep(ctx, delay+jitter); sleepErr != nil {
			return err
		}
		delay *= 2
	}
}

// applyRetention removes the backups the policy no longer keeps, locally and
// in the bucket. A backup is marked deleted only once all its files are gone.
func (s *BackupService) applyRetention(ctx context.Context, settings *domain.BackupSettings, secrets []string) {
	runs, err := s.repo.ListRetainable(ctx)
	if err != nil {
		s.logger.WithError(err).Warn("could not list backups for retention")
		return
	}
	expired := domain.SelectForRetention(runs, settings.BackupRetention, s.now())
	if len(expired) == 0 {
		return
	}
	var store backupObjectStore
	var dest BackupS3Destination
	for _, run := range expired {
		if run.S3Key != "" && run.UploadStatus == domain.BackupUploadUploaded {
			if store == nil {
				if dest, err = s.destination(settings); err == nil && dest.Bucket != "" {
					store, err = s.openS3(dest)
				}
				if store == nil {
					s.logger.WithField("run_id", run.ID).Warn("retention kept a backup: the S3 destination is unavailable")
					continue
				}
			}
			errObject := store.Delete(ctx, run.S3Key)
			errSidecar := store.Delete(ctx, run.S3Key+".sha256")
			if err := errors.Join(errObject, errSidecar); err != nil {
				s.logger.WithFields(log.Fields{"run_id": run.ID, "error": s.sanitize(err, append(secrets, dest.AccessKey, dest.SecretKey)...)}).Warn("retention could not delete a backup from S3")
				continue
			}
		}
		if run.LocalPath != "" {
			errFile := removeIfExists(run.LocalPath)
			errSidecar := removeIfExists(run.LocalPath + ".sha256")
			if err := errors.Join(errFile, errSidecar); err != nil {
				s.logger.WithError(err).WithField("run_id", run.ID).Warn("retention could not delete a local backup")
				continue
			}
		}
		if err := s.repo.MarkDeleted(ctx, run.ID, s.now()); err != nil {
			s.logger.WithError(err).WithField("run_id", run.ID).Warn("could not mark a backup deleted")
		}
	}
}

func removeIfExists(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// notifyFailure tells every sysadmin that a scheduled backup failed. The
// notice goes to the inbox of the first workspace and team they belong to.
func (s *BackupService) notifyFailure(ctx context.Context, title string) {
	if s.notifier == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	if len(title) > 300 {
		title = title[:300]
	}
	for _, adminID := range s.cfg.Sysadmins {
		workspaceID, teamID, ok, err := s.repo.NotificationTarget(ctx, adminID)
		if err != nil || !ok {
			s.logger.WithField("user_id", adminID).Warn("backup failure notice not delivered: the sysadmin is in no team")
			continue
		}
		if err := s.notifier.Create(ctx, adminID, workspaceID, nil, backupNotifyType, title, teamID); err != nil {
			s.logger.WithError(err).WithField("user_id", adminID).Warn("could not store a backup failure notice")
		}
	}
}

// ---- Verify ----

// Verify re-reads a backup (the local file, or the S3 copy when the local
// file is gone), checks it against its recorded checksum and manifest, and
// stores the outcome on the run. It returns the updated run.
func (s *BackupService) Verify(ctx context.Context, runID uuid.UUID) (*domain.BackupRun, error) {
	run, err := s.repo.GetRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	switch {
	case run.Kind == domain.BackupKindPreUpgrade:
		return nil, backupInvalid(PreUpgradeVerifyMessage)
	case run.Status != domain.BackupStatusSucceeded:
		return nil, backupInvalid("only a finished, successful backup can be verified")
	case run.DeletedAt != nil:
		return nil, backupInvalid("this backup was removed by retention")
	case run.ChecksumSHA256 == nil:
		return nil, backupInvalid("this backup has no recorded checksum")
	}
	settings, err := s.repo.GetSettings(ctx)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, s.cfg.Timeout)
	defer cancel()
	verifyErr := s.verifyRun(ctx, run, settings)
	now := s.now()
	run.VerifiedAt = &now
	run.VerifyError = nil
	if verifyErr != nil {
		message := s.sanitize(verifyErr, s.secretsOf(settings)...)
		run.VerifyError = &message
	}
	if err := s.repo.UpdateRun(ctx, run); err != nil {
		return nil, err
	}
	return run, nil
}

func (s *BackupService) verifyRun(ctx context.Context, run *domain.BackupRun, settings *domain.BackupSettings) error {
	passphrase := ""
	if run.Encrypted {
		var err error
		if passphrase, err = s.decrypt(settings.PassphraseEncrypted); err != nil {
			return err
		}
		if passphrase == "" {
			return errors.New("the backup is encrypted and no passphrase is stored")
		}
	}
	var source io.ReadCloser
	file, err := os.Open(run.LocalPath)
	switch {
	case err == nil:
		source = file
	case errors.Is(err, os.ErrNotExist) && run.S3Key != "" && run.UploadStatus == domain.BackupUploadUploaded:
		dest, destErr := s.destination(settings)
		if destErr != nil {
			return destErr
		}
		if !settings.S3Enabled || dest.Bucket == "" {
			return errors.New("the local file is gone and the S3 destination is not configured")
		}
		store, openErr := s.openS3(dest)
		if openErr != nil {
			return openErr
		}
		if source, err = store.Get(ctx, run.S3Key); err != nil {
			return fmt.Errorf("download from S3: %w", err)
		}
	case errors.Is(err, os.ErrNotExist):
		return errors.New("the backup file is missing")
	default:
		return err
	}
	defer source.Close()
	return verifyBackupArchive(&contextReader{ctx: ctx, r: source}, *run.ChecksumSHA256, passphrase)
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (c *contextReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}

// ---- Secrets ----

func (s *BackupService) secretsOf(settings *domain.BackupSettings) []string {
	var secrets []string
	for _, encrypted := range []string{settings.S3AccessKeyEncrypted, settings.S3SecretKeyEncrypted, settings.PassphraseEncrypted} {
		if plain, err := s.decrypt(encrypted); err == nil && plain != "" {
			secrets = append(secrets, plain)
		}
	}
	return secrets
}

// sanitize renders err for storage, logs and responses: it removes the given
// secrets, the database password and any URL credentials, and caps length.
func (s *BackupService) sanitize(err error, secrets ...string) string {
	if err == nil {
		return ""
	}
	if _, password, splitErr := splitDatabasePassword(s.cfg.DatabaseURL); splitErr == nil && password != "" {
		secrets = append(secrets, password)
	}
	return sanitizeBackupMessage(err.Error(), secrets...)
}

var urlUserinfo = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://)[^/@\s]+@`)

func sanitizeBackupMessage(message string, secrets ...string) string {
	for _, secret := range secrets {
		if len(secret) < 3 {
			continue
		}
		message = strings.ReplaceAll(message, secret, "[redacted]")
		if escaped := url.QueryEscape(secret); escaped != secret {
			message = strings.ReplaceAll(message, escaped, "[redacted]")
		}
		if escaped := url.PathEscape(secret); escaped != secret {
			message = strings.ReplaceAll(message, escaped, "[redacted]")
		}
	}
	message = urlUserinfo.ReplaceAllString(message, "${1}[redacted]@")
	message = strings.TrimSpace(message)
	if len(message) > 1000 {
		message = message[:1000] + "…"
	}
	return message
}

// splitDatabasePassword returns the database URL without its password, and
// the password, so pg_dump can receive it in PGPASSWORD.
func splitDatabasePassword(databaseURL string) (string, string, error) {
	parsed, err := url.Parse(databaseURL)
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") {
		return "", "", errors.New("DATABASE_URL must be a postgres:// URL to run backups")
	}
	password := ""
	if parsed.User != nil {
		password, _ = parsed.User.Password()
		parsed.User = url.User(parsed.User.Username())
	}
	query := parsed.Query()
	if p := query.Get("password"); p != "" {
		if password == "" {
			password = p
		}
		query.Del("password")
		parsed.RawQuery = query.Encode()
	}
	return parsed.String(), password, nil
}
