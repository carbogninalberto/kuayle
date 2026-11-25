package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/config"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/instanceconfigtest"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/labstack/echo/v5"
	"github.com/sirupsen/logrus"
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

// newRestartHandlerForTest is a handler whose service can edit restart-only
// settings; the restart itself is counted instead of stopping the test.
func newRestartHandlerForTest(t *testing.T, admin uuid.UUID, supervised bool) (*InstanceConfigHandler, *instanceconfigtest.RestartStore, *atomic.Int32) {
	t.Helper()
	cfg := &config.Config{
		JWTSecret: secretSentinel + "jwt0123456789abcdef0123456789", IssueDeleteGracePeriod: 720 * time.Hour, PasskeysEnabled: true,
		PasskeyRPDisplayName: "Kuayle", SSOInstanceDefaultRole: "member", WorkflowPipelines: true, BackupTimeout: 2 * time.Hour,
		DatabaseURL: "postgres://kuayle:" + secretSentinel + "@db/kuayle",
	}
	cfg.SMTP.Password = secretSentinel + "smtp"
	store := instanceconfigtest.NewRestartStore()
	startup := service.ResolveStartup(context.Background(), store, cfg, true, time.Now, logrus.New())
	svc := service.NewInstanceConfigService(startup.Config, &memoryOverrides{rows: map[string]domain.InstanceConfigOverride{}})
	var exits atomic.Int32
	svc.SetRestart(service.NewInstanceRestart(store, startup, supervised, func() { exits.Add(1) }))
	return NewInstanceConfigHandler(svc, func(id uuid.UUID) bool { return id == admin }), store, &exits
}

func TestRestartEndpointsRequireSysadmin(t *testing.T) {
	admin, member := uuid.New(), uuid.New()
	h, store, exits := newRestartHandlerForTest(t, admin, true)
	body := `{"changes":{"PASSKEYS_ENABLED":"false"}}`
	for name, handler := range map[string]echo.HandlerFunc{"validate": h.ValidateRestart, "apply": h.ApplyRestart} {
		t.Run(name, func(t *testing.T) {
			rec := callInstanceConfig(t, handler, http.MethodPost, body, member, "")
			require.Equal(t, http.StatusForbidden, rec.Code)
			require.Equal(t, "private, no-store", rec.Header().Get("Cache-Control"))
			require.NotContains(t, rec.Body.String(), secretSentinel)
		})
	}
	require.Empty(t, store.Overrides, "a refused request stores nothing")
	require.Empty(t, store.Runs)
	time.Sleep(20 * time.Millisecond)
	require.Zero(t, exits.Load(), "a refused request does not stop the API")
}

func TestValidateRestartReportsErrorsPerVariableAndStoresNothing(t *testing.T) {
	admin := uuid.New()
	h, store, _ := newRestartHandlerForTest(t, admin, true)
	rec := callInstanceConfig(t, h.ValidateRestart, http.MethodPost,
		`{"changes":{"SMTP_PORT":"99999","SSO_OIDC_DEFAULT_ROLE":"owner","JWT_SECRET":"x","OIDC_ALLOW_PRIVATE_NETWORK":"true","PASSKEYS_ENABLED":"false"}}`, admin, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var plan struct {
		Valid   bool `json:"valid"`
		Changes []struct{ Name, From, To string }
		Issues  []struct{ Name, Message string }
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &plan))
	require.False(t, plan.Valid)
	byName := map[string]string{}
	for _, issue := range plan.Issues {
		byName[issue.Name] = issue.Message
	}
	require.Len(t, byName, 4)
	require.Contains(t, byName["SMTP_PORT"], "between 1 and 65535")
	require.Contains(t, byName["SSO_OIDC_DEFAULT_ROLE"], "member, guest")
	require.Contains(t, byName["JWT_SECRET"], "cannot be changed")
	require.Contains(t, byName["OIDC_ALLOW_PRIVATE_NETWORK"], "cannot be changed")
	require.NotContains(t, rec.Body.String(), secretSentinel)
	require.Empty(t, store.Overrides)
	require.Empty(t, store.Runs)

	ok := callInstanceConfig(t, h.ValidateRestart, http.MethodPost, `{"changes":{"PASSKEYS_ENABLED":"false"}}`, admin, "")
	require.Equal(t, http.StatusOK, ok.Code)
	require.JSONEq(t, `{"valid":true,"changes":[{"name":"PASSKEYS_ENABLED","from":"true","to":"false"}],"issues":[]}`, ok.Body.String())
	require.Empty(t, store.Overrides, "a valid dry run still stores nothing")

	empty := callInstanceConfig(t, h.ValidateRestart, http.MethodPost, `{"changes":{}}`, admin, "")
	require.Equal(t, http.StatusBadRequest, empty.Code)
}

func TestApplyRestartRecordsTheRunAndStopsTheAPI(t *testing.T) {
	admin := uuid.New()
	h, store, exits := newRestartHandlerForTest(t, admin, true)
	rec := callInstanceConfig(t, h.ApplyRestart, http.MethodPost, `{"changes":{"SMTP_PORT":"2525","PASSKEY_RP_DISPLAY_NAME":"Acme"}}`, admin, "")
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	require.NotContains(t, rec.Body.String(), secretSentinel)
	var run struct {
		ID          uuid.UUID `json:"id"`
		Status      string    `json:"status"`
		RequestedBy uuid.UUID `json:"requested_by"`
		Changes     []struct{ Name, From, To string }
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &run))
	require.Equal(t, "restarting", run.Status)
	require.Equal(t, admin, run.RequestedBy)
	require.Len(t, run.Changes, 2)
	require.Equal(t, map[string]string{"SMTP_PORT": "2525", "PASSKEY_RP_DISPLAY_NAME": "Acme"}, store.Overrides)

	// Apply again while the run waits for its outcome.
	again := callInstanceConfig(t, h.ApplyRestart, http.MethodPost, `{"changes":{"PASSKEYS_ENABLED":"false"}}`, admin, "")
	require.Equal(t, http.StatusConflict, again.Code)
	require.Contains(t, again.Body.String(), "RESTART_IN_PROGRESS")
	require.Len(t, store.Runs, 1)
	require.Eventually(t, func() bool { return exits.Load() == 1 }, 3*time.Second, 10*time.Millisecond)

	list := callInstanceConfig(t, h.List, http.MethodGet, "", admin, "")
	require.Equal(t, http.StatusOK, list.Code)
	require.NotContains(t, list.Body.String(), secretSentinel)
	var body struct {
		Restart struct {
			Supported bool `json:"supported"`
			Run       struct {
				ID     uuid.UUID `json:"id"`
				Status string    `json:"status"`
			} `json:"run"`
		} `json:"restart"`
		Variables []struct {
			Name            string `json:"name"`
			RestartEditable bool   `json:"restart_editable"`
			Tunable         bool   `json:"tunable"`
			Rule            *struct {
				Min string `json:"min"`
				Max string `json:"max"`
			} `json:"rule"`
		} `json:"variables"`
	}
	require.NoError(t, json.Unmarshal(list.Body.Bytes(), &body))
	require.True(t, body.Restart.Supported)
	require.Equal(t, run.ID, body.Restart.Run.ID)
	require.Equal(t, "restarting", body.Restart.Run.Status)
	editable := []string{}
	for _, variable := range body.Variables {
		if variable.RestartEditable {
			editable = append(editable, variable.Name)
			require.False(t, variable.Tunable, variable.Name)
		}
		switch variable.Name {
		case "SMTP_PORT":
			require.Equal(t, "1", variable.Rule.Min)
			require.Equal(t, "65535", variable.Rule.Max)
		case "JWT_SECRET", "DATABASE_URL", "SYSADMINS", "OIDC_ALLOW_PRIVATE_NETWORK", "FRONTEND_URL":
			require.False(t, variable.RestartEditable, variable.Name)
		}
	}
	require.ElementsMatch(t, config.RestartEditableNames(), editable)
}

func TestApplyRestartRefusesInvalidValuesAndAnUnsupervisedAPI(t *testing.T) {
	admin := uuid.New()
	h, store, exits := newRestartHandlerForTest(t, admin, true)
	invalid := callInstanceConfig(t, h.ApplyRestart, http.MethodPost, `{"changes":{"SMTP_PORT":"0","JWT_SECRET":"x"}}`, admin, "")
	require.Equal(t, http.StatusBadRequest, invalid.Code)
	var failure struct {
		Error struct {
			Code    string `json:"code"`
			Details []struct{ Field, Message string }
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(invalid.Body.Bytes(), &failure))
	require.Equal(t, "INVALID_CONFIG_VALUE", failure.Error.Code)
	require.Len(t, failure.Error.Details, 2)

	unchanged := callInstanceConfig(t, h.ApplyRestart, http.MethodPost, `{"changes":{"PASSKEYS_ENABLED":"true"}}`, admin, "")
	require.Equal(t, http.StatusBadRequest, unchanged.Code)
	require.Contains(t, unchanged.Body.String(), "NO_CHANGES")

	unsupervised, unsupervisedStore, unsupervisedExits := newRestartHandlerForTest(t, admin, false)
	rec := callInstanceConfig(t, unsupervised.ApplyRestart, http.MethodPost, `{"changes":{"PASSKEYS_ENABLED":"false"}}`, admin, "")
	require.Equal(t, http.StatusConflict, rec.Code)
	require.Contains(t, rec.Body.String(), "RESTART_UNSUPPORTED")
	dry := callInstanceConfig(t, unsupervised.ValidateRestart, http.MethodPost, `{"changes":{"PASSKEYS_ENABLED":"false"}}`, admin, "")
	require.Equal(t, http.StatusOK, dry.Code, "the dry run needs no supervisor")

	for _, s := range []*instanceconfigtest.RestartStore{store, unsupervisedStore} {
		require.Empty(t, s.Overrides)
		require.Empty(t, s.Runs)
	}
	time.Sleep(20 * time.Millisecond)
	require.Zero(t, exits.Load()+unsupervisedExits.Load())
}

// The runtime endpoints still refuse a restart-only variable: it changes only
// through the dry run and the apply.
func TestRuntimeEndpointsStillRefuseRestartOnlyVariables(t *testing.T) {
	admin := uuid.New()
	h, store, _ := newRestartHandlerForTest(t, admin, true)
	rec := callInstanceConfig(t, h.Update, http.MethodPut, `{"value":"false"}`, admin, "PASSKEYS_ENABLED")
	require.Equal(t, http.StatusConflict, rec.Code)
	require.Empty(t, store.Overrides)
}
