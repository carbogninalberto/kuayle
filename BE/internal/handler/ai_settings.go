package handler

import (
	"errors"
	"net/http"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/kuayle/kuayle-backend/pkg/response"
	"github.com/kuayle/kuayle-backend/pkg/validate"
	"github.com/labstack/echo/v4"
)

type AISettingsHandler struct {
	aiSvc *service.AISettingsService
}

func NewAISettingsHandler(aiSvc *service.AISettingsService) *AISettingsHandler {
	return &AISettingsHandler{aiSvc: aiSvc}
}

func (h *AISettingsHandler) Get(c echo.Context) error {
	ws := c.Get("workspace").(*domain.Workspace)
	settings, err := h.aiSvc.Get(c.Request().Context(), ws.ID)
	if err != nil {
		return response.InternalError(c)
	}
	return response.Success(c, http.StatusOK, service.ToAISettingsResponse(settings))
}

func (h *AISettingsHandler) Update(c echo.Context) error {
	var req dto.UpdateAISettingsRequest
	if err := c.Bind(&req); err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid request body")
	}
	if err := validate.Struct(&req); err != nil {
		details := make([]dto.ErrorDetail, 0)
		for _, e := range validate.FormatErrors(err) {
			details = append(details, dto.ErrorDetail{Field: e["field"], Message: e["message"]})
		}
		return response.ValidationError(c, details)
	}

	ws := c.Get("workspace").(*domain.Workspace)
	userID := middleware.GetUserID(c)
	settings, err := h.aiSvc.Update(c.Request().Context(), ws.ID, userID, req)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrNotWorkspaceOwner):
			return response.Error(c, http.StatusForbidden, "FORBIDDEN", "Only the workspace owner can edit AI settings")
		case errors.Is(err, service.ErrWorkspaceNotFound):
			return response.NotFound(c, "Workspace")
		default:
			return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		}
	}
	return response.Success(c, http.StatusOK, service.ToAISettingsResponse(settings))
}

func (h *AISettingsHandler) ExpandIssueDescription(c echo.Context) error {
	var req dto.ExpandIssueDescriptionRequest
	if err := c.Bind(&req); err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid request body")
	}
	if err := validate.Struct(&req); err != nil {
		details := make([]dto.ErrorDetail, 0)
		for _, e := range validate.FormatErrors(err) {
			details = append(details, dto.ErrorDetail{Field: e["field"], Message: e["message"]})
		}
		return response.ValidationError(c, details)
	}

	ws := c.Get("workspace").(*domain.Workspace)
	description, err := h.aiSvc.ExpandIssueDescription(c.Request().Context(), ws.ID, c.Param("identifier"), req.SelectedText)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrIssueNotFound):
			return response.NotFound(c, "Issue")
		case errors.Is(err, service.ErrAISettingsNotConfigured):
			return response.Error(c, http.StatusBadRequest, "AI_NOT_CONFIGURED", "AI settings are incomplete. Configure a base URL, model, and API key first.")
		case errors.Is(err, service.ErrAIProviderRequestFailed):
			return response.Error(c, http.StatusBadGateway, "AI_PROVIDER_ERROR", err.Error())
		default:
			return response.InternalError(c)
		}
	}
	return response.Success(c, http.StatusOK, dto.ExpandIssueDescriptionResponse{Description: description})
}
