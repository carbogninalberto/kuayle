package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/kuayle/kuayle-backend/pkg/response"
	"github.com/labstack/echo/v5"
)

// AssistantResearchHandler serves Settings → Assistant → Research.
type AssistantResearchHandler struct {
	svc *service.AssistantResearchService
}

func NewAssistantResearchHandler(svc *service.AssistantResearchService) *AssistantResearchHandler {
	return &AssistantResearchHandler{svc: svc}
}

func researchError(c *echo.Context, err error) error {
	switch {
	case errors.Is(err, service.ErrAssistantResearchDenied):
		return response.Forbidden(c)
	case errors.Is(err, service.ErrInvalidResearchSettings):
		return response.Error(c, http.StatusBadRequest, "INVALID_ASSISTANT_RESEARCH_SETTINGS", strings.TrimPrefix(err.Error(), service.ErrInvalidResearchSettings.Error()+": "))
	default:
		return response.InternalError(c)
	}
}

// Get returns the research settings; the search key is only reported as set.
func (h *AssistantResearchHandler) Get(c *echo.Context) error {
	result, err := h.svc.View(c.Request().Context(), middleware.GetWorkspace(c).ID)
	if err != nil {
		return researchError(c, err)
	}
	return response.Success(c, http.StatusOK, result)
}

// Update changes the research settings; only owners and admins may.
func (h *AssistantResearchHandler) Update(c *echo.Context) error {
	var input service.ResearchSettingsUpdate
	decoder := json.NewDecoder(io.LimitReader(c.Request().Body, 8<<10))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil {
		return response.Error(c, http.StatusBadRequest, "INVALID_ASSISTANT_RESEARCH_SETTINGS", "Invalid request body")
	}
	result, err := h.svc.Update(c.Request().Context(), middleware.GetWorkspace(c).ID, input)
	if err != nil {
		return researchError(c, err)
	}
	return response.Success(c, http.StatusOK, result)
}
