package handler

import (
	"net/http"

	"github.com/carbon/carbon-backend/internal/domain"
	"github.com/carbon/carbon-backend/internal/dto"
	"github.com/carbon/carbon-backend/internal/service"
	"github.com/carbon/carbon-backend/pkg/response"
	"github.com/carbon/carbon-backend/pkg/validate"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

type ProjectHandler struct {
	projectSvc *service.ProjectService
}

func NewProjectHandler(projectSvc *service.ProjectService) *ProjectHandler {
	return &ProjectHandler{projectSvc: projectSvc}
}

func (h *ProjectHandler) List(c echo.Context) error {
	ws := c.Get("workspace").(*domain.Workspace)
	projects, err := h.projectSvc.ListByWorkspace(c.Request().Context(), ws.ID)
	if err != nil {
		return response.InternalError(c)
	}
	resp := make([]dto.ProjectResponse, len(projects))
	for i, p := range projects {
		r := toProjectResponse(p)
		stats, _ := h.projectSvc.GetStats(c.Request().Context(), p.ID)
		if stats != nil {
			r.Progress = stats
		}
		resp[i] = r
	}
	return response.Success(c, http.StatusOK, resp)
}

func (h *ProjectHandler) Create(c echo.Context) error {
	var req dto.CreateProjectRequest
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
	project, err := h.projectSvc.Create(c.Request().Context(), ws.ID, req)
	if err != nil {
		return response.InternalError(c)
	}
	return response.Success(c, http.StatusCreated, toProjectResponse(*project))
}

func (h *ProjectHandler) Get(c echo.Context) error {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid project ID")
	}
	project, err := h.projectSvc.GetByID(c.Request().Context(), id)
	if err != nil || project == nil {
		return response.NotFound(c, "Project")
	}
	r := toProjectResponse(*project)
	stats, _ := h.projectSvc.GetStats(c.Request().Context(), project.ID)
	if stats != nil {
		r.Progress = stats
	}
	return response.Success(c, http.StatusOK, r)
}

func (h *ProjectHandler) Update(c echo.Context) error {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid project ID")
	}
	var req dto.UpdateProjectRequest
	if err := c.Bind(&req); err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid request body")
	}
	project, err := h.projectSvc.Update(c.Request().Context(), id, req)
	if err != nil {
		return response.InternalError(c)
	}
	return response.Success(c, http.StatusOK, toProjectResponse(*project))
}

func (h *ProjectHandler) Delete(c echo.Context) error {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid project ID")
	}
	if err := h.projectSvc.Delete(c.Request().Context(), id); err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", err.Error())
	}
	return response.Success(c, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *ProjectHandler) ListByTeam(c echo.Context) error {
	teamID, err := uuid.Parse(c.Param("teamId"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid team ID")
	}
	projects, err := h.projectSvc.ListByTeam(c.Request().Context(), teamID)
	if err != nil {
		return response.InternalError(c)
	}
	resp := make([]dto.ProjectResponse, len(projects))
	for i, p := range projects {
		r := toProjectResponse(p)
		stats, _ := h.projectSvc.GetStats(c.Request().Context(), p.ID)
		if stats != nil {
			r.Progress = stats
		}
		resp[i] = r
	}
	return response.Success(c, http.StatusOK, resp)
}

func toProjectResponse(p domain.Project) dto.ProjectResponse {
	resp := dto.ProjectResponse{
		ID:          p.ID.String(),
		Name:        p.Name,
		Description: p.Description,
		Status:      string(p.Status),
		SortOrder:   p.SortOrder,
		CreatedAt:   p.CreatedAt,
		UpdatedAt:   p.UpdatedAt,
	}
	if p.TeamID != nil {
		s := p.TeamID.String()
		resp.TeamID = &s
	}
	if p.LeadID != nil {
		s := p.LeadID.String()
		resp.LeadID = &s
	}
	resp.StartDate = p.StartDate
	resp.TargetDate = p.TargetDate
	return resp
}
