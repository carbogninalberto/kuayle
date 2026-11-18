package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/agent"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"
)

func TestCodexDeviceEndpointsRejectGuestsAndInvalidAttempts(t *testing.T) {
	// No store or encryption key so none of these cases requires a machine,
	// writes credentials, or starts a real Codex process.
	h := NewDevMachineHandler(service.NewDevMachineService(nil, agent.NewRegistry(), true, "", nil, time.Minute, service.DevMachineImages{}))
	workspace, user := uuid.New(), uuid.New()
	for _, tc := range []struct {
		name, role, body, code string
		handler                echo.HandlerFunc
		status                 int
	}{
		{"guest cannot start", domain.RoleGuest, "", "FORBIDDEN", h.StartCodexDeviceLogin, http.StatusForbidden},
		{"guest cannot poll", domain.RoleGuest, "", "FORBIDDEN", h.PollCodexDeviceLogin, http.StatusForbidden},
		{"guest cannot cancel", domain.RoleGuest, "", "FORBIDDEN", h.CancelCodexDeviceLogin, http.StatusForbidden},
		{"unavailable", domain.RoleMember, "", "CODEX_DEVICE_UNAVAILABLE", h.StartCodexDeviceLogin, http.StatusServiceUnavailable},
		{"missing attempt ID", domain.RoleMember, `{}`, "BAD_REQUEST", h.PollCodexDeviceLogin, http.StatusBadRequest},
		{"invalid JSON", domain.RoleMember, `{"attempt_id":`, "BAD_REQUEST", h.CancelCodexDeviceLogin, http.StatusBadRequest},
		{"unknown attempt", domain.RoleMember, `{"attempt_id":"` + uuid.NewString() + `"}`, "NOT_FOUND", h.PollCodexDeviceLogin, http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/agent-sign-ins/codex/device", strings.NewReader(tc.body))
			request.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			ctx := devMachineHandlerContext(request, recorder, workspace, user)
			ctx.Set("workspace_role", tc.role)
			require.NoError(t, tc.handler(ctx))
			require.Equal(t, tc.status, recorder.Code, recorder.Body.String())
			require.Contains(t, recorder.Body.String(), tc.code)
			require.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
		})
	}
}
