package handler

import (
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/kuayle/kuayle-backend/pkg/response"
	"github.com/kuayle/kuayle-backend/pkg/validate"
	"github.com/labstack/echo/v5"
)

// A migration action's command and secret stay small, but JSON escaping can
// grow them; 128 KiB covers the largest allowed pair.
const migrationActionRequestBytes = 128 * 1024

// migrationActionError maps a migration action error to its HTTP response.
// Only typed errors are reported; neither the raw nor the encrypted secret
// ever reaches a message.
func migrationActionError(c *echo.Context, err error) error {
	switch {
	case errors.Is(err, service.ErrInvalidMigrationAction):
		return response.Error(c, http.StatusBadRequest, "INVALID_MIGRATION_ACTION", err.Error())
	case errors.Is(err, repository.ErrConversationInvalid):
		return response.Error(c, http.StatusBadRequest, "INVALID_MIGRATION_ACTION", "Invalid migration action")
	case errors.Is(err, repository.ErrMigrationActionForbidden):
		return response.Forbidden(c)
	case errors.Is(err, repository.ErrMigrationActionNotFound):
		return response.NotFound(c, "Migration action")
	case errors.Is(err, repository.ErrMigrationActionConflict):
		return response.Error(c, http.StatusConflict, "MIGRATION_ACTION_CONFLICT", "A migration action of that name already exists on this machine")
	default:
		return response.InternalError(c)
	}
}

// bindMigrationAction binds and validates a migration action request,
// writing the 400 response and returning false when it is invalid; the
// caller then stops without acting.
func bindMigrationAction(c *echo.Context, input *dto.DevMachineMigrationActionInput) bool {
	c.Request().Body = http.MaxBytesReader(c.Response(), c.Request().Body, migrationActionRequestBytes)
	if err := c.Bind(input); err != nil {
		_ = response.Error(c, http.StatusBadRequest, "INVALID_MIGRATION_ACTION", "Invalid request body")
		return false
	}
	if err := validate.Struct(input); err != nil {
		_ = response.Error(c, http.StatusBadRequest, "INVALID_MIGRATION_ACTION", "Invalid migration action")
		return false
	}
	return true
}

// ListMigrationActions returns a machine's named migration actions; the
// encrypted secret is excluded from every response (json:"-").
func (h *DevMachineHandler) ListMigrationActions(c *echo.Context) error {
	machineID, err := uuid.Parse(c.Param("machineId"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid machine ID")
	}
	actions, err := h.service.ListMigrationActions(c.Request().Context(), middleware.GetWorkspace(c).ID, machineID, middleware.GetUserID(c))
	if err != nil {
		return migrationActionError(c, err)
	}
	return response.Success(c, http.StatusOK, map[string]any{"actions": actions})
}

// CreateMigrationAction stores a named migration action with its encrypted
// connection secret; the secret never appears in the response.
func (h *DevMachineHandler) CreateMigrationAction(c *echo.Context) error {
	var input dto.DevMachineMigrationActionInput
	if !bindMigrationAction(c, &input) {
		return nil
	}
	machineID, err := uuid.Parse(c.Param("machineId"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid machine ID")
	}
	action, err := h.service.CreateMigrationAction(c.Request().Context(), middleware.GetWorkspace(c).ID, machineID, middleware.GetUserID(c), input)
	if err != nil {
		return migrationActionError(c, err)
	}
	return response.Success(c, http.StatusCreated, action)
}

// UpdateMigrationAction changes one action; an omitted secret keeps the
// stored one.
func (h *DevMachineHandler) UpdateMigrationAction(c *echo.Context) error {
	var input dto.DevMachineMigrationActionInput
	if !bindMigrationAction(c, &input) {
		return nil
	}
	machineID, err := uuid.Parse(c.Param("machineId"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid machine ID")
	}
	actionID, err := uuid.Parse(c.Param("actionId"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid migration action ID")
	}
	action, err := h.service.UpdateMigrationAction(c.Request().Context(), middleware.GetWorkspace(c).ID, machineID, actionID, middleware.GetUserID(c), input)
	if err != nil {
		return migrationActionError(c, err)
	}
	return response.Success(c, http.StatusOK, action)
}

// DeleteMigrationAction removes one action; queued runs have already copied
// its command.
func (h *DevMachineHandler) DeleteMigrationAction(c *echo.Context) error {
	machineID, err := uuid.Parse(c.Param("machineId"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid machine ID")
	}
	actionID, err := uuid.Parse(c.Param("actionId"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid migration action ID")
	}
	if err := h.service.DeleteMigrationAction(c.Request().Context(), middleware.GetWorkspace(c).ID, machineID, actionID, middleware.GetUserID(c)); err != nil {
		return migrationActionError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}
