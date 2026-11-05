package handler

import (
	"errors"
	"net/http"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/repository"
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

func (h *AISettingsHandler) GetIssueCopyPrompt(c echo.Context) error {
	ws := c.Get("workspace").(*domain.Workspace)
	settings, err := h.aiSvc.Get(c.Request().Context(), ws.ID)
	if err != nil {
		return response.InternalError(c)
	}
	return response.Success(c, http.StatusOK, dto.IssueCopyPromptResponse{
		IssueCopyPrompt:        settings.IssueCopyPrompt,
		DefaultIssueCopyPrompt: service.DefaultIssueCopyPrompt,
	})
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
	if ok, err := bindExpansionRequest(c, &req); !ok {
		return err
	}
	ws := c.Get("workspace").(*domain.Workspace)
	description, err := h.aiSvc.ExpandIssueDescription(c.Request().Context(), ws.ID, c.Param("identifier"), req.SelectedText)
	return expansionResponse(c, description, err)
}

// ExpandDraftIssueDescription expands the description of an issue that is
// still being created; it never reads stored issues.
func (h *AISettingsHandler) ExpandDraftIssueDescription(c echo.Context) error {
	var req dto.ExpandDraftIssueDescriptionRequest
	if ok, err := bindExpansionRequest(c, &req); !ok {
		return err
	}
	ws := c.Get("workspace").(*domain.Workspace)
	description, err := h.aiSvc.ExpandDraftIssueDescription(c.Request().Context(), ws.ID, req.Title, req.Description, req.SelectedText)
	return expansionResponse(c, description, err)
}

// bindExpansionRequest binds and validates req. When it reports false the
// error response has been written and its result must be returned.
func bindExpansionRequest(c echo.Context, req any) (bool, error) {
	if err := c.Bind(req); err != nil {
		return false, response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid request body")
	}
	if err := validate.Struct(req); err != nil {
		details := make([]dto.ErrorDetail, 0)
		for _, e := range validate.FormatErrors(err) {
			details = append(details, dto.ErrorDetail{Field: e["field"], Message: e["message"]})
		}
		return false, response.ValidationError(c, details)
	}
	return true, nil
}

func expansionResponse(c echo.Context, description string, err error) error {
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrPrivateWorkspaceOperation):
			return response.Error(c, http.StatusForbidden, "PRIVATE_WORKSPACE_OPERATION_DISABLED", "AI expansion is unavailable after private teams are enabled")
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

// CheckVision probes the saved assistant model with a tiny image and reports
// whether it answered and recognised it.
func (h *AISettingsHandler) CheckVision(c echo.Context) error {
	ws := c.Get("workspace").(*domain.Workspace)
	result, err := h.aiSvc.CheckVision(c.Request().Context(), ws.ID, middleware.GetUserID(c))
	switch {
	case errors.Is(err, service.ErrNotWorkspaceOwner):
		return response.Error(c, http.StatusForbidden, "FORBIDDEN", "Only the workspace owner can check AI settings")
	case errors.Is(err, service.ErrWorkspaceNotFound):
		return response.NotFound(c, "Workspace")
	case errors.Is(err, service.ErrAISettingsNotConfigured):
		return response.Error(c, http.StatusBadRequest, "AI_NOT_CONFIGURED", "AI settings are incomplete. Configure a base URL, model, and API key first.")
	case err != nil:
		return response.InternalError(c)
	}
	return response.Success(c, http.StatusOK, result)
}
