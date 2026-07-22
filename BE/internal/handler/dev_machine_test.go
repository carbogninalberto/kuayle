package handler

import (
	"context"
	"encoding/json"
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
