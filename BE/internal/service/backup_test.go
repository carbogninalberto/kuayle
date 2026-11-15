package service

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/pkg/crypto"
	"github.com/kuayle/kuayle-backend/pkg/storage"
	log "github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/require"
)

// ---- fakes ----

type fakeBackupRepo struct {
	mu         sync.Mutex
	settings   domain.BackupSettings
	leaseToken uuid.UUID
	leaseUntil time.Time
	runs       map[uuid.UUID]*domain.BackupRun
	targets    map[uuid.UUID][2]uuid.UUID
	now        func() time.Time
}

func newFakeBackupRepo() *fakeBackupRepo {
	return &fakeBackupRepo{
		settings: domain.BackupSettings{
			BackupSchedule:  domain.BackupSchedule{Kind: "daily", Time: "03:00"},
			BackupRetention: domain.BackupRetention{Kind: "count", Value: 7},
		},
		runs:    map[uuid.UUID]*domain.BackupRun{},
		targets: map[uuid.UUID][2]uuid.UUID{},
		now:     time.Now,
	}
}

func (r *fakeBackupRepo) GetSettings(context.Context) (*domain.BackupSettings, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	copy := r.settings
	return &copy, nil
}

func (r *fakeBackupRepo) UpdateSettings(_ context.Context, s *domain.BackupSettings) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.settings = *s
	return nil
}

func (r *fakeBackupRepo) ClaimSchedule(_ context.Context, expected *time.Time, next time.Time) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	current := r.settings.NextRunAt
	same := (current == nil && expected == nil) || (current != nil && expected != nil && current.Equal(*expected))
	if !r.settings.Enabled || !same {
		return false, nil
	}
	r.settings.NextRunAt = &next
	return true, nil
}

func (r *fakeBackupRepo) AcquireLease(_ context.Context, token uuid.UUID, ttl time.Duration) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.leaseToken != uuid.Nil && r.leaseUntil.After(r.now()) {
		return false, nil
	}
	r.leaseToken, r.leaseUntil = token, r.now().Add(ttl)
	return true, nil
}

func (r *fakeBackupRepo) RenewLease(_ context.Context, token uuid.UUID, ttl time.Duration) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.leaseToken != token {
		return false, nil
	}
	r.leaseUntil = r.now().Add(ttl)
	return true, nil
}

func (r *fakeBackupRepo) ReleaseLease(_ context.Context, token uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.leaseToken == token {
		r.leaseToken = uuid.Nil
	}
	return nil
}

func (r *fakeBackupRepo) CreateRun(_ context.Context, run *domain.BackupRun) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	run.StartedAt = r.now()
	copy := *run
	r.runs[run.ID] = &copy
	return nil
}

func (r *fakeBackupRepo) UpdateRun(_ context.Context, run *domain.BackupRun) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.runs[run.ID]; !ok {
		return repository.ErrBackupRunNotFound
	}
	copy := *run
	r.runs[run.ID] = &copy
	return nil
}

func (r *fakeBackupRepo) sorted(filter func(*domain.BackupRun) bool) []domain.BackupRun {
	out := []domain.BackupRun{}
	for _, run := range r.runs {
		if filter(run) {
			out = append(out, *run)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.After(out[j].StartedAt) })
	return out
}

func (r *fakeBackupRepo) ListRuns(_ context.Context, limit, offset int) ([]domain.BackupRun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	all := r.sorted(func(*domain.BackupRun) bool { return true })
	if offset > len(all) {
		return nil, nil
	}
	all = all[offset:]
	if len(all) > limit {
		all = all[:limit]
	}
	return all, nil
}

func (r *fakeBackupRepo) ListRetainable(context.Context) ([]domain.BackupRun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.sorted(func(run *domain.BackupRun) bool {
		return run.Status == domain.BackupStatusSucceeded && run.DeletedAt == nil && run.Kind != domain.BackupKindPreUpgrade
	}), nil
}

func (r *fakeBackupRepo) GetRun(_ context.Context, id uuid.UUID) (*domain.BackupRun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	run, ok := r.runs[id]
	if !ok {
		return nil, repository.ErrBackupRunNotFound
	}
	copy := *run
	return &copy, nil
}

func (r *fakeBackupRepo) MarkDeleted(_ context.Context, id uuid.UUID, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if run, ok := r.runs[id]; ok {
		run.DeletedAt = &at
	}
	return nil
}

func (r *fakeBackupRepo) MarkStaleRunning(context.Context, time.Time) (int64, error) { return 0, nil }

func (r *fakeBackupRepo) NotificationTarget(_ context.Context, userID uuid.UUID) (uuid.UUID, uuid.UUID, bool, error) {
	target, ok := r.targets[userID]
	return target[0], target[1], ok, nil
}

func (r *fakeBackupRepo) onlyRun(t *testing.T) domain.BackupRun {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	require.Len(t, r.runs, 1)
	for _, run := range r.runs {
		return *run
	}
	return domain.BackupRun{}
}

type fakeDumper struct {
	body []byte
	err  error
}

func (d fakeDumper) Dump(_ context.Context, w io.Writer) error {
	if d.err != nil {
		return d.err
	}
	_, err := w.Write(d.body)
	return err
}

type fakeObject struct {
	body     []byte
	checksum string
}

type fakeStore struct {
	mu        sync.Mutex
	objects   map[string]fakeObject
	failPuts  int
	putErr    error
	puts      int
	deleteErr error
}

func newFakeStore() *fakeStore { return &fakeStore{objects: map[string]fakeObject{}} }

func (s *fakeStore) PutFile(_ context.Context, key, path, checksum string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.puts++
	if s.failPuts > 0 {
		s.failPuts--
		return s.putErr
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	s.objects[key] = fakeObject{body: body, checksum: checksum}
	return nil
}

func (s *fakeStore) PutBytes(_ context.Context, key string, body []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.objects[key] = fakeObject{body: append([]byte(nil), body...)}
	return nil
}

func (s *fakeStore) Get(_ context.Context, key string) (io.ReadCloser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	object, ok := s.objects[key]
	if !ok {
		return nil, errors.New("no such key")
	}
	return io.NopCloser(bytes.NewReader(object.body)), nil
}

func (s *fakeStore) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.deleteErr != nil {
		return s.deleteErr
	}
	delete(s.objects, key)
	return nil
}

func (s *fakeStore) keys() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	keys := []string{}
	for key := range s.objects {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

type capturedNotice struct {
	userID, workspaceID uuid.UUID
	notifType, title    string
	teams               []uuid.UUID
}

type fakeNotifier struct {
	mu      sync.Mutex
	notices []capturedNotice
}

func (n *fakeNotifier) Create(_ context.Context, userID, workspaceID uuid.UUID, _ *uuid.UUID, notifType, title string, teams ...uuid.UUID) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.notices = append(n.notices, capturedNotice{userID, workspaceID, notifType, title, teams})
	return nil
}

// ---- harness ----

const testDatabasePassword = "db-pass-Zq81x"

type backupHarness struct {
	svc      *BackupService
	repo     *fakeBackupRepo
	store    *fakeStore
	notifier *fakeNotifier
	logs     *logtest.Hook
	sleeps   []time.Duration
	dir      string
	uploads  string
	key      []byte
	admins   []uuid.UUID
	clock    time.Time
	clockMu  sync.Mutex
}

func validDump() []byte { return append([]byte("PGDMP"), bytes.Repeat([]byte("data"), 5000)...) }

func newBackupHarness(t *testing.T) *backupHarness {
	t.Helper()
	root := t.TempDir()
	h := &backupHarness{
		repo:     newFakeBackupRepo(),
		store:    newFakeStore(),
		notifier: &fakeNotifier{},
		dir:      filepath.Join(root, "backups"),
		uploads:  filepath.Join(root, "uploads"),
		key:      crypto.DeriveKey("test-jwt-secret:backup"),
		admins:   []uuid.UUID{uuid.New(), uuid.New()},
		clock:    time.Date(2026, 11, 15, 3, 0, 30, 0, time.UTC),
	}
	h.repo.now = h.now
	for _, admin := range h.admins {
		h.repo.targets[admin] = [2]uuid.UUID{uuid.New(), uuid.New()}
	}
	require.NoError(t, os.MkdirAll(filepath.Join(h.uploads, "workspace", "a"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(h.uploads, "workspace", "a", "logo.png"), []byte("png-bytes"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(h.uploads, "avatar.jpg"), []byte("jpg-bytes"), 0o644))
	require.NoError(t, os.Symlink("/etc/passwd", filepath.Join(h.uploads, "escape")))

	h.svc = NewBackupService(h.repo, h.notifier, BackupServiceConfig{
		Dir:         h.dir,
		Timeout:     time.Minute,
		DatabaseURL: "postgres://kuayle:" + testDatabasePassword + "@postgres:5432/kuayle?sslmode=disable",
		SecretKey:   h.key,
		Sysadmins:   h.admins,
		AppVersion:  "test",
	}, BackupUploadsFromDir(h.uploads))
	h.svc.dumper = fakeDumper{body: validDump()}
	h.svc.openS3 = func(BackupS3Destination) (backupObjectStore, error) { return h.store, nil }
	h.svc.now = h.now
	h.svc.sleep = func(_ context.Context, d time.Duration) error {
		h.sleeps = append(h.sleeps, d)
		return nil
	}
	logger, hook := logtest.NewNullLogger()
	logger.SetLevel(log.DebugLevel)
	h.svc.logger = logger
	h.logs = hook
	return h
}

func (h *backupHarness) now() time.Time {
	h.clockMu.Lock()
	defer h.clockMu.Unlock()
	return h.clock
}

func (h *backupHarness) advance(d time.Duration) {
	h.clockMu.Lock()
	defer h.clockMu.Unlock()
	h.clock = h.clock.Add(d)
}

func (h *backupHarness) encrypt(t *testing.T, value string) string {
	encrypted, err := crypto.Encrypt(value, h.key)
	require.NoError(t, err)
	return encrypted
}

// enableSchedule makes a scheduled run due now.
func (h *backupHarness) enableSchedule() {
	due := h.now().Add(-30 * time.Second)
	h.repo.settings.Enabled = true
	h.repo.settings.NextRunAt = &due
}

func (h *backupHarness) enableS3(t *testing.T, access, secret string) {
	h.repo.settings.S3Enabled = true
	h.repo.settings.S3Bucket = "backups"
	h.repo.settings.S3Region = "us-east-1"
	h.repo.settings.S3Prefix = "kuayle"
	h.repo.settings.S3AccessKeyEncrypted = h.encrypt(t, access)
	h.repo.settings.S3SecretKeyEncrypted = h.encrypt(t, secret)
}

func (h *backupHarness) allLogText() string {
	var b strings.Builder
	for _, entry := range h.logs.AllEntries() {
		line, _ := entry.String()
		b.WriteString(line)
	}
	return b.String()
}

// ---- scheduling, lease and notifications ----

func TestBackupScheduledRunWritesVerifiableArchive(t *testing.T) {
	h := newBackupHarness(t)
	h.enableSchedule()

	require.True(t, h.svc.runDue(context.Background()))
	run := h.repo.onlyRun(t)
	require.Equal(t, domain.BackupStatusSucceeded, run.Status, "error: %v", run.Error)
	require.Equal(t, domain.BackupKindScheduled, run.Kind)
	require.Nil(t, run.Error)
	require.NotNil(t, run.ChecksumSHA256)
	require.FileExists(t, run.LocalPath)
	require.True(t, strings.HasPrefix(run.LocalPath, h.dir))
	require.Equal(t, uuid.Nil, h.repo.leaseToken, "the lease is released")
	require.True(t, h.repo.settings.NextRunAt.After(h.now()), "the next slot is scheduled")

	info, err := os.Stat(run.LocalPath)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	require.Equal(t, *run.SizeBytes, info.Size())
	sidecar, err := os.ReadFile(run.LocalPath + ".sha256")
	require.NoError(t, err)
	require.Equal(t, *run.ChecksumSHA256+"  "+filepath.Base(run.LocalPath)+"\n", string(sidecar))
	partials, _ := filepath.Glob(filepath.Join(h.dir, "*.partial"))
	require.Empty(t, partials)

	verified, err := h.svc.Verify(context.Background(), run.ID)
	require.NoError(t, err)
	require.Nil(t, verified.VerifyError)
	require.NotNil(t, verified.VerifiedAt)

	entries := readArchiveEntries(t, run.LocalPath, "")
	require.Contains(t, entries, "manifest.json")
	require.Contains(t, entries, "database.dump")
	require.Contains(t, entries, "uploads/avatar.jpg")
	require.Contains(t, entries, "uploads/workspace/a/logo.png")
	require.NotContains(t, entries, "uploads/escape", "symlinks are skipped")
	var manifest domain.BackupManifest
	require.NoError(t, json.Unmarshal(entries["manifest.json"], &manifest))
	require.Equal(t, 2, manifest.Uploads.Count)
	require.Equal(t, domain.BackupUploadsModeFiles, manifest.Uploads.Mode)
	require.Equal(t, int64(len(validDump())), manifest.Database.Size)
}

func TestBackupNotDueDoesNotRun(t *testing.T) {
	h := newBackupHarness(t)
	h.enableSchedule()
	later := h.now().Add(time.Hour)
	h.repo.settings.NextRunAt = &later
	require.False(t, h.svc.runDue(context.Background()))
	h.repo.settings.Enabled = false
	h.repo.settings.NextRunAt = nil
	require.False(t, h.svc.runDue(context.Background()))
	require.Empty(t, h.repo.runs)
}

func TestBackupScheduledFailureNotifiesSysadmins(t *testing.T) {
	h := newBackupHarness(t)
	h.enableSchedule()
	h.svc.dumper = fakeDumper{err: errors.New("connection refused")}

	h.svc.runDue(context.Background())
	run := h.repo.onlyRun(t)
	require.Equal(t, domain.BackupStatusFailed, run.Status)
	require.Contains(t, *run.Error, "connection refused")
	require.NotNil(t, run.FinishedAt)
	require.Equal(t, uuid.Nil, h.repo.leaseToken)
	files, _ := os.ReadDir(h.dir)
	require.Empty(t, files, "a failed run leaves no file behind")

	require.Len(t, h.notifier.notices, len(h.admins))
	for i, notice := range h.notifier.notices {
		target := h.repo.targets[h.admins[i]]
		require.Equal(t, h.admins[i], notice.userID)
		require.Equal(t, target[0], notice.workspaceID)
		require.Equal(t, []uuid.UUID{target[1]}, notice.teams)
		require.Equal(t, "backup_failed", notice.notifType)
		require.Contains(t, notice.title, "connection refused")
	}
}

func TestBackupManualFailureDoesNotNotify(t *testing.T) {
	h := newBackupHarness(t)
	h.svc.dumper = fakeDumper{err: errors.New("disk full")}
	user := h.admins[0]

	run, err := h.svc.RunNow(context.Background(), user)
	require.NoError(t, err)
	h.svc.Wait()
	stored, err := h.repo.GetRun(context.Background(), run.ID)
	require.NoError(t, err)
	require.Equal(t, domain.BackupStatusFailed, stored.Status)
	require.Equal(t, domain.BackupKindManual, stored.Kind)
	require.Equal(t, &user, stored.CreatedBy)
	require.Empty(t, h.notifier.notices)
}

func TestBackupLeaseConflictStartsNoSecondRun(t *testing.T) {
	h := newBackupHarness(t)
	other := uuid.New()
	won, err := h.repo.AcquireLease(context.Background(), other, time.Minute)
	require.NoError(t, err)
	require.True(t, won)

	_, err = h.svc.RunNow(context.Background(), h.admins[0])
	require.ErrorIs(t, err, ErrBackupRunning)

	h.enableSchedule()
	require.True(t, h.svc.runDue(context.Background()), "the slot is claimed so no replica runs it twice")
	require.Empty(t, h.repo.runs, "no run starts while another holds the lease")
	require.Equal(t, other, h.repo.leaseToken, "the other holder keeps the lease")

	h.advance(2 * time.Minute)
	run, err := h.svc.RunNow(context.Background(), h.admins[0])
	require.NoError(t, err, "an expired lease is taken over")
	h.svc.Wait()
	stored, _ := h.repo.GetRun(context.Background(), run.ID)
	require.Equal(t, domain.BackupStatusSucceeded, stored.Status)
}

// ---- S3 upload ----

func TestBackupUploadRetriesWithBackoff(t *testing.T) {
	h := newBackupHarness(t)
	h.enableSchedule()
	h.enableS3(t, "AKIAEXAMPLE", "s3-secret-value")
	h.store.failPuts = 2
	h.store.putErr = errors.New("503 slow down")

	h.svc.runDue(context.Background())
	run := h.repo.onlyRun(t)
	require.Equal(t, domain.BackupStatusSucceeded, run.Status)
	require.Equal(t, domain.BackupUploadUploaded, run.UploadStatus)
	require.Nil(t, run.Error)
	require.Equal(t, 3, h.store.puts)
	require.Len(t, h.sleeps, 2)
	require.GreaterOrEqual(t, h.sleeps[0], time.Second)
	require.Less(t, h.sleeps[0], 2*time.Second)
	require.GreaterOrEqual(t, h.sleeps[1], 2*time.Second)
	require.Less(t, h.sleeps[1], 3*time.Second)

	name := filepath.Base(run.LocalPath)
	require.Equal(t, "kuayle/"+name, run.S3Key)
	require.Equal(t, []string{"kuayle/" + name, "kuayle/" + name + ".sha256"}, h.store.keys())
	require.Equal(t, *run.ChecksumSHA256, h.store.objects[run.S3Key].checksum)
	require.Contains(t, run.Destination, "s3://backups/kuayle")
	require.Empty(t, h.notifier.notices)

	// With the local file gone, Verify reads the bucket copy.
	require.NoError(t, os.Remove(run.LocalPath))
	verified, err := h.svc.Verify(context.Background(), run.ID)
	require.NoError(t, err)
	require.Nil(t, verified.VerifyError)
}

func TestBackupUploadGivesUpAndNotifies(t *testing.T) {
	h := newBackupHarness(t)
	h.enableSchedule()
	h.enableS3(t, "AKIAEXAMPLE", "s3-secret-value")
	h.store.failPuts = 100
	h.store.putErr = errors.New("403 forbidden")

	h.svc.runDue(context.Background())
	run := h.repo.onlyRun(t)
	require.Equal(t, 5, h.store.puts, "one try and four retries")
	require.Len(t, h.sleeps, 4)
	require.Equal(t, domain.BackupStatusSucceeded, run.Status, "the local backup is still good")
	require.Equal(t, domain.BackupUploadFailed, run.UploadStatus)
	require.Contains(t, *run.Error, "upload failed")
	require.FileExists(t, run.LocalPath)
	require.Len(t, h.notifier.notices, len(h.admins))
}

// ---- retention ----

func TestBackupRetentionAfterSuccessKeepsUploadingBackups(t *testing.T) {
	h := newBackupHarness(t)
	h.enableSchedule()
	h.repo.settings.BackupRetention = domain.BackupRetention{Kind: "count", Value: 1}
	require.NoError(t, os.MkdirAll(h.dir, 0o700))

	old := func(age time.Duration, upload string) *domain.BackupRun {
		path := filepath.Join(h.dir, uuid.NewString()+".tar.gz")
		require.NoError(t, os.WriteFile(path, []byte("old"), 0o600))
		require.NoError(t, os.WriteFile(path+".sha256", []byte("x"), 0o600))
		run := &domain.BackupRun{ID: uuid.New(), Kind: domain.BackupKindScheduled, Status: domain.BackupStatusSucceeded,
			UploadStatus: upload, StartedAt: h.now().Add(-age), LocalPath: path}
		h.repo.runs[run.ID] = run
		return run
	}
	expired := old(48*time.Hour, domain.BackupUploadNone)
	uploading := old(72*time.Hour, domain.BackupUploadUploading)

	h.svc.runDue(context.Background())
	require.NoFileExists(t, expired.LocalPath)
	require.NoFileExists(t, expired.LocalPath+".sha256")
	require.NotNil(t, h.repo.runs[expired.ID].DeletedAt)
	require.FileExists(t, uploading.LocalPath, "a backup still uploading is kept")
	require.Nil(t, h.repo.runs[uploading.ID].DeletedAt)

	var newest *domain.BackupRun
	for _, run := range h.repo.runs {
		if run.ID != expired.ID && run.ID != uploading.ID {
			newest = run
		}
	}
	require.NotNil(t, newest)
	require.FileExists(t, newest.LocalPath, "the new backup is kept")
}

func TestBackupRetentionDeletesBucketCopiesAndKeepsRowsWhenDeleteFails(t *testing.T) {
	h := newBackupHarness(t)
	h.enableSchedule()
	h.enableS3(t, "AKIAEXAMPLE", "s3-secret-value")
	h.repo.settings.BackupRetention = domain.BackupRetention{Kind: "days", Value: 1}
	require.NoError(t, os.MkdirAll(h.dir, 0o700))
	path := filepath.Join(h.dir, "old.tar.gz")
	require.NoError(t, os.WriteFile(path, []byte("old"), 0o600))
	h.store.objects["kuayle/old.tar.gz"] = fakeObject{body: []byte("old")}
	h.store.objects["kuayle/old.tar.gz.sha256"] = fakeObject{body: []byte("x")}
	stale := &domain.BackupRun{ID: uuid.New(), Kind: domain.BackupKindManual, Status: domain.BackupStatusSucceeded,
		UploadStatus: domain.BackupUploadUploaded, StartedAt: h.now().Add(-72 * time.Hour), LocalPath: path, S3Key: "kuayle/old.tar.gz"}
	h.repo.runs[stale.ID] = stale

	h.store.deleteErr = errors.New("access denied")
	h.svc.runDue(context.Background())
	require.Nil(t, h.repo.runs[stale.ID].DeletedAt, "a backup whose copy could not be deleted stays listed")
	require.FileExists(t, path)

	h.store.deleteErr = nil
	h.advance(24 * time.Hour)
	h.enableSchedule()
	h.svc.runDue(context.Background())
	require.NotNil(t, h.repo.runs[stale.ID].DeletedAt)
	require.NoFileExists(t, path)
	require.NotContains(t, h.store.keys(), "kuayle/old.tar.gz")
	require.NotContains(t, h.store.keys(), "kuayle/old.tar.gz.sha256")
}

// ---- verify and encryption ----

func TestBackupVerifyDetectsAFlippedByte(t *testing.T) {
	h := newBackupHarness(t)
	run, err := h.svc.RunNow(context.Background(), h.admins[0])
	require.NoError(t, err)
	h.svc.Wait()
	stored, _ := h.repo.GetRun(context.Background(), run.ID)

	data, err := os.ReadFile(stored.LocalPath)
	require.NoError(t, err)
	data[len(data)/2] ^= 0x01
	require.NoError(t, os.WriteFile(stored.LocalPath, data, 0o600))

	verified, err := h.svc.Verify(context.Background(), run.ID)
	require.NoError(t, err)
	require.NotNil(t, verified.VerifyError)
	require.NotNil(t, verified.VerifiedAt)
}

func TestBackupVerifyRejectsNonPgDump(t *testing.T) {
	h := newBackupHarness(t)
	h.svc.dumper = fakeDumper{body: []byte("not a dump")}
	run, err := h.svc.RunNow(context.Background(), h.admins[0])
	require.NoError(t, err)
	h.svc.Wait()
	verified, err := h.svc.Verify(context.Background(), run.ID)
	require.NoError(t, err)
	require.NotNil(t, verified.VerifyError)
	require.Contains(t, *verified.VerifyError, "pg_dump")
}

func TestBackupVerifyRefusesUnverifiableRuns(t *testing.T) {
	h := newBackupHarness(t)
	sum := "abc"
	pre := &domain.BackupRun{ID: uuid.New(), Kind: domain.BackupKindPreUpgrade, Status: domain.BackupStatusSucceeded, ChecksumSHA256: &sum}
	h.repo.runs[pre.ID] = pre
	_, err := h.svc.Verify(context.Background(), pre.ID)
	var invalid *BackupValidationError
	require.ErrorAs(t, err, &invalid)
	require.Equal(t, PreUpgradeVerifyMessage, invalid.Message)

	_, err = h.svc.Verify(context.Background(), uuid.New())
	require.ErrorIs(t, err, ErrBackupRunNotFound)
}

func TestBackupEncryptedArchiveRoundTrip(t *testing.T) {
	h := newBackupHarness(t)
	h.repo.settings.EncryptionEnabled = true
	h.repo.settings.PassphraseEncrypted = h.encrypt(t, "correct horse battery staple")

	run, err := h.svc.RunNow(context.Background(), h.admins[0])
	require.NoError(t, err)
	h.svc.Wait()
	stored, _ := h.repo.GetRun(context.Background(), run.ID)
	require.Equal(t, domain.BackupStatusSucceeded, stored.Status, "error: %v", stored.Error)
	require.True(t, stored.Encrypted)
	require.True(t, strings.HasSuffix(stored.LocalPath, ".tar.gz.enc"))

	raw, err := os.ReadFile(stored.LocalPath)
	require.NoError(t, err)
	require.True(t, bytes.HasPrefix(raw, []byte("KUAYLEBK1")))
	require.NotContains(t, string(raw), "PGDMP", "the dump is not readable without the passphrase")

	verified, err := h.svc.Verify(context.Background(), run.ID)
	require.NoError(t, err)
	require.Nil(t, verified.VerifyError)

	entries := readArchiveEntries(t, stored.LocalPath, "correct horse battery staple")
	require.Equal(t, validDump(), entries["database.dump"])

	var out bytes.Buffer
	err = DecryptBackup(&out, bytes.NewReader(raw), "wrong passphrase!!")
	require.ErrorIs(t, err, ErrBackupDecrypt)

	h.repo.settings.PassphraseEncrypted = h.encrypt(t, "a different passphrase")
	verified, err = h.svc.Verify(context.Background(), run.ID)
	require.NoError(t, err)
	require.NotNil(t, verified.VerifyError)
}

func TestBackupStreamEncryptionDetectsTampering(t *testing.T) {
	for _, size := range []int{0, 1, backupCryptoChunkSize - 1, backupCryptoChunkSize, backupCryptoChunkSize + 1, 3*backupCryptoChunkSize + 17} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			plain := bytes.Repeat([]byte{7}, size)
			for i := range plain {
				plain[i] = byte(i * 31)
			}
			var sealed bytes.Buffer
			w, err := newBackupEncryptWriter(&sealed, "passphrase-123")
			require.NoError(t, err)
			_, err = w.Write(plain)
			require.NoError(t, err)
			require.NoError(t, w.Close())

			var out bytes.Buffer
			require.NoError(t, DecryptBackup(&out, bytes.NewReader(sealed.Bytes()), "passphrase-123"))
			require.Equal(t, plain, out.Bytes())

			chunk := backupCryptoChunkSize + 16
			if sealed.Len() > backupCryptoHeaderLen+chunk {
				truncated := sealed.Bytes()[:backupCryptoHeaderLen+chunk]
				require.ErrorIs(t, DecryptBackup(io.Discard, bytes.NewReader(truncated), "passphrase-123"), ErrBackupDecrypt, "dropping trailing chunks is detected")
			}
			flipped := append([]byte(nil), sealed.Bytes()...)
			flipped[len(flipped)-1] ^= 1
			require.ErrorIs(t, DecryptBackup(io.Discard, bytes.NewReader(flipped), "passphrase-123"), ErrBackupDecrypt)
		})
	}
}

// ---- settings and secrets ----

func TestBackupSettingsKeepAndClearSecrets(t *testing.T) {
	h := newBackupHarness(t)
	ctx := context.Background()
	admin := h.admins[0]
	input := BackupSettingsInput{
		Enabled:   true,
		Schedule:  domain.BackupSchedule{Kind: "weekly", Time: "04:15", Weekday: 2},
		Retention: domain.BackupRetention{Kind: "days", Value: 30},
		S3Enabled: true, S3Endpoint: "https://s3.example.com/", S3Bucket: "b", S3Region: "eu", S3Prefix: "/kuayle/",
		S3AccessKey: strPtr("AKIA-ACCESS"), S3SecretKey: strPtr("super-secret-key"),
		EncryptionEnabled: true, Passphrase: strPtr("long enough passphrase"),
	}
	saved, err := h.svc.UpdateSettings(ctx, admin, input)
	require.NoError(t, err)
	require.Equal(t, "https://s3.example.com", saved.S3Endpoint)
	require.Equal(t, "kuayle", saved.S3Prefix)
	require.NotContains(t, saved.S3SecretKeyEncrypted, "super-secret-key")
	require.Equal(t, time.Date(2026, 11, 17, 4, 15, 0, 0, time.UTC), *saved.NextRunAt)
	require.Equal(t, &admin, saved.UpdatedBy)

	input.S3AccessKey, input.S3SecretKey, input.Passphrase = nil, strPtr(""), nil
	saved, err = h.svc.UpdateSettings(ctx, admin, input)
	require.NoError(t, err)
	dest, err := h.svc.destination(saved)
	require.NoError(t, err)
	require.Equal(t, "AKIA-ACCESS", dest.AccessKey, "an empty field keeps the stored secret")
	require.Equal(t, "super-secret-key", dest.SecretKey)
	require.NotEmpty(t, saved.PassphraseEncrypted)

	input.S3Enabled, input.ClearS3Secrets = false, true
	input.EncryptionEnabled, input.ClearPassphrase = false, true
	input.Enabled = false
	saved, err = h.svc.UpdateSettings(ctx, admin, input)
	require.NoError(t, err)
	require.Empty(t, saved.S3AccessKeyEncrypted)
	require.Empty(t, saved.S3SecretKeyEncrypted)
	require.Empty(t, saved.PassphraseEncrypted)
	require.Nil(t, saved.NextRunAt, "a disabled schedule has no next run")
}

func TestBackupSettingsValidation(t *testing.T) {
	h := newBackupHarness(t)
	base := BackupSettingsInput{
		Schedule:  domain.BackupSchedule{Kind: "daily", Time: "03:00"},
		Retention: domain.BackupRetention{Kind: "count", Value: 7},
	}
	cases := map[string]func(*BackupSettingsInput){
		"bad time":       func(in *BackupSettingsInput) { in.Schedule.Time = "25:00" },
		"bad cron":       func(in *BackupSettingsInput) { in.Schedule = domain.BackupSchedule{Kind: "cron", Cron: "every day"} },
		"zero retention": func(in *BackupSettingsInput) { in.Retention.Value = 0 },
		"huge retention": func(in *BackupSettingsInput) { in.Retention.Value = 1001 },
		"s3 without bucket": func(in *BackupSettingsInput) {
			in.S3Enabled = true
			in.S3Region = "eu"
			in.S3AccessKey = strPtr("a")
			in.S3SecretKey = strPtr("b")
		},
		"s3 without keys": func(in *BackupSettingsInput) { in.S3Enabled = true; in.S3Bucket = "b"; in.S3Region = "eu" },
		"endpoint with credentials": func(in *BackupSettingsInput) {
			in.S3Enabled, in.S3Bucket, in.S3Region = true, "b", "eu"
			in.S3AccessKey, in.S3SecretKey = strPtr("a"), strPtr("b")
			in.S3Endpoint = "https://user:pass@s3.example.com"
		},
		"ftp endpoint":             func(in *BackupSettingsInput) { in.S3Endpoint = "ftp://example.com" },
		"encryption no passphrase": func(in *BackupSettingsInput) { in.EncryptionEnabled = true },
		"short passphrase":         func(in *BackupSettingsInput) { in.EncryptionEnabled = true; in.Passphrase = strPtr("short") },
	}
	t.Run("fields the schedule kind ignores keep valid stored values", func(t *testing.T) {
		input := base
		input.Schedule = domain.BackupSchedule{Kind: "cron", Cron: "0  4 * * *", Time: "bogus", Weekday: 42}
		saved, err := h.svc.UpdateSettings(context.Background(), h.admins[0], input)
		require.NoError(t, err)
		require.Equal(t, domain.BackupSchedule{Kind: "cron", Time: "03:00", Weekday: 0, Cron: "0 4 * * *"}, saved.BackupSchedule)
		input.Schedule = domain.BackupSchedule{Kind: "daily", Time: "05:00", Weekday: 42}
		saved, err = h.svc.UpdateSettings(context.Background(), h.admins[0], input)
		require.NoError(t, err)
		require.Equal(t, 0, saved.Weekday)
		require.Equal(t, "0 4 * * *", saved.Cron)
	})
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			input := base
			mutate(&input)
			_, err := h.svc.UpdateSettings(context.Background(), h.admins[0], input)
			var invalid *BackupValidationError
			require.ErrorAs(t, err, &invalid)
		})
	}
}

func TestBackupSecretsNeverReachErrorsOrLogs(t *testing.T) {
	h := newBackupHarness(t)
	h.enableSchedule()
	const access, secret, passphrase = "AKIA-LEAKY-ACCESS", "leaky/secret+key", "leaky passphrase value"
	h.enableS3(t, access, secret)
	h.repo.settings.EncryptionEnabled = true
	h.repo.settings.PassphraseEncrypted = h.encrypt(t, passphrase)
	h.svc.dumper = fakeDumper{err: fmt.Errorf("pg_dump: connection to postgres://kuayle:%s@db failed; key %s %s pass %s escaped %s",
		testDatabasePassword, access, secret, passphrase, "leaky%2Fsecret%2Bkey")}

	h.svc.runDue(context.Background())
	run := h.repo.onlyRun(t)
	require.Equal(t, domain.BackupStatusFailed, run.Status)

	h.repo.settings.NextRunAt = nil
	h.svc.dumper = fakeDumper{body: validDump()}
	h.store.failPuts = 100
	h.store.putErr = fmt.Errorf("signature for %s/%s rejected (pass %s)", access, secret, testDatabasePassword)
	_, err := h.svc.RunNow(context.Background(), h.admins[0])
	require.NoError(t, err)
	h.svc.Wait()

	h.svc.openS3 = func(dest BackupS3Destination) (backupObjectStore, error) {
		return nil, fmt.Errorf("dial https://%s:%s@s3.example.com failed", dest.AccessKey, dest.SecretKey)
	}
	testErr := h.svc.TestConnection(context.Background(), BackupS3Destination{Bucket: "b", Region: "eu"})
	require.Error(t, testErr)

	var stored strings.Builder
	for _, r := range h.repo.runs {
		if r.Error != nil {
			stored.WriteString(*r.Error)
		}
	}
	for _, notice := range h.notifier.notices {
		stored.WriteString(notice.title)
	}
	stored.WriteString(testErr.Error())
	stored.WriteString(h.allLogText())
	text := stored.String()
	require.Contains(t, text, "[redacted]")
	for _, leaked := range []string{access, secret, passphrase, testDatabasePassword, "leaky%2Fsecret%2Bkey"} {
		require.NotContains(t, text, leaked)
	}
}

func TestBackupTestConnectionUsesStoredSecrets(t *testing.T) {
	h := newBackupHarness(t)
	h.enableS3(t, "stored-access", "stored-secret")
	var used BackupS3Destination
	h.svc.openS3 = func(dest BackupS3Destination) (backupObjectStore, error) {
		used = dest
		return h.store, nil
	}
	require.NoError(t, h.svc.TestConnection(context.Background(), BackupS3Destination{Bucket: "unsaved", Region: "eu", Prefix: "/p/"}))
	require.Equal(t, "stored-access", used.AccessKey)
	require.Equal(t, "stored-secret", used.SecretKey)
	require.Equal(t, "unsaved", used.Bucket)
	require.Empty(t, h.store.keys(), "the write-test object is deleted")
}

func TestBackupUploadsFromObjectStorageAreListed(t *testing.T) {
	h := newBackupHarness(t)
	h.svc.uploads = BackupUploads{Lister: fakeLister{{Key: "a/1.png", Size: 10, ETag: "e1"}, {Key: "b/2.pdf", Size: 20}}}
	run, err := h.svc.RunNow(context.Background(), h.admins[0])
	require.NoError(t, err)
	h.svc.Wait()
	stored, _ := h.repo.GetRun(context.Background(), run.ID)
	entries := readArchiveEntries(t, stored.LocalPath, "")
	require.NotContains(t, entries, "uploads/avatar.jpg")
	var objects []domain.BackupUploadObject
	require.NoError(t, json.Unmarshal(entries["uploads-manifest.json"], &objects))
	require.Len(t, objects, 2)
	var manifest domain.BackupManifest
	require.NoError(t, json.Unmarshal(entries["manifest.json"], &manifest))
	require.Equal(t, domain.BackupUploadsModeS3Manifest, manifest.Uploads.Mode)
	require.EqualValues(t, 30, manifest.Uploads.Bytes)
	verified, err := h.svc.Verify(context.Background(), run.ID)
	require.NoError(t, err)
	require.Nil(t, verified.VerifyError)
}

type fakeLister []storage.Object

func (l fakeLister) List(context.Context, string) ([]storage.Object, error) { return l, nil }

func TestSplitDatabasePasswordKeepsItOutOfTheURL(t *testing.T) {
	conn, password, err := splitDatabasePassword("postgres://kuayle:p%40ss@db:5432/kuayle?sslmode=disable")
	require.NoError(t, err)
	require.Equal(t, "p@ss", password)
	require.Equal(t, "postgres://kuayle@db:5432/kuayle?sslmode=disable", conn)
	conn, password, err = splitDatabasePassword("postgresql://db/kuayle?user=k&password=q")
	require.NoError(t, err)
	require.Equal(t, "q", password)
	require.NotContains(t, conn, "password")
	_, _, err = splitDatabasePassword("host=db password=x")
	require.Error(t, err)
}

// readArchiveEntries decrypts (when passphrase is set) and extracts an
// archive into memory.
func readArchiveEntries(t *testing.T, path, passphrase string) map[string][]byte {
	t.Helper()
	file, err := os.Open(path)
	require.NoError(t, err)
	defer file.Close()
	var reader io.Reader = file
	if passphrase != "" {
		reader, err = newBackupDecryptReader(file, passphrase)
		require.NoError(t, err)
	}
	gz, err := gzip.NewReader(reader)
	require.NoError(t, err)
	tr := tar.NewReader(gz)
	entries := map[string][]byte{}
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		body, err := io.ReadAll(tr)
		require.NoError(t, err)
		entries[header.Name] = body
	}
	return entries
}
