package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"
)

func (f *handlerDevMachineStoreFake) ListAgentModelDefaults(context.Context, uuid.UUID) ([]domain.AgentModelDefault, error) {
	return append([]domain.AgentModelDefault(nil), f.agentModelDefaults...), nil
}

func (f *handlerDevMachineStoreFake) SaveAgentModelDefault(_ context.Context, value *domain.AgentModelDefault) error {
	f.agentModelDefaults = []domain.AgentModelDefault{*value}
	return nil
}

func sendAgentModelDefault(t *testing.T, h *DevMachineHandler, method, provider, body, role string, workspaceID, userID uuid.UUID) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, "/agent-model-defaults/"+provider, strings.NewReader(body))
	request.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	ctx := devMachineHandlerContext(request, recorder, workspaceID, userID)
	ctx.Set("workspace_role", role)
	if method == http.MethodGet {
		require.NoError(t, h.ListAgentModelDefaults(ctx))
	} else {
		ctx.SetPathValues(echo.PathValues{{Name: "provider", Value: provider}})
		require.NoError(t, h.SaveAgentModelDefault(ctx))
	}
	return recorder
}

func TestOnlyOwnersAndAdminsSetTheWorkspaceAgentModelDefault(t *testing.T) {
	workspaceID, userID := uuid.New(), uuid.New()
	store := &handlerDevMachineStoreFake{}
	h := modelServerHandler(store)
	body := `{"model":"claude-opus-5-5","thinking":"medium"}`

	for _, role := range []string{domain.RoleMember, domain.RoleGuest} {
		denied := sendAgentModelDefault(t, h, http.MethodPut, "claude-code", body, role, workspaceID, userID)
		require.Equal(t, http.StatusForbidden, denied.Code, role)
	}
	require.Empty(t, store.agentModelDefaults)

	for _, role := range []string{domain.RoleOwner, domain.RoleAdmin} {
		saved := sendAgentModelDefault(t, h, http.MethodPut, "claude-code", body, role, workspaceID, userID)
		require.Equal(t, http.StatusOK, saved.Code, saved.Body.String())
	}
	require.Equal(t, "medium", store.agentModelDefaults[0].Thinking)

	invalid := sendAgentModelDefault(t, h, http.MethodPut, "claude-code", `{"model":"claude-opus-5-5","thinking":"ultra"}`, domain.RoleOwner, workspaceID, userID)
	require.Equal(t, http.StatusBadRequest, invalid.Code)

	// Members read the defaults their tasks run with.
	listed := sendAgentModelDefault(t, h, http.MethodGet, "", "", domain.RoleMember, workspaceID, userID)
	require.Equal(t, http.StatusOK, listed.Code)
	var defaults []dto.AgentModelDefaultResponse
	require.NoError(t, json.Unmarshal(listed.Body.Bytes(), &defaults))
	require.Len(t, defaults, 2)
	require.Equal(t, "claude-opus-5-5", defaults[1].Model)
}
