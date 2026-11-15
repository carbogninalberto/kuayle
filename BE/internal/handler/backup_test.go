package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/kuayle/kuayle-backend/pkg/crypto"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"
)

// settingsOnlyBackupRepo keeps the settings row in memory; the handler
// tests below do not start runs.
type settingsOnlyBackupRepo struct {
	mu       sync.Mutex
	settings domain.BackupSettings
	leases   int
}

func (r *settingsOnlyBackupRepo) GetSettings(context.Context) (*domain.BackupSettings, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	copy := r.settings
	return &copy, nil
}
func (r *settingsOnlyBackupRepo) UpdateSettings(_ context.Context, s *domain.BackupSettings) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.settings = *s
	return nil
}
func (r *settingsOnlyBackupRepo) ClaimSchedule(context.Context, *time.Time, time.Time) (bool, error) {
	return false, nil
}
func (r *settingsOnlyBackupRepo) AcquireLease(context.Context, uuid.UUID, time.Duration) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.leases++
	return false, nil
}
func (r *settingsOnlyBackupRepo) RenewLease(context.Context, uuid.UUID, time.Duration) (bool, error) {
	return false, nil
}
func (r *settingsOnlyBackupRepo) ReleaseLease(context.Context, uuid.UUID) error           { return nil }
func (r *settingsOnlyBackupRepo) CreateRun(context.Context, *domain.BackupRun) error      { return nil }
func (r *settingsOnlyBackupRepo) UpdateRun(context.Context, *domain.BackupRun) error      { return nil }
func (r *settingsOnlyBackupRepo) MarkDeleted(context.Context, uuid.UUID, time.Time) error { return nil }
func (r *settingsOnlyBackupRepo) ListRuns(context.Context, int, int) ([]domain.BackupRun, error) {
	return nil, nil
}
func (r *settingsOnlyBackupRepo) ListRetainable(context.Context) ([]domain.BackupRun, error) {
	return nil, nil
}
func (r *settingsOnlyBackupRepo) GetRun(context.Context, uuid.UUID) (*domain.BackupRun, error) {
	return nil, service.ErrBackupRunNotFound
}
func (r *settingsOnlyBackupRepo) MarkStaleRunning(context.Context, time.Time) (int64, error) {
	return 0, nil
}
func (r *settingsOnlyBackupRepo) NotificationTarget(context.Context, uuid.UUID) (uuid.UUID, uuid.UUID, bool, error) {
	return uuid.Nil, uuid.Nil, false, nil
}

func newBackupHandlerForTest(admin uuid.UUID) (*BackupHandler, *settingsOnlyBackupRepo) {
	repo := &settingsOnlyBackupRepo{settings: domain.BackupSettings{
		BackupSchedule:  domain.BackupSchedule{Kind: "daily", Time: "03:00"},
		BackupRetention: domain.BackupRetention{Kind: "count", Value: 7},
	}}
	svc := service.NewBackupService(repo, nil, service.BackupServiceConfig{
		Dir: "/app/backups", SecretKey: crypto.DeriveKey("handler-test:backup"),
		DatabaseURL: "postgres://u:p@db/kuayle",
	}, service.BackupUploadsFromDir(""))
	return NewBackupHandler(svc, func(id uuid.UUID) bool { return id == admin }, "/app/backups"), repo
}

func callBackup(t *testing.T, handler echo.HandlerFunc, method, path, body string, userID uuid.UUID, params ...string) *httptest.ResponseRecorder {
	t.Helper()
	e := echo.New()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", userID)
	if len(params) == 2 {
		c.SetPathValues(echo.PathValues{{Name: params[0], Value: params[1]}})
	}
	require.NoError(t, handler(c))
	return rec
}

func TestBackupRoutesRequireSysAdmin(t *testing.T) {
	admin := uuid.New()
	h, repo := newBackupHandlerForTest(admin)
	outsider := uuid.New()
	routes := []struct {
		name    string
		handler echo.HandlerFunc
		method  string
		body    string
	}{
		{"get settings", h.GetSettings, http.MethodGet, ""},
		{"update settings", h.UpdateSettings, http.MethodPut, `{"enabled":true,"schedule_kind":"daily","schedule_time":"01:00","retention_kind":"count","retention_value":3}`},
		{"test connection", h.TestConnection, http.MethodPost, `{"s3_bucket":"b","s3_region":"r","s3_access_key":"a","s3_secret_key":"s"}`},
		{"run now", h.RunNow, http.MethodPost, ""},
		{"list runs", h.ListRuns, http.MethodGet, ""},
		{"verify", h.Verify, http.MethodPost, ""},
	}
	for _, route := range routes {
		t.Run(route.name, func(t *testing.T) {
			rec := callBackup(t, route.handler, route.method, "/api/system/backups", route.body, outsider, "id", uuid.NewString())
			require.Equal(t, http.StatusForbidden, rec.Code)
		})
	}
	require.False(t, repo.settings.Enabled, "a refused update changes nothing")
	require.Zero(t, repo.leases, "a refused run does not take the lease")
}

func TestBackupSettingsNeverReturnSecrets(t *testing.T) {
	admin := uuid.New()
	h, repo := newBackupHandlerForTest(admin)
	const access, secret, passphrase = "AKIA-HANDLER-ACCESS", "handler-secret-key", "handler passphrase 123"
	body := `{"enabled":true,"schedule_kind":"weekly","schedule_time":"02:30","schedule_weekday":3,
		"retention_kind":"days","retention_value":14,"s3_enabled":true,"s3_endpoint":"https://s3.example.com",
		"s3_bucket":"bucket","s3_region":"eu-west-1","s3_prefix":"kuayle","s3_access_key":"` + access + `",
		"s3_secret_key":"` + secret + `","encryption_enabled":true,"passphrase":"` + passphrase + `"}`

	put := callBackup(t, h.UpdateSettings, http.MethodPut, "/api/system/backups/settings", body, admin)
	require.Equal(t, http.StatusOK, put.Code, put.Body.String())
	get := callBackup(t, h.GetSettings, http.MethodGet, "/api/system/backups/settings", "", admin)
	require.Equal(t, http.StatusOK, get.Code)

	for _, rec := range []*httptest.ResponseRecorder{put, get} {
		text := rec.Body.String()
		for _, leaked := range []string{access, secret, passphrase, repo.settings.S3SecretKeyEncrypted, repo.settings.PassphraseEncrypted} {
			require.NotContains(t, text, leaked)
		}
		require.Contains(t, text, `"has_secret_key":true`)
		require.Contains(t, text, `"has_access_key":true`)
		require.Contains(t, text, `"has_passphrase":true`)
		require.Contains(t, text, `"schedule_kind":"weekly"`)
	}

	invalid := callBackup(t, h.UpdateSettings, http.MethodPut, "/api/system/backups/settings",
		`{"schedule_kind":"daily","schedule_time":"99:00","retention_kind":"count","retention_value":3}`, admin)
	require.Equal(t, http.StatusBadRequest, invalid.Code)
	require.Contains(t, invalid.Body.String(), "INVALID_BACKUP")
}

func TestBackupRunNowConflictIs409(t *testing.T) {
	admin := uuid.New()
	h, _ := newBackupHandlerForTest(admin)
	rec := callBackup(t, h.RunNow, http.MethodPost, "/api/system/backups/run", "", admin)
	require.Equal(t, http.StatusConflict, rec.Code)
	require.Contains(t, rec.Body.String(), "BACKUP_RUNNING")
}
