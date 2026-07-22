package handler

import (
	"context"
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

func TestAgentRunTraceRejectsInvalidRunID(t *testing.T) {
	e := echo.New()
	recorder := httptest.NewRecorder()
	ctx := e.NewContext(httptest.NewRequest(http.MethodGet, "/agent-runs/not-a-uuid/trace", nil), recorder)
	ctx.SetPath("/agent-runs/:agentRunId/trace")
	ctx.SetParamNames("agentRunId")
	ctx.SetParamValues("not-a-uuid")

	h := &DevMachineHandler{service: nil}
	err := h.AgentRunTrace(ctx)

	require.NoError(t, err)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	var response dto.ErrorResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, "BAD_REQUEST", response.Error.Code)
}

func TestAgentRunTraceDefaultsQueryParams(t *testing.T) {
	e := echo.New()
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/agent-runs/00000000-0000-0000-0000-000000000001/trace", nil)
	ctx := e.NewContext(req, recorder)
	ctx.SetPath("/agent-runs/:agentRunId/trace")
	ctx.SetParamNames("agentRunId")
	ctx.SetParamValues("00000000-0000-0000-0000-000000000001")

	var params dto.TraceListParams
	require.NoError(t, ctx.Bind(&params))
	params.Defaults()
	require.Equal(t, 200, params.EventsLimit)
	require.Equal(t, 500, params.LogsLimit)
	require.Equal(t, int64(0), params.EventsAfterID)
	require.Equal(t, int64(0), params.LogsAfterID)
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

type handlerDevMachineStoreFake struct {
	repository.DevMachineStore
	machine *domain.DevMachine
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
