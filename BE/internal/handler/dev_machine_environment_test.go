package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/agent"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/service"
	cryptoutil "github.com/kuayle/kuayle-backend/pkg/crypto"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"
)

type environmentHandlerStoreFake struct {
	*handlerDevMachineStoreFake
	queued int
}

func (f *environmentHandlerStoreFake) ListServices(context.Context, uuid.UUID, uuid.UUID) ([]domain.DevMachineService, error) {
	container := "developer"
	return []domain.DevMachineService{{ID: uuid.New(), ServiceType: "terminal", InternalPort: 7681, Status: "running", ContainerID: &container}}, nil
}
func (f *environmentHandlerStoreFake) GetEnvironmentCheck(context.Context, uuid.UUID) (*domain.DevMachineEnvironmentCheck, error) {
	return &domain.DevMachineEnvironmentCheck{ContainerID: "developer", PackageIndexState: domain.PackageIndexAbsent, CheckedAt: time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC), RefreshState: domain.PackageIndexRefreshIdle}, nil
}
func (f *environmentHandlerStoreFake) RequestPackageIndexRefresh(context.Context, uuid.UUID, uuid.UUID, string, uuid.UUID) (bool, error) {
	f.queued++
	return true, nil
}

func sendEnvironment(t *testing.T, h *DevMachineHandler, method string, workspaceID, userID, machineID uuid.UUID) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, "/dev-machines/"+machineID.String()+"/environment-status", nil)
	ctx := devMachineHandlerContext(request, recorder, workspaceID, userID)
	ctx.Set("workspace_role", "member")
	ctx.SetPathValues(echo.PathValues{{Name: "machineId", Value: machineID.String()}})
	if method == http.MethodGet {
		require.NoError(t, h.EnvironmentStatus(ctx))
	} else {
		require.NoError(t, h.RefreshPackageIndex(ctx))
	}
	return recorder
}

func TestEnvironmentStatusEndpointsFollowTheContract(t *testing.T) {
	workspaceID, ownerID, machineID := uuid.New(), uuid.New(), uuid.New()
	store := &environmentHandlerStoreFake{handlerDevMachineStoreFake: &handlerDevMachineStoreFake{
		policy: testPolicyForHandler(workspaceID),
		machine: &domain.DevMachine{ID: machineID, WorkspaceID: workspaceID, CreatedByUserID: &ownerID, RoutingKey: "0123456789abcdef0123",
			Status: domain.DevMachineStatusRunning, DesiredStatus: domain.DevMachineStatusRunning},
	}}
	h := NewDevMachineHandler(service.NewDevMachineService(store, agent.NewRegistry(), true, "machines.example.test", cryptoutil.DeriveKey("test"), time.Minute, service.DevMachineImages{}))

	read := sendEnvironment(t, h, http.MethodGet, workspaceID, ownerID, machineID)
	require.Equal(t, http.StatusOK, read.Code, read.Body.String())
	var body map[string]any
	require.NoError(t, json.Unmarshal(read.Body.Bytes(), &body))
	require.Equal(t, map[string]any{"package_index": map[string]any{
		"state": "absent", "index_mtime": nil, "sudo_available": false, "checked_at": "2026-11-15T09:00:00Z", "refresh": nil,
	}}, body)

	refused := sendEnvironment(t, h, http.MethodPost, workspaceID, ownerID, machineID)
	require.Equal(t, http.StatusForbidden, refused.Code)
	var problem dto.ErrorResponse
	require.NoError(t, json.Unmarshal(refused.Body.Bytes(), &problem))
	require.Equal(t, "SUDO_DISABLED", problem.Error.Code)
	require.Contains(t, problem.Error.Message, "Settings → Dev Machines")
	require.Contains(t, problem.Error.Message, "sudo apt-get update")
	require.Zero(t, store.queued)

	store.machine.SudoEnabled = true
	accepted := sendEnvironment(t, h, http.MethodPost, workspaceID, ownerID, machineID)
	require.Equal(t, http.StatusAccepted, accepted.Code, accepted.Body.String())
	require.Equal(t, 1, store.queued)

	store.machine.Status = domain.DevMachineStatusStopped
	stopped := sendEnvironment(t, h, http.MethodPost, workspaceID, ownerID, machineID)
	require.Equal(t, http.StatusConflict, stopped.Code)
	require.Equal(t, "MACHINE_NOT_RUNNING", errorCode(t, stopped))
}
