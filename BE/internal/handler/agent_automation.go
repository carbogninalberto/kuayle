package handler

import (
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/kuayle/kuayle-backend/pkg/response"
	"github.com/labstack/echo/v4"
)

// AgentAutomationHandler manages delegation rules. Rules are workspace
// administration; a pending run may also be decided by the rule's owner.
type AgentAutomationHandler struct {
	svc *service.AgentAutomationService
}

func NewAgentAutomationHandler(svc *service.AgentAutomationService) *AgentAutomationHandler {
	return &AgentAutomationHandler{svc}
}

func automationError(c echo.Context, err error) error {
	switch {
	case errors.Is(err, repository.ErrAutomationNotFound):
		return response.NotFound(c, "Automation")
	case errors.Is(err, service.ErrInvalidAutomation), errors.Is(err, repository.ErrConversationInvalid):
		return response.Error(c, http.StatusBadRequest, "INVALID_AUTOMATION", "Invalid automation")
	default:
		return machineError(c, err)
	}
}

func automationID(c echo.Context) (uuid.UUID, error) {
	id, err := uuid.Parse(c.Param("automationId"))
	if err != nil {
		return uuid.Nil, repository.ErrAutomationNotFound
	}
	return id, nil
}

func (h *AgentAutomationHandler) List(c echo.Context) error {
	if !canAdministerDevMachines(c) {
		return response.Forbidden(c)
	}
	automations, err := h.svc.List(c.Request().Context(), middleware.GetWorkspace(c).ID)
	if err != nil {
		return automationError(c, err)
	}
	return response.Success(c, http.StatusOK, automations)
}

func (h *AgentAutomationHandler) Create(c echo.Context) error {
	if !canAdministerDevMachines(c) {
		return response.Forbidden(c)
	}
	var input service.AutomationInput
	if err := bindTask(c, &input); err != nil {
		return automationError(c, err)
	}
	automation, err := h.svc.Create(c.Request().Context(), middleware.GetWorkspace(c).ID, middleware.GetUserID(c), input)
	if err != nil {
		return automationError(c, err)
	}
	return response.Success(c, http.StatusCreated, automation)
}

func (h *AgentAutomationHandler) Update(c echo.Context) error {
	if !canAdministerDevMachines(c) {
		return response.Forbidden(c)
	}
	id, err := automationID(c)
	if err != nil {
		return automationError(c, err)
	}
	var input struct {
		Enabled bool `json:"enabled"`
	}
	if err := bindTask(c, &input); err != nil {
		return automationError(c, err)
	}
	automation, err := h.svc.SetEnabled(c.Request().Context(), middleware.GetWorkspace(c).ID, id, input.Enabled)
	if err != nil {
		return automationError(c, err)
	}
	return response.Success(c, http.StatusOK, automation)
}

func (h *AgentAutomationHandler) Delete(c echo.Context) error {
	if !canAdministerDevMachines(c) {
		return response.Forbidden(c)
	}
	id, err := automationID(c)
	if err != nil {
		return automationError(c, err)
	}
	if err := h.svc.Delete(c.Request().Context(), middleware.GetWorkspace(c).ID, id); err != nil {
		return automationError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *AgentAutomationHandler) Runs(c echo.Context) error {
	if !canAdministerDevMachines(c) {
		return response.Forbidden(c)
	}
	runs, err := h.svc.Runs(c.Request().Context(), middleware.GetWorkspace(c).ID)
	if err != nil {
		return automationError(c, err)
	}
	return response.Success(c, http.StatusOK, runs)
}

func (h *AgentAutomationHandler) decide(c echo.Context, approve bool) error {
	id, err := uuid.Parse(c.Param("runId"))
	if err != nil {
		return automationError(c, repository.ErrAutomationNotFound)
	}
	run, err := h.svc.Decide(c.Request().Context(), middleware.GetWorkspace(c).ID, id, middleware.GetUserID(c), canAdministerDevMachines(c), approve)
	if err != nil {
		return automationError(c, err)
	}
	return response.Success(c, http.StatusOK, run)
}

func (h *AgentAutomationHandler) Approve(c echo.Context) error { return h.decide(c, true) }
func (h *AgentAutomationHandler) Dismiss(c echo.Context) error { return h.decide(c, false) }
