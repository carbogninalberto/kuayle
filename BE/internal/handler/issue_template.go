package handler

import (
	"net/http"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/kuayle/kuayle-backend/pkg/response"
	"github.com/kuayle/kuayle-backend/pkg/validate"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

type IssueTemplateHandler struct {
	templateSvc *service.IssueTemplateService
}

func NewIssueTemplateHandler(templateSvc *service.IssueTemplateService) *IssueTemplateHandler {
	return &IssueTemplateHandler{templateSvc: templateSvc}
}

func (h *IssueTemplateHandler) Create(c echo.Context) error {
	var req dto.CreateIssueTemplateRequest
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

	tmpl, err := h.templateSvc.Create(c.Request().Context(), ws.ID, userID, req)
	if err != nil {
		return response.InternalError(c)
	}

	return response.Success(c, http.StatusCreated, toIssueTemplateResponse(tmpl))
}

func (h *IssueTemplateHandler) List(c echo.Context) error {
	ws := c.Get("workspace").(*domain.Workspace)

	templates, err := h.templateSvc.ListByWorkspace(c.Request().Context(), ws.ID)
	if err != nil {
		return response.InternalError(c)
	}

	resp := make([]dto.IssueTemplateResponse, len(templates))
	for i, tmpl := range templates {
		resp[i] = toIssueTemplateResponse(&tmpl)
	}

	return response.Success(c, http.StatusOK, resp)
}

func (h *IssueTemplateHandler) Get(c echo.Context) error {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid template ID")
	}

	tmpl, err := h.templateSvc.GetByID(c.Request().Context(), id)
	if err != nil || tmpl == nil {
		return response.NotFound(c, "Issue template")
	}

	return response.Success(c, http.StatusOK, toIssueTemplateResponse(tmpl))
}

func (h *IssueTemplateHandler) Update(c echo.Context) error {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid template ID")
	}

	var req dto.UpdateIssueTemplateRequest
	if err := c.Bind(&req); err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid request body")
	}

	tmpl, err := h.templateSvc.Update(c.Request().Context(), id, req)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", err.Error())
	}

	return response.Success(c, http.StatusOK, toIssueTemplateResponse(tmpl))
}

func (h *IssueTemplateHandler) Delete(c echo.Context) error {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid template ID")
	}

	if err := h.templateSvc.Delete(c.Request().Context(), id); err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", err.Error())
	}

	return response.Success(c, http.StatusOK, map[string]string{"status": "deleted"})
}

func toIssueTemplateResponse(tmpl *domain.IssueTemplate) dto.IssueTemplateResponse {
	resp := dto.IssueTemplateResponse{
		ID:             tmpl.ID.String(),
		WorkspaceID:    tmpl.WorkspaceID.String(),
		Title:          tmpl.Title,
		Description:    tmpl.Description,
		Status:         tmpl.Status,
		Priority:       tmpl.Priority,
		LabelIDs:       tmpl.LabelIDs,
		RecurrenceRule: tmpl.RecurrenceRule,
		NextRunAt:      tmpl.NextRunAt,
		IsActive:       tmpl.IsActive,
		CreatedBy:      tmpl.CreatedBy.String(),
		CreatedAt:      tmpl.CreatedAt,
		UpdatedAt:      tmpl.UpdatedAt,
	}
	if tmpl.TeamID != nil {
		s := tmpl.TeamID.String()
		resp.TeamID = &s
	}
	if tmpl.AssigneeID != nil {
		s := tmpl.AssigneeID.String()
		resp.AssigneeID = &s
	}
	return resp
}
