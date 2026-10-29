package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/agent"
	"github.com/kuayle/kuayle-backend/internal/config"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/internal/service"
	cryptoutil "github.com/kuayle/kuayle-backend/pkg/crypto"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
)

func TestMachineErrorMapsCheckoutEligibilityConflict(t *testing.T) {
	e := echo.New()
	recorder := httptest.NewRecorder()
	ctx := e.NewContext(httptest.NewRequest(http.MethodPost, "/checkouts", nil), recorder)

	err := machineError(ctx, fmt.Errorf("%w: issue has no development repository", service.ErrCheckoutNotEligible))

	require.NoError(t, err)
	require.Equal(t, http.StatusConflict, recorder.Code)
	var response dto.ErrorResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, "CHECKOUT_NOT_ELIGIBLE", response.Error.Code)
	require.Contains(t, response.Error.Message, "no development repository")
}

func TestMachineErrorMapsCheckoutReadinessConflict(t *testing.T) {
	e := echo.New()
	recorder := httptest.NewRecorder()
	ctx := e.NewContext(httptest.NewRequest(http.MethodPost, "/agent-runs", nil), recorder)

	err := machineError(ctx, fmt.Errorf("%w: checkout preparation is still in progress", service.ErrCheckoutNotReady))

	require.NoError(t, err)
	require.Equal(t, http.StatusConflict, recorder.Code)
	var response dto.ErrorResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, "CHECKOUT_NOT_READY", response.Error.Code)
	require.Contains(t, response.Error.Message, "in progress")
}

func TestMachineErrorMapsNativeTerminalRequirement(t *testing.T) {
	e := echo.New()
	recorder := httptest.NewRecorder()
	ctx := e.NewContext(httptest.NewRequest(http.MethodPost, "/services/terminal/launch", nil), recorder)

	err := machineError(ctx, service.ErrTerminalSessionRequired)

	require.NoError(t, err)
	require.Equal(t, http.StatusConflict, recorder.Code)
	var response dto.ErrorResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, "TERMINAL_SESSION_REQUIRED", response.Error.Code)
}

func TestMachineErrorMapsEnvironmentDeletionStates(t *testing.T) {
	for _, test := range []struct {
		name            string
		err             error
		status          int
		code            string
		messageContains string
	}{
		{name: "missing", err: service.ErrEnvironmentNotFound, status: http.StatusNotFound, code: "NOT_FOUND", messageContains: "Development Environment"},
		{name: "in use", err: service.ErrEnvironmentInUse, status: http.StatusConflict, code: "ENVIRONMENT_IN_USE"},
		{name: "invalid lifecycle state", err: service.ErrEnvironmentInvalidState, status: http.StatusConflict, code: "ENVIRONMENT_INVALID_STATE"},
		{name: "active cleanup work", err: service.ErrEnvironmentCleanupActive, status: http.StatusConflict, code: "ENVIRONMENT_CLEANUP_ACTIVE"},
		{name: "invalid request", err: fmt.Errorf("%w: invalid environment id", service.ErrInvalidMachineInput), status: http.StatusBadRequest, code: "BAD_REQUEST"},
	} {
		t.Run(test.name, func(t *testing.T) {
			e := echo.New()
			recorder := httptest.NewRecorder()
			ctx := e.NewContext(httptest.NewRequest(http.MethodDelete, "/environments/test", nil), recorder)

			require.NoError(t, machineError(ctx, test.err))
			require.Equal(t, test.status, recorder.Code)
			var response dto.ErrorResponse
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
			require.Equal(t, test.code, response.Error.Code)
			if test.messageContains != "" {
				require.Contains(t, response.Error.Message, test.messageContains)
			}
		})
	}
}

func TestMachineErrorUsesResourceSpecificNotFoundMessages(t *testing.T) {
	for _, test := range []struct {
		name     string
		err      error
		resource string
		code     string
	}{
		{name: "machine", err: service.ErrMachineNotFound, resource: "Dev Machine", code: "NOT_FOUND"},
		{name: "agent run", err: service.ErrAgentRunNotFound, resource: "Agent Run", code: "NOT_FOUND"},
		{name: "environment", err: service.ErrEnvironmentNotFound, resource: "Development Environment", code: "NOT_FOUND"},
		{name: "checkout", err: service.ErrCheckoutNotFound, resource: "Checkout", code: "NOT_FOUND"},
		{name: "terminal session", err: service.ErrTerminalSessionNotFound, resource: "Terminal Session", code: "NOT_FOUND"},
		{name: "service", err: service.ErrServiceNotAvailable, resource: "machine service", code: "SERVICE_NOT_AVAILABLE"},
	} {
		t.Run(test.name, func(t *testing.T) {
			e := echo.New()
			recorder := httptest.NewRecorder()
			ctx := e.NewContext(httptest.NewRequest(http.MethodGet, "/resource", nil), recorder)

			require.NoError(t, machineError(ctx, test.err))
			require.Equal(t, http.StatusNotFound, recorder.Code)
			var response dto.ErrorResponse
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
			require.Equal(t, test.code, response.Error.Code)
			require.Contains(t, response.Error.Message, test.resource)
		})
	}
}

func TestMachineErrorUsesTypedClassificationOnly(t *testing.T) {
	for _, test := range []struct {
		name            string
		err             error
		status          int
		code            string
		messageContains string
		messageExcludes string
	}{
		{name: "invalid input", err: fmt.Errorf("%w: invalid branch", service.ErrInvalidMachineInput), status: http.StatusBadRequest, code: "BAD_REQUEST", messageContains: "invalid branch"},
		{name: "state conflict", err: fmt.Errorf("%w: machine must be running", service.ErrInvalidOperation), status: http.StatusConflict, code: "INVALID_OPERATION"},
		{name: "quota", err: service.ErrMachineQuota, status: http.StatusConflict, code: "QUOTA_EXCEEDED"},
		{name: "name conflict", err: service.ErrMachineNameConflict, status: http.StatusConflict, code: "MACHINE_NAME_CONFLICT"},
		{name: "authentication", err: service.ErrMachineAuthentication, status: http.StatusUnauthorized, code: "UNAUTHORIZED"},
		{name: "authorization", err: service.ErrProviderNotAllowed, status: http.StatusForbidden, code: "FORBIDDEN"},
		{name: "private missing resource", err: service.ErrMachineNotFound, status: http.StatusNotFound, code: "NOT_FOUND"},
		{name: "raw invalid error", err: errors.New("invalid provider database secret"), status: http.StatusInternalServerError, code: "INTERNAL_ERROR", messageExcludes: "database secret"},
		{name: "raw unique error", err: errors.New("idx_dev_machines_workspace_name secret detail"), status: http.StatusInternalServerError, code: "INTERNAL_ERROR", messageExcludes: "secret detail"},
	} {
		t.Run(test.name, func(t *testing.T) {
			e := echo.New()
			recorder := httptest.NewRecorder()
			ctx := e.NewContext(httptest.NewRequest(http.MethodPost, "/machines", nil), recorder)

			require.NoError(t, machineError(ctx, test.err))
			require.Equal(t, test.status, recorder.Code)
			var response dto.ErrorResponse
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
			require.Equal(t, test.code, response.Error.Code)
			if test.messageContains != "" {
				require.Contains(t, response.Error.Message, test.messageContains)
			}
			if test.messageExcludes != "" {
				require.NotContains(t, response.Error.Message, test.messageExcludes)
			}
		})
	}
}

func TestCollectorIngestionReturnsAccurateFailures(t *testing.T) {
	workspaceID, machineID := uuid.New(), uuid.New()
	validToken := strings.Repeat("a", 64)
	runID, serviceID := uuid.New(), uuid.New()
	storageErr := errors.New("collector database secret detail")
	validEvent := `{"source":"collector","event_type":"heartbeat","payload":{}}`

	newStore := func() *collectorHandlerStoreFake {
		return &collectorHandlerStoreFake{
			token:   &domain.DevMachineToken{MachineID: machineID},
			machine: &domain.DevMachine{ID: machineID, WorkspaceID: workspaceID},
		}
	}
	missingRunStore := newStore()
	missingServiceStore := newStore()
	storageFailureStore := newStore()
	storageFailureStore.createEventErr = storageErr
	decryptionFailureStore := newStore()
	decryptionFailureStore.runtimeCredentials = []domain.DevMachineRuntimeCredential{{EncryptedValue: "invalid"}}
	successStore := newStore()
	authFailureStore := newStore()
	authFailureStore.authenticateErr = storageErr
	missingMachineStore := newStore()
	missingMachineStore.machine = nil

	for _, test := range []struct {
		name            string
		body            string
		token           string
		log             bool
		store           *collectorHandlerStoreFake
		status          int
		code            string
		messageExcludes string
	}{
		{name: "invalid payload", body: `{}`, token: validToken, store: newStore(), status: http.StatusBadRequest, code: "VALIDATION_ERROR"},
		{name: "invalid token", body: validEvent, token: "short", store: newStore(), status: http.StatusUnauthorized, code: "UNAUTHORIZED"},
		{name: "missing machine is private safe", body: validEvent, token: validToken, store: missingMachineStore, status: http.StatusUnauthorized, code: "UNAUTHORIZED"},
		{name: "authentication storage failure", body: validEvent, token: validToken, store: authFailureStore, status: http.StatusInternalServerError, code: "INTERNAL_ERROR", messageExcludes: "database secret"},
		{name: "missing agent run", body: fmt.Sprintf(`{"agent_run_id":%q,"source":"collector","event_type":"heartbeat","payload":{}}`, runID), token: validToken, store: missingRunStore, status: http.StatusNotFound, code: "NOT_FOUND"},
		{name: "missing service", body: fmt.Sprintf(`{"service_id":%q,"stream":"stdout","sequence":1,"content":"test"}`, serviceID), token: validToken, log: true, store: missingServiceStore, status: http.StatusNotFound, code: "SERVICE_NOT_AVAILABLE"},
		{name: "credential decryption failure", body: validEvent, token: validToken, store: decryptionFailureStore, status: http.StatusInternalServerError, code: "INTERNAL_ERROR"},
		{name: "temporary storage failure", body: validEvent, token: validToken, store: storageFailureStore, status: http.StatusInternalServerError, code: "INTERNAL_ERROR", messageExcludes: "database secret"},
		{name: "accepted", body: validEvent, token: validToken, store: successStore, status: http.StatusAccepted},
	} {
		t.Run(test.name, func(t *testing.T) {
			handler := NewDevMachineHandler(service.NewDevMachineService(
				test.store, agent.NewRegistry(), true, "machines.example.test", cryptoutil.DeriveKey("test"), time.Minute, service.DevMachineImages{},
			))
			request := httptest.NewRequest(http.MethodPost, "/api/dev-machine-ingest/events", strings.NewReader(test.body))
			request.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
			request.Header.Set(echo.HeaderAuthorization, "Bearer "+test.token)
			recorder := httptest.NewRecorder()
			ctx := echo.New().NewContext(request, recorder)

			var err error
			if test.log {
				err = handler.IngestLog(ctx)
			} else {
				err = handler.IngestEvent(ctx)
			}

			require.NoError(t, err)
			require.Equal(t, test.status, recorder.Code)
			if test.code == "" {
				require.Empty(t, recorder.Body.String())
				return
			}
			var body dto.ErrorResponse
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
			require.Equal(t, test.code, body.Error.Code)
			if test.messageExcludes != "" {
				require.NotContains(t, body.Error.Message, test.messageExcludes)
			}
		})
	}
}

func TestBulkDeleteReturnsStructuredAcceptedResponse(t *testing.T) {
	workspaceID, userID, machineID := uuid.New(), uuid.New(), uuid.New()
	store := &handlerDevMachineStoreFake{machine: &domain.DevMachine{ID: machineID, WorkspaceID: workspaceID, CreatedByUserID: &userID}}
	handler := NewDevMachineHandler(service.NewDevMachineService(
		store, agent.NewRegistry(), true, "machines.example.test", cryptoutil.DeriveKey("test"), time.Minute, service.DevMachineImages{},
	))
	request := httptest.NewRequest(http.MethodDelete, "/dev-machines/bulk", strings.NewReader(fmt.Sprintf(`{"machine_ids":[%q,%q]}`, machineID, machineID)))
	request.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	recorder := httptest.NewRecorder()
	ctx := devMachineHandlerContext(request, recorder, workspaceID, userID)

	require.NoError(t, handler.BulkDelete(ctx))
	require.Equal(t, http.StatusAccepted, recorder.Code)
	var result dto.BulkDeleteDevMachinesResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &result))
	require.Equal(t, 1, result.Count)
	require.Equal(t, 1, result.Requested)
	require.Equal(t, []dto.BulkDeleteDevMachineResult{{MachineID: machineID.String(), Status: "accepted"}}, result.Results)
	require.Equal(t, 1, store.permanentDeleteRequests)
}

func TestDevMachineGetIsCreatorScoped(t *testing.T) {
	workspaceID, ownerID, otherID, machineID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	store := &handlerDevMachineStoreFake{machine: &domain.DevMachine{
		ID: machineID, WorkspaceID: workspaceID, CreatedByUserID: &ownerID,
		Name: "builder-01", Status: domain.DevMachineStatusRunning, DesiredStatus: domain.DevMachineStatusRunning,
	}}
	h := NewDevMachineHandler(service.NewDevMachineService(
		store, agent.NewRegistry(), true, "machines.example.test", cryptoutil.DeriveKey("test"), time.Minute, service.DevMachineImages{},
	))

	recorder := httptest.NewRecorder()
	ctx := devMachineHandlerContext(httptest.NewRequest(http.MethodGet, "/dev-machines/"+machineID.String(), nil), recorder, workspaceID, otherID)
	ctx.SetPath("/dev-machines/:machineId")
	ctx.SetParamNames("machineId")
	ctx.SetParamValues(machineID.String())

	require.NoError(t, h.Get(ctx))
	require.Equal(t, http.StatusNotFound, recorder.Code)

	recorder = httptest.NewRecorder()
	ctx = devMachineHandlerContext(httptest.NewRequest(http.MethodGet, "/dev-machines/"+machineID.String(), nil), recorder, workspaceID, ownerID)
	ctx.SetPath("/dev-machines/:machineId")
	ctx.SetParamNames("machineId")
	ctx.SetParamValues(machineID.String())

	require.NoError(t, h.Get(ctx))
	require.Equal(t, http.StatusOK, recorder.Code)
}

func modelServerHandler(store *handlerDevMachineStoreFake) *DevMachineHandler {
	return NewDevMachineHandler(service.NewDevMachineService(
		store, agent.NewRegistry(), true, "machines.example.test", cryptoutil.DeriveKey("test"), time.Minute, service.DevMachineImages{},
	))
}

func sendModelServer(t *testing.T, h *DevMachineHandler, method, path, body, role string, workspaceID, userID uuid.UUID, serverID *uuid.UUID) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	ctx := devMachineHandlerContext(request, recorder, workspaceID, userID)
	ctx.Set("workspace_role", role)
	if serverID != nil {
		ctx.SetParamNames("serverId")
		ctx.SetParamValues(serverID.String())
	}
	switch {
	case method == http.MethodGet:
		require.NoError(t, h.ListModelServers(ctx))
	case method == http.MethodPost && strings.HasSuffix(path, "/discover"):
		require.NoError(t, h.DiscoverModels(ctx))
	case method == http.MethodPost:
		require.NoError(t, h.CreateModelServer(ctx))
	case method == http.MethodPut:
		require.NoError(t, h.UpdateModelServer(ctx))
	case method == http.MethodDelete:
		require.NoError(t, h.DeleteModelServer(ctx))
	}
	return recorder
}

func TestModelServerResponsesNeverExposeTheAPIKey(t *testing.T) {
	workspaceID, userID := uuid.New(), uuid.New()
	store := &handlerDevMachineStoreFake{}
	h := modelServerHandler(store)

	created := sendModelServer(t, h, http.MethodPost, "/agent-model-servers", `{"name":"DeepSeek","openai_base_url":"https://api.deepseek.com","anthropic_base_url":"https://api.deepseek.com/anthropic","api_key":"sk-handler-secret","models":["deepseek-flash","deepseek-v4-pro"],"default_model":"deepseek-flash","thinking":"high"}`, domain.RoleOwner, workspaceID, userID, nil)
	require.Equal(t, http.StatusCreated, created.Code, created.Body.String())
	require.NotContains(t, created.Body.String(), "sk-handler-secret")
	var server dto.ModelServerResponse
	require.NoError(t, json.Unmarshal(created.Body.Bytes(), &server))
	require.Equal(t, []string{"codex", "claude-code"}, server.Harnesses)
	encrypted := store.modelServers[0].EncryptedAPIKey

	// Members read configuration so they can choose a server; never the key.
	listed := sendModelServer(t, h, http.MethodGet, "/agent-model-servers", "", domain.RoleMember, workspaceID, userID, nil)
	require.Equal(t, http.StatusOK, listed.Code)
	require.NotContains(t, listed.Body.String(), "sk-handler-secret")
	require.NotContains(t, listed.Body.String(), encrypted)
	var servers []dto.ModelServerResponse
	require.NoError(t, json.Unmarshal(listed.Body.Bytes(), &servers))
	require.Len(t, servers, 1)
	require.True(t, servers[0].IsDefault)

	updated := sendModelServer(t, h, http.MethodPut, "/agent-model-servers/"+server.ID.String(), `{"name":"DeepSeek","openai_base_url":"https://api.deepseek.com","default_model":"deepseek-v4-pro","models":["deepseek-flash","deepseek-v4-pro"],"thinking":"low"}`, domain.RoleAdmin, workspaceID, userID, &server.ID)
	require.Equal(t, http.StatusOK, updated.Code, updated.Body.String())
	require.Equal(t, encrypted, store.modelServers[0].EncryptedAPIKey, "an omitted key is kept")

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		denied := sendModelServer(t, h, method, "/agent-model-servers", `{"name":"X","openai_base_url":"https://a.test","api_key":"k","default_model":"m","thinking":"high"}`, domain.RoleMember, workspaceID, userID, &server.ID)
		require.Equal(t, http.StatusForbidden, denied.Code, method)
	}
	denied := sendModelServer(t, h, http.MethodPost, "/agent-model-servers/discover", `{"openai_base_url":"https://a.test","api_key":"k"}`, domain.RoleMember, workspaceID, userID, nil)
	require.Equal(t, http.StatusForbidden, denied.Code, "only admins send keys to chosen URLs")

	invalid := sendModelServer(t, h, http.MethodPost, "/agent-model-servers", `{"name":"Nowhere","api_key":"k","default_model":"m","thinking":"high"}`, domain.RoleOwner, workspaceID, userID, nil)
	require.Equal(t, http.StatusBadRequest, invalid.Code)

	deleted := sendModelServer(t, h, http.MethodDelete, "/agent-model-servers/"+server.ID.String(), "", domain.RoleOwner, workspaceID, userID, &server.ID)
	require.Equal(t, http.StatusNoContent, deleted.Code)
	require.Empty(t, store.modelServers)
	missing := sendModelServer(t, h, http.MethodDelete, "/agent-model-servers/"+server.ID.String(), "", domain.RoleOwner, workspaceID, userID, &server.ID)
	require.Equal(t, http.StatusNotFound, missing.Code)
}

func TestDevMachinePolicyIncludesConfiguredDiskQuotaMode(t *testing.T) {
	workspaceID := uuid.New()
	for _, test := range []struct {
		mode       string
		clientMode string
	}{
		{mode: config.DiskQuotaModeHard, clientMode: config.DiskQuotaModeMonitored},
		{mode: config.DiskQuotaModeMonitored, clientMode: config.DiskQuotaModeHard},
	} {
		t.Run(test.mode, func(t *testing.T) {
			store := &handlerDevMachineStoreFake{policy: &domain.DevMachineWorkspacePolicy{
				WorkspaceID: workspaceID, Enabled: true, MaxConcurrentMachines: 5, MaxMachinesPerUser: 2,
				MaxDailyAgentRuns: 25, MaxRuntimeMinutes: 480, MaxDiskGB: 100, IdlePauseMinutes: 240,
				AllowedProviders:    json.RawMessage(`["claude-code","opencode","codex"]`),
				AllowedRepositories: json.RawMessage(`[]`),
			}}
			h := NewDevMachineHandler(service.NewDevMachineService(
				store, agent.NewRegistry(), true, "machines.example.test", cryptoutil.DeriveKey("test"), time.Minute, service.DevMachineImages{},
			), test.mode)

			recorder := httptest.NewRecorder()
			ctx := devMachineHandlerContext(httptest.NewRequest(http.MethodGet, "/dev-machine-policy", nil), recorder, workspaceID, uuid.New())
			require.NoError(t, h.GetPolicy(ctx))
			require.Equal(t, http.StatusOK, recorder.Code)
			var current domain.DevMachineWorkspacePolicy
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &current))
			require.Equal(t, test.mode, current.DiskQuotaMode)
			require.Equal(t, []int{20, 30, 40, 50, 75, 100}, current.SizeOptions.DiskGB)
			require.Equal(t, []int{1000, 2000, 4000, 6000, 8000, 12000, 16000}, current.SizeOptions.CPUMillis)

			body := fmt.Sprintf(`{
				"enabled": true,
				"max_concurrent_machines": 5,
				"max_machines_per_user": 2,
				"max_daily_agent_runs": 25,
				"max_runtime_minutes": 480,
				"max_disk_gb": 100,
				"allowed_providers": ["opencode"],
				"allowed_repositories": [],
				"allow_custom_providers": false,
				"idle_pause_minutes": 240,
				"disk_quota_mode": %q,
				"size_options": {"cpu_millis": [16000], "memory_mb": [65536], "disk_gb": [2048]}
			}`, test.clientMode)
			recorder = httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPatch, "/dev-machine-policy", strings.NewReader(body))
			request.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
			ctx = devMachineHandlerContext(request, recorder, workspaceID, uuid.New())
			require.NoError(t, h.UpdatePolicy(ctx))
			require.Equal(t, http.StatusOK, recorder.Code)
			var updated domain.DevMachineWorkspacePolicy
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &updated))
			require.Equal(t, test.mode, updated.DiskQuotaMode)
			require.Equal(t, []int{20, 30, 40, 50, 75, 100}, updated.SizeOptions.DiskGB)
			require.Empty(t, store.policy.DiskQuotaMode)
		})
	}
}

// The Docker daemon is off by default. A policy update without allow_docker
// (an older client) keeps the current choice instead of turning it off.
func TestDevMachinePolicyDockerSwitchIsKeptUnlessSent(t *testing.T) {
	workspaceID := uuid.New()
	store := &handlerDevMachineStoreFake{policy: &domain.DevMachineWorkspacePolicy{
		WorkspaceID: workspaceID, Enabled: true, MaxConcurrentMachines: 5, MaxMachinesPerUser: 2,
		MaxDailyAgentRuns: 25, MaxRuntimeMinutes: 480, MaxDiskGB: 100, IdlePauseMinutes: 240,
		AllowedProviders: json.RawMessage(`["opencode"]`), AllowedRepositories: json.RawMessage(`[]`),
	}}
	h := NewDevMachineHandler(service.NewDevMachineService(
		store, agent.NewRegistry(), true, "machines.example.test", cryptoutil.DeriveKey("test"), time.Minute, service.DevMachineImages{},
	), config.DiskQuotaModeHard)
	update := func(extra string) domain.DevMachineWorkspacePolicy {
		t.Helper()
		body := `{"enabled":true,"max_concurrent_machines":5,"max_machines_per_user":2,"max_daily_agent_runs":25,
			"max_runtime_minutes":480,"max_disk_gb":100,"allowed_providers":["opencode"],"allowed_repositories":[],
			"idle_pause_minutes":240` + extra + `}`
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPatch, "/dev-machine-policy", strings.NewReader(body))
		request.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		require.NoError(t, h.UpdatePolicy(devMachineHandlerContext(request, recorder, workspaceID, uuid.New())))
		require.Equal(t, http.StatusOK, recorder.Code)
		var updated domain.DevMachineWorkspacePolicy
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &updated))
		return updated
	}
	require.False(t, update("").AllowDocker, "off by default")
	require.True(t, update(`,"allow_docker":true`).AllowDocker)
	require.True(t, store.policy.AllowDocker)
	require.True(t, update("").AllowDocker, "omitting the field keeps it on")
	require.False(t, update(`,"allow_docker":false`).AllowDocker)
	require.False(t, store.policy.AllowDocker)
}

// Sudo is off by default. A policy update without allow_sudo (an older
// client) keeps the current choice, and it is independent of allow_docker.
func TestDevMachinePolicySudoSwitchIsKeptUnlessSent(t *testing.T) {
	workspaceID := uuid.New()
	store := &handlerDevMachineStoreFake{policy: &domain.DevMachineWorkspacePolicy{
		WorkspaceID: workspaceID, Enabled: true, MaxConcurrentMachines: 5, MaxMachinesPerUser: 2,
		MaxDailyAgentRuns: 25, MaxRuntimeMinutes: 480, MaxDiskGB: 100, IdlePauseMinutes: 240,
		AllowedProviders: json.RawMessage(`["opencode"]`), AllowedRepositories: json.RawMessage(`[]`), AllowDocker: true,
	}}
	h := NewDevMachineHandler(service.NewDevMachineService(
		store, agent.NewRegistry(), true, "machines.example.test", cryptoutil.DeriveKey("test"), time.Minute, service.DevMachineImages{},
	), config.DiskQuotaModeHard)
	update := func(extra string) domain.DevMachineWorkspacePolicy {
		t.Helper()
		body := `{"enabled":true,"max_concurrent_machines":5,"max_machines_per_user":2,"max_daily_agent_runs":25,
			"max_runtime_minutes":480,"max_disk_gb":100,"allowed_providers":["opencode"],"allowed_repositories":[],
			"idle_pause_minutes":240` + extra + `}`
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPatch, "/dev-machine-policy", strings.NewReader(body))
		request.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		require.NoError(t, h.UpdatePolicy(devMachineHandlerContext(request, recorder, workspaceID, uuid.New())))
		require.Equal(t, http.StatusOK, recorder.Code)
		var updated domain.DevMachineWorkspacePolicy
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &updated))
		return updated
	}
	first := update("")
	require.False(t, first.AllowSudo, "off by default")
	require.True(t, first.AllowDocker, "the Docker switch is kept")
	require.True(t, update(`,"allow_sudo":true`).AllowSudo)
	require.True(t, store.policy.AllowSudo)
	kept := update("")
	require.True(t, kept.AllowSudo, "omitting the field keeps it on")
	require.True(t, kept.AllowDocker)
	off := update(`,"allow_sudo":false,"allow_docker":true`)
	require.False(t, off.AllowSudo)
	require.True(t, off.AllowDocker)
	require.False(t, store.policy.AllowSudo)
}

func TestDevMachineCreateRejectsInvalidCustomResources(t *testing.T) {
	workspaceID, userID := uuid.New(), uuid.New()
	for _, test := range []struct {
		name       string
		body       string
		errMessage string
	}{
		{
			name: "cpu", body: `{"name":"custom-builder","size":"custom","cpu_millis":1500,"memory_mb":4096,"disk_gb":20}`,
			errMessage: "cpu_millis 1500 is not a supported option",
		},
		{
			name: "memory ratio", body: `{"name":"custom-builder","size":"custom","cpu_millis":4000,"memory_mb":2048,"disk_gb":20}`,
			errMessage: "memory_mb 2048 must be at least 4096 for cpu_millis 4000 (1 GiB per vCPU)",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &handlerDevMachineStoreFake{policy: testPolicyForHandler(workspaceID)}
			h := NewDevMachineHandler(service.NewDevMachineService(
				store, agent.NewRegistry(), true, "machines.example.test", cryptoutil.DeriveKey("test"), time.Minute, service.DevMachineImages{},
			))
			request := httptest.NewRequest(http.MethodPost, "/dev-machines", strings.NewReader(test.body))
			request.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
			recorder := httptest.NewRecorder()
			ctx := devMachineHandlerContext(request, recorder, workspaceID, userID)

			require.NoError(t, h.Create(ctx))
			require.Equal(t, http.StatusBadRequest, recorder.Code)
			var body dto.ErrorResponse
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body), recorder.Body.String())
			require.Equal(t, "BAD_REQUEST", body.Error.Code)
			require.Contains(t, body.Error.Message, test.errMessage)
		})
	}
}

func TestDevMachineCreateReturnsSpecificQuotaMessages(t *testing.T) {
	workspaceID, userID := uuid.New(), uuid.New()
	for _, test := range []struct {
		name           string
		body           string
		workspaceCount int
		userCount      int
		errMessage     string
	}{
		{
			name: "workspace", body: `{"name":"quota-builder","size":"small"}`,
			workspaceCount: 10, userCount: 1,
			errMessage: "the workspace already has 10 of 10 allowed active Dev Machines",
		},
		{
			name: "per user", body: `{"name":"quota-builder","size":"small"}`,
			workspaceCount: 1, userCount: 2,
			errMessage: "you already have 2 of 2 allowed Dev Machines; delete or stop one, or ask an admin to raise the per-user limit",
		},
		{
			name: "custom disk", body: `{"name":"quota-builder","size":"custom","cpu_millis":2000,"memory_mb":4096,"disk_gb":150}`,
			errMessage: "machine disk request of 150 GB exceeds the workspace limit of 100 GB",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			policy := testPolicyForHandler(workspaceID)
			store := &handlerDevMachineStoreFake{
				policy: policy, workspaceActiveCount: test.workspaceCount, userActiveCount: test.userCount,
			}
			h := NewDevMachineHandler(service.NewDevMachineService(
				store, agent.NewRegistry(), true, "machines.example.test", cryptoutil.DeriveKey("test"), time.Minute, service.DevMachineImages{},
			))
			request := httptest.NewRequest(http.MethodPost, "/dev-machines", strings.NewReader(test.body))
			request.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
			recorder := httptest.NewRecorder()
			ctx := devMachineHandlerContext(request, recorder, workspaceID, userID)

			require.NoError(t, h.Create(ctx))
			require.Equal(t, http.StatusConflict, recorder.Code)
			var body dto.ErrorResponse
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body), recorder.Body.String())
			require.Equal(t, "QUOTA_EXCEEDED", body.Error.Code)
			require.Equal(t, test.errMessage, body.Error.Message)
		})
	}
}

func TestTerminalCloseReturnsAcceptedWhileRuntimeCleanupIsPending(t *testing.T) {
	workspaceID, userID, machineID, sessionID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	store := &handlerDevMachineStoreFake{machine: &domain.DevMachine{
		ID: machineID, WorkspaceID: workspaceID, CreatedByUserID: &userID, Generation: 3,
	}}
	h := NewDevMachineHandler(service.NewDevMachineService(
		store, agent.NewRegistry(), true, "machines.example.test", cryptoutil.DeriveKey("test"), time.Minute, service.DevMachineImages{},
	))
	recorder := httptest.NewRecorder()
	ctx := devMachineHandlerContext(httptest.NewRequest(http.MethodPost, "/dev-machines/"+machineID.String()+"/terminal-sessions/"+sessionID.String()+"/close", nil), recorder, workspaceID, userID)
	ctx.SetPath("/dev-machines/:machineId/terminal-sessions/:sessionId/close")
	ctx.SetParamNames("machineId", "sessionId")
	ctx.SetParamValues(machineID.String(), sessionID.String())

	require.NoError(t, h.CloseTerminalSession(ctx))
	require.Equal(t, http.StatusAccepted, recorder.Code)
	require.NotNil(t, store.terminalCloseOperation)
	require.Equal(t, domain.DevMachineOpTerminateTerminal, store.terminalCloseOperation.Action)
}

type handlerDevMachineStoreFake struct {
	repository.DevMachineStore
	machine                 *domain.DevMachine
	policy                  *domain.DevMachineWorkspacePolicy
	workspaceActiveCount    int
	userActiveCount         int
	permanentDeleteRequests int
	terminalCloseOperation  *domain.DevMachineOperation
	modelServers            []domain.AgentModelServer
}

func (f *handlerDevMachineStoreFake) GetPolicy(context.Context, uuid.UUID) (*domain.DevMachineWorkspacePolicy, error) {
	return f.policy, nil
}

func (f *handlerDevMachineStoreFake) UpsertPolicy(_ context.Context, policy *domain.DevMachineWorkspacePolicy) error {
	f.policy = policy
	return nil
}

func (f *handlerDevMachineStoreFake) CountActiveMachines(_ context.Context, _ uuid.UUID, userID *uuid.UUID) (int, error) {
	if userID != nil {
		return f.userActiveCount, nil
	}
	return f.workspaceActiveCount, nil
}

func (f *handlerDevMachineStoreFake) ListModelServers(context.Context, uuid.UUID) ([]domain.AgentModelServer, error) {
	return append([]domain.AgentModelServer(nil), f.modelServers...), nil
}

func (f *handlerDevMachineStoreFake) GetModelServer(_ context.Context, _ uuid.UUID, id *uuid.UUID) (*domain.AgentModelServer, error) {
	for _, server := range f.modelServers {
		if (id == nil && server.IsDefault) || (id != nil && server.ID == *id) {
			copy := server
			return &copy, nil
		}
	}
	return nil, nil
}

func (f *handlerDevMachineStoreFake) SaveModelServer(_ context.Context, server *domain.AgentModelServer) error {
	if server.ID == uuid.Nil {
		server.ID = uuid.New()
	}
	for i := range f.modelServers {
		if f.modelServers[i].ID == server.ID {
			f.modelServers[i] = *server
			return nil
		}
	}
	f.modelServers = append(f.modelServers, *server)
	return nil
}

func (f *handlerDevMachineStoreFake) DeleteModelServer(_ context.Context, _ uuid.UUID, id uuid.UUID) error {
	for i := range f.modelServers {
		if f.modelServers[i].ID == id {
			f.modelServers = append(f.modelServers[:i], f.modelServers[i+1:]...)
			return nil
		}
	}
	return sql.ErrNoRows
}

type collectorHandlerStoreFake struct {
	repository.DevMachineStore
	token              *domain.DevMachineToken
	machine            *domain.DevMachine
	authenticateErr    error
	createEventErr     error
	runtimeCredentials []domain.DevMachineRuntimeCredential
}

func (f *collectorHandlerStoreFake) AuthenticateMachineToken(context.Context, string, string) (*domain.DevMachineToken, *domain.DevMachine, error) {
	return f.token, f.machine, f.authenticateErr
}

func (f *collectorHandlerStoreFake) GetAgentRun(context.Context, uuid.UUID, uuid.UUID) (*domain.DevMachineAgentRun, error) {
	return nil, nil
}

func (f *collectorHandlerStoreFake) ListServices(context.Context, uuid.UUID, uuid.UUID) ([]domain.DevMachineService, error) {
	return nil, nil
}

func (f *collectorHandlerStoreFake) ListEnvVarsInternal(context.Context, uuid.UUID, *string, string) ([]domain.DevMachineEnvVar, error) {
	return nil, nil
}

func (f *collectorHandlerStoreFake) ListRuntimeCredentials(context.Context, uuid.UUID) ([]domain.DevMachineRuntimeCredential, error) {
	return f.runtimeCredentials, nil
}

func (f *collectorHandlerStoreFake) CreateEvent(context.Context, *domain.DevMachineEvent) error {
	return f.createEventErr
}

func (f *collectorHandlerStoreFake) CreateLogChunk(context.Context, *domain.DevMachineLogChunk) error {
	return nil
}

func (f *handlerDevMachineStoreFake) GetMachineForUser(_ context.Context, workspaceID, machineID, userID uuid.UUID) (*domain.DevMachine, error) {
	if f.machine == nil || f.machine.WorkspaceID != workspaceID || f.machine.ID != machineID || f.machine.CreatedByUserID == nil || *f.machine.CreatedByUserID != userID {
		return nil, nil
	}
	return f.machine, nil
}

func (f *handlerDevMachineStoreFake) GetMachine(_ context.Context, workspaceID, machineID uuid.UUID) (*domain.DevMachine, error) {
	if f.machine == nil || f.machine.WorkspaceID != workspaceID || f.machine.ID != machineID {
		return nil, nil
	}
	return f.machine, nil
}

func (f *handlerDevMachineStoreFake) RequestPermanentDelete(_ context.Context, workspaceID, machineID uuid.UUID, requestedByUserID *uuid.UUID) (*domain.DevMachineOperation, error) {
	f.permanentDeleteRequests++
	return &domain.DevMachineOperation{
		ID: uuid.New(), WorkspaceID: workspaceID, MachineID: machineID, RequestedByUserID: requestedByUserID,
		Action: domain.DevMachineOpTeardown, Status: domain.DevMachineOpStatusPending,
	}, nil
}

func (f *handlerDevMachineStoreFake) RequestTerminalSessionClose(_ context.Context, workspaceID, machineID, userID, sessionID uuid.UUID, operation *domain.DevMachineOperation) (*domain.DevMachineTerminalSession, error) {
	operation.TerminalSessionID = &sessionID
	f.terminalCloseOperation = operation
	return &domain.DevMachineTerminalSession{
		ID: sessionID, WorkspaceID: workspaceID, MachineID: machineID, UserID: userID,
		RuntimeSessionName: "term-test", Status: "closing", CreatedAt: time.Now(), LastActivityAt: time.Now(),
	}, nil
}

func (f *handlerDevMachineStoreFake) CreateEvent(context.Context, *domain.DevMachineEvent) error {
	return nil
}

func devMachineHandlerContext(req *http.Request, recorder *httptest.ResponseRecorder, workspaceID, userID uuid.UUID) echo.Context {
	e := echo.New()
	ctx := e.NewContext(req, recorder)
	ctx.Set("workspace", &domain.Workspace{ID: workspaceID})
	ctx.Set(string(middleware.UserIDKey), userID)
	return ctx
}

func testPolicyForHandler(workspaceID uuid.UUID) *domain.DevMachineWorkspacePolicy {
	return &domain.DevMachineWorkspacePolicy{
		WorkspaceID: workspaceID, Enabled: true, MaxConcurrentMachines: 10, MaxMachinesPerUser: 2,
		MaxDailyAgentRuns: 25, MaxRuntimeMinutes: 480, MaxDiskGB: 100, IdlePauseMinutes: 240,
		AllowedProviders: json.RawMessage(`[]`), AllowedRepositories: json.RawMessage(`[]`),
	}
}

func (f *collectorHandlerStoreFake) ListModelServers(context.Context, uuid.UUID) ([]domain.AgentModelServer, error) {
	return nil, nil
}
func (f *collectorHandlerStoreFake) ListAgentRunSecretSnapshots(context.Context, uuid.UUID, uuid.UUID) ([]string, error) {
	return nil, nil
}

func TestRepositoryDefaultsValidatesScopeAndPolicy(t *testing.T) {
	workspaceID, userID := uuid.New(), uuid.New()
	store := &handlerDevMachineStoreFake{policy: &domain.DevMachineWorkspacePolicy{WorkspaceID: workspaceID, Enabled: false}}
	h := modelServerHandler(store)
	get := func(query string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		ctx := devMachineHandlerContext(httptest.NewRequest(http.MethodGet, "/dev-machine-repository-defaults"+query, nil), recorder, workspaceID, userID)
		require.NoError(t, h.RepositoryDefaults(ctx))
		return recorder
	}
	require.Equal(t, http.StatusBadRequest, get("?issue_id=not-a-uuid").Code)
	require.Equal(t, http.StatusBadRequest, get("?project_id=1").Code)
	require.Equal(t, http.StatusForbidden, get("?issue_id="+uuid.NewString()).Code, "disabled Dev Machines expose no development defaults")
}
