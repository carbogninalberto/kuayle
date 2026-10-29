package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/kuayle/kuayle-backend/pkg/response"
	"github.com/labstack/echo/v4"
)

func capabilityError(c echo.Context, err error) error {
	switch {
	case errors.Is(err, service.ErrAssistantCapabilitiesDenied), errors.Is(err, repository.ErrAssistantToolSettingDenied):
		return response.Forbidden(c)
	case errors.Is(err, service.ErrUnknownAssistantTool):
		return response.NotFound(c, "Assistant tool")
	case errors.Is(err, service.ErrAssistantCapabilitiesUnavailable):
		return response.Error(c, http.StatusServiceUnavailable, "ASSISTANT_TOOLS_UNAVAILABLE", "Assistant tools are not available")
	default:
		return response.InternalError(c)
	}
}

// Capabilities lists the assistant's built-in tools for workspace members.
func (h *ConversationHandler) Capabilities(c echo.Context) error {
	result, err := h.svc.Capabilities(c.Request().Context(), middleware.GetWorkspace(c).ID)
	if err != nil {
		return capabilityError(c, err)
	}
	return response.Success(c, http.StatusOK, result)
}

// SetCapability turns one tool on or off; the service and SQL allow only
// current owners and admins.
func (h *ConversationHandler) SetCapability(c echo.Context) error {
	var input struct {
		Enabled *bool `json:"enabled"`
	}
	decoder := json.NewDecoder(io.LimitReader(c.Request().Body, 1024))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || input.Enabled == nil {
		return response.Error(c, http.StatusBadRequest, "INVALID_ASSISTANT_TOOL_SETTING", "enabled must be true or false")
	}
	result, err := h.svc.SetCapability(c.Request().Context(), middleware.GetWorkspace(c).ID, c.Param("name"), *input.Enabled)
	if err != nil {
		return capabilityError(c, err)
	}
	return response.Success(c, http.StatusOK, result)
}
