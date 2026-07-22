package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
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
		name   string
		err    error
		status int
		code   string
	}{
		{name: "missing", err: service.ErrEnvironmentNotFound, status: http.StatusNotFound, code: "NOT_FOUND"},
		{name: "conflict", err: fmt.Errorf("%w: environment build is active", service.ErrInvalidOperation), status: http.StatusConflict, code: "INVALID_OPERATION"},
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
