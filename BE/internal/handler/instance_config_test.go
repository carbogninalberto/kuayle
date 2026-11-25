package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/config"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"
)

type memoryOverrides struct {
	mu   sync.Mutex
	rows map[string]domain.InstanceConfigOverride
}

func (m *memoryOverrides) ListOverrides(context.Context) ([]domain.InstanceConfigOverride, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rows := []domain.InstanceConfigOverride{}
	for _, row := range m.rows {
		rows = append(rows, row)
	}
	return rows, nil
}

func (m *memoryOverrides) SetOverride(_ context.Context, name, value string, actor uuid.UUID) (*domain.InstanceConfigOverride, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	row := domain.InstanceConfigOverride{Name: name, Value: value, UpdatedBy: &actor, UpdatedAt: time.Now()}
	m.rows[name] = row
	return &row, nil
}

func (m *memoryOverrides) DeleteOverride(_ context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.rows, name)
	return nil
}

// secretSentinels fills every secret the API reads with a value the tests
// look for in each response.
const secretSentinel = "SENTINELSECRETVALUE"

func newInstanceConfigHandlerForTest(t *testing.T, admin uuid.UUID) (*InstanceConfigHandler, *memoryOverrides) {
	t.Helper()
	cfg := &config.Config{
		JWTSecret: secretSentinel + "jwt", DatabaseURL: "postgres://kuayle:" + secretSentinel + "@db/kuayle",
		RedisURL: "redis://:" + secretSentinel + "@redis:6379", WorkflowPipelines: true, BackupTimeout: 2 * time.Hour,
		SystemUpdaterToken: secretSentinel + "updater", VoiceInternalToken: secretSentinel + "voice",
		SSOInstanceClientSecret: secretSentinel + "oidc", GitHubWebhookURL: "https://smee.io/" + secretSentinel,
		VoiceURL: "http://voice:" + secretSentinel + "@voice:8000",
	}
	cfg.SMTP.Password, cfg.SMTP.Username = secretSentinel+"smtp", secretSentinel+"user"
	cfg.Storage.S3SecretKey, cfg.Storage.S3AccessKey = secretSentinel+"s3", secretSentinel+"access"
	cfg.VAPID.PrivateKey = secretSentinel + "vapid"
	cfg.GitHubApp.PrivateKey, cfg.GitHubApp.ClientSecret, cfg.GitHubApp.WebhookSecret = secretSentinel+"pem", secretSentinel+"client", secretSentinel+"hook"
	cfg.Slack.ClientSecret, cfg.Slack.SigningSecret = secretSentinel+"slack", secretSentinel+"signing"
	cfg.DevMachine.EncryptionKey = secretSentinel + "machines"
	store := &memoryOverrides{rows: map[string]domain.InstanceConfigOverride{}}
	svc := service.NewInstanceConfigService(cfg, store)
	return NewInstanceConfigHandler(svc, func(id uuid.UUID) bool { return id == admin }), store
}

func callInstanceConfig(t *testing.T, handler echo.HandlerFunc, method, body string, user uuid.UUID, name string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, "/api/system/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := echo.New().NewContext(req, rec)
	c.Set("user_id", user)
	if name != "" {
		c.SetPathValues(echo.PathValues{{Name: "name", Value: name}})
	}
	require.NoError(t, handler(c))
	return rec
}

func TestInstanceConfigRequiresSysadmin(t *testing.T) {
	admin, member := uuid.New(), uuid.New()
	h, store := newInstanceConfigHandlerForTest(t, admin)
	for _, route := range []struct {
		name    string
		handler echo.HandlerFunc
		method  string
		body    string
	}{
		{"list", h.List, http.MethodGet, ""},
		{"update", h.Update, http.MethodPut, `{"value":"false"}`},
		{"reset", h.Reset, http.MethodDelete, ""},
	} {
		t.Run(route.name, func(t *testing.T) {
			rec := callInstanceConfig(t, route.handler, route.method, route.body, member, config.VarWorkflowPipelines)
			require.Equal(t, http.StatusForbidden, rec.Code)
			require.Equal(t, "private, no-store", rec.Header().Get("Cache-Control"))
			require.NotContains(t, rec.Body.String(), secretSentinel)
		})
	}
	require.Empty(t, store.rows, "a refused change stores nothing")
}

// No response, listing or change, carries a secret or a part of one.
func TestInstanceConfigResponsesNeverCarrySecrets(t *testing.T) {
	admin := uuid.New()
	h, _ := newInstanceConfigHandlerForTest(t, admin)
	list := callInstanceConfig(t, h.List, http.MethodGet, "", admin, "")
	require.Equal(t, http.StatusOK, list.Code)
	require.Equal(t, "private, no-store", list.Header().Get("Cache-Control"))
	update := callInstanceConfig(t, h.Update, http.MethodPut, `{"value":"false"}`, admin, config.VarWorkflowPipelines)
	require.Equal(t, http.StatusOK, update.Code, update.Body.String())
	reset := callInstanceConfig(t, h.Reset, http.MethodDelete, "", admin, config.VarWorkflowPipelines)
	require.Equal(t, http.StatusOK, reset.Code)
	for _, rec := range []*httptest.ResponseRecorder{list, update, reset} {
		require.NotContains(t, rec.Body.String(), secretSentinel)
	}

	var body struct {
		Groups    []string `json:"groups"`
		Variables []struct {
			Name     string `json:"name"`
			Secret   bool   `json:"secret"`
			Set      bool   `json:"set"`
			Value    string `json:"value"`
			Withheld bool   `json:"withheld"`
			Tunable  bool   `json:"tunable"`
			Source   string `json:"source"`
		} `json:"variables"`
	}
	require.NoError(t, json.Unmarshal(list.Body.Bytes(), &body))
	require.Equal(t, config.Groups(), body.Groups)
	require.Len(t, body.Variables, len(config.Variables()))
	byName := map[string]int{}
	for i, variable := range body.Variables {
		byName[variable.Name] = i
	}
	jwt := body.Variables[byName["JWT_SECRET"]]
	require.True(t, jwt.Secret)
	require.True(t, jwt.Set)
	require.Empty(t, jwt.Value)
	require.False(t, body.Variables[byName["VAPID_PRIVATE_KEY"]].Tunable)
	voice := body.Variables[byName["VOICE_URL"]]
	require.True(t, voice.Withheld, "a URL with credentials is withheld")
	pipelines := body.Variables[byName[config.VarWorkflowPipelines]]
	require.True(t, pipelines.Tunable)
	require.Equal(t, "true", pipelines.Value)
}

func TestInstanceConfigUpdateValidatesAndResets(t *testing.T) {
	admin := uuid.New()
	h, store := newInstanceConfigHandlerForTest(t, admin)

	for _, tc := range []struct {
		name, variable, body, code string
		status                     int
	}{
		{"not a bool", config.VarWorkflowPipelines, `{"value":"maybe"}`, "INVALID_CONFIG_VALUE", http.StatusBadRequest},
		{"below the range", config.VarBackupTimeout, `{"value":"1s"}`, "INVALID_CONFIG_VALUE", http.StatusBadRequest},
		{"not a duration", config.VarBackupTimeout, `{"value":"soon"}`, "INVALID_CONFIG_VALUE", http.StatusBadRequest},
		{"no value", config.VarBackupTimeout, `{}`, "INVALID_CONFIG_VALUE", http.StatusBadRequest},
		{"not json", config.VarBackupTimeout, `value=2h`, "INVALID_CONFIG_VALUE", http.StatusBadRequest},
		{"needs a restart", "JWT_SECRET", `{"value":"` + strings.Repeat("x", 40) + `"}`, "CONFIG_READ_ONLY", http.StatusConflict},
		{"unknown", "NOT_A_VARIABLE", `{"value":"true"}`, "NOT_FOUND", http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := callInstanceConfig(t, h.Update, http.MethodPut, tc.body, admin, tc.variable)
			require.Equal(t, tc.status, rec.Code, rec.Body.String())
			require.Contains(t, rec.Body.String(), tc.code)
		})
	}
	require.Empty(t, store.rows)

	rec := callInstanceConfig(t, h.Update, http.MethodPut, `{"value":"90m"}`, admin, config.VarBackupTimeout)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"value":"1h30m"`)
	require.Contains(t, rec.Body.String(), `"source":"override"`)
	require.Contains(t, rec.Body.String(), `"base_value":"2h"`)
	require.Equal(t, "1h30m", store.rows[config.VarBackupTimeout].Value)

	rec = callInstanceConfig(t, h.Reset, http.MethodDelete, "", admin, config.VarBackupTimeout)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"value":"2h"`)
	require.Contains(t, rec.Body.String(), `"source":"default"`)
	require.Empty(t, store.rows)

	rec = callInstanceConfig(t, h.Reset, http.MethodDelete, "", admin, "DATABASE_URL")
	require.Equal(t, http.StatusConflict, rec.Code)
}
