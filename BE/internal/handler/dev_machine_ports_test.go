package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/agent"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/internal/service"
	cryptoutil "github.com/kuayle/kuayle-backend/pkg/crypto"
	"github.com/labstack/echo/v4"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

type portHandlerStoreFake struct {
	*handlerDevMachineStoreFake
	ports []domain.DevMachinePort
}

func (f *portHandlerStoreFake) ListServices(context.Context, uuid.UUID, uuid.UUID) ([]domain.DevMachineService, error) {
	return []domain.DevMachineService{{ID: uuid.New(), ServiceType: "ide", InternalPort: 8080, Status: "running"}}, nil
}
func (f *portHandlerStoreFake) ListPorts(context.Context, uuid.UUID, uuid.UUID) ([]domain.DevMachinePort, error) {
	return f.ports, nil
}
func (f *portHandlerStoreFake) CreatePort(_ context.Context, port *domain.DevMachinePort) error {
	for _, existing := range f.ports {
		if existing.Port == port.Port {
			return repository.ErrPortExists
		}
	}
	port.ID, port.CreatedAt, port.Target = uuid.New(), time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC), domain.DevMachinePortTargetDeveloper
	f.ports = append(f.ports, *port)
	return nil
}
func (f *portHandlerStoreFake) DeletePort(_ context.Context, _, _, portID, _ uuid.UUID) (*domain.DevMachinePort, error) {
	for i, port := range f.ports {
		if port.ID == portID {
			f.ports = append(f.ports[:i], f.ports[i+1:]...)
			return &port, nil
		}
	}
	return nil, nil
}

func sendPort(t *testing.T, h *DevMachineHandler, method, body string, workspaceID, userID, machineID uuid.UUID, portID *uuid.UUID) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, "/dev-machines/"+machineID.String()+"/ports", strings.NewReader(body))
	request.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	ctx := devMachineHandlerContext(request, recorder, workspaceID, userID)
	ctx.Set("workspace_role", "member")
	names, values := []string{"machineId"}, []string{machineID.String()}
	if portID != nil {
		names, values = append(names, "portId"), append(values, portID.String())
	}
	ctx.SetParamNames(names...)
	ctx.SetParamValues(values...)
	switch method {
	case http.MethodGet:
		require.NoError(t, h.ListPorts(ctx))
	case http.MethodPost:
		require.NoError(t, h.CreatePort(ctx))
	case http.MethodDelete:
		require.NoError(t, h.DeletePort(ctx))
	}
	return recorder
}

func errorCode(t *testing.T, recorder *httptest.ResponseRecorder) string {
	t.Helper()
	var response dto.ErrorResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	return response.Error.Code
}

func TestAppPortEndpointsFollowTheContract(t *testing.T) {
	workspaceID, ownerID, machineID := uuid.New(), uuid.New(), uuid.New()
	store := &portHandlerStoreFake{handlerDevMachineStoreFake: &handlerDevMachineStoreFake{
		policy: testPolicyForHandler(workspaceID),
		machine: &domain.DevMachine{ID: machineID, WorkspaceID: workspaceID, CreatedByUserID: &ownerID, RoutingKey: "0123456789abcdef0123",
			Status: domain.DevMachineStatusRunning, DesiredStatus: domain.DevMachineStatusRunning,
			ListeningPorts: pq.Int64Array{3000}},
	}}
	h := NewDevMachineHandler(service.NewDevMachineService(store, agent.NewRegistry(), true, "machines.example.test", cryptoutil.DeriveKey("test"), time.Minute, service.DevMachineImages{}))

	created := sendPort(t, h, http.MethodPost, `{"port":3000,"label":"web"}`, workspaceID, ownerID, machineID, nil)
	require.Equal(t, http.StatusCreated, created.Code, created.Body.String())
	var body map[string]any
	require.NoError(t, json.Unmarshal(created.Body.Bytes(), &body))
	require.Equal(t, map[string]any{
		"id": body["id"], "port": float64(3000), "label": "web", "source": "user", "agent_task_id": nil,
		"task_identifier": nil, "host": "0123456789abcdef0123-3000.machines.example.test", "listening": true, "target": "developer",
		"created_at": "2026-10-28T09:00:00Z",
	}, body)

	listed := sendPort(t, h, http.MethodGet, "", workspaceID, ownerID, machineID, nil)
	require.Equal(t, http.StatusOK, listed.Code)
	var list map[string]any
	require.NoError(t, json.Unmarshal(listed.Body.Bytes(), &list))
	require.Len(t, list["ports"], 1)
	require.Equal(t, []any{float64(3000)}, list["listening"])
	require.Contains(t, list, "scanned_at")

	for _, test := range []struct {
		body, code string
		status     int
	}{
		{`{"port":3000}`, "PORT_EXISTS", http.StatusConflict},
		{`{"port":8080}`, "PORT_RESERVED", http.StatusUnprocessableEntity},
		{`{"port":80}`, "BAD_REQUEST", http.StatusBadRequest},
		{`{"port":3001,"extra":true}`, "BAD_REQUEST", http.StatusBadRequest},
	} {
		recorder := sendPort(t, h, http.MethodPost, test.body, workspaceID, ownerID, machineID, nil)
		require.Equal(t, test.status, recorder.Code, test.body)
		require.Equal(t, test.code, errorCode(t, recorder), test.body)
	}
	for len(store.ports) < 30 {
		store.ports = append(store.ports, domain.DevMachinePort{ID: uuid.New(), Port: 4000 + len(store.ports)})
	}
	many := sendPort(t, h, http.MethodPost, `{"port":5000}`, workspaceID, ownerID, machineID, nil)
	require.Equal(t, http.StatusCreated, many.Code, "the number of ports is not limited")

	stranger := sendPort(t, h, http.MethodPost, `{"port":5001}`, workspaceID, uuid.New(), machineID, nil)
	require.Equal(t, http.StatusNotFound, stranger.Code, "other members cannot expose ports")

	id := store.ports[0].ID
	require.Equal(t, http.StatusNoContent, sendPort(t, h, http.MethodDelete, "", workspaceID, ownerID, machineID, &id).Code)
	missing := sendPort(t, h, http.MethodDelete, "", workspaceID, ownerID, machineID, &id)
	require.Equal(t, http.StatusNotFound, missing.Code)
}
