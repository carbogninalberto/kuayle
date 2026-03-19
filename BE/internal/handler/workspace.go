package handler

import (
	"net/http"

	"github.com/carbon/carbon-backend/internal/domain"
	"github.com/carbon/carbon-backend/internal/dto"
	"github.com/carbon/carbon-backend/internal/middleware"
	"github.com/carbon/carbon-backend/internal/service"
	"github.com/carbon/carbon-backend/pkg/response"
	"github.com/carbon/carbon-backend/pkg/validate"
	"github.com/labstack/echo/v4"
)

type WorkspaceHandler struct {
	workspaceSvc *service.WorkspaceService
}

func NewWorkspaceHandler(workspaceSvc *service.WorkspaceService) *WorkspaceHandler {
	return &WorkspaceHandler{workspaceSvc: workspaceSvc}
}

func (h *WorkspaceHandler) List(c echo.Context) error {
	userID := middleware.GetUserID(c)
	workspaces, err := h.workspaceSvc.ListByUser(c.Request().Context(), userID)
	if err != nil {
		return response.InternalError(c)
	}
	resp := make([]dto.WorkspaceResponse, len(workspaces))
	for i, ws := range workspaces {
		resp[i] = toWorkspaceResponse(ws)
	}
	return response.Success(c, http.StatusOK, resp)
}

func (h *WorkspaceHandler) Create(c echo.Context) error {
	var req dto.CreateWorkspaceRequest
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

	userID := middleware.GetUserID(c)
	ws, err := h.workspaceSvc.Create(c.Request().Context(), userID, req)
	if err != nil {
		return response.Error(c, http.StatusConflict, "CONFLICT", err.Error())
	}
	return response.Success(c, http.StatusCreated, toWorkspaceResponse(*ws))
}

func (h *WorkspaceHandler) Get(c echo.Context) error {
	ws := c.Get("workspace").(*domain.Workspace)
	return response.Success(c, http.StatusOK, toWorkspaceResponse(*ws))
}

func (h *WorkspaceHandler) Update(c echo.Context) error {
	var req dto.UpdateWorkspaceRequest
	if err := c.Bind(&req); err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid request body")
	}

	slug := c.Param("slug")
	ws, err := h.workspaceSvc.Update(c.Request().Context(), slug, req)
	if err != nil {
		return response.InternalError(c)
	}
	return response.Success(c, http.StatusOK, toWorkspaceResponse(*ws))
}

func (h *WorkspaceHandler) Invite(c echo.Context) error {
	var req dto.InviteMemberRequest
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

	if err := h.workspaceSvc.InviteMember(c.Request().Context(), ws.ID, req); err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", err.Error())
	}
	return response.Success(c, http.StatusOK, map[string]string{"status": "invited"})
}

func (h *WorkspaceHandler) ListMembers(c echo.Context) error {
	ws := c.Get("workspace").(*domain.Workspace)
	members, err := h.workspaceSvc.ListMembersWithUsers(c.Request().Context(), ws.ID)
	if err != nil {
		return response.InternalError(c)
	}

	resp := make([]dto.WorkspaceMemberResponse, 0, len(members))
	for _, m := range members {
		resp = append(resp, dto.WorkspaceMemberResponse{
			UserID:    m.UserID.String(),
			Email:     m.Email,
			Name:      m.Name,
			Role:      m.Role,
			CreatedAt: m.CreatedAt,
		})
	}
	return response.Success(c, http.StatusOK, resp)
}

func toWorkspaceResponse(ws domain.Workspace) dto.WorkspaceResponse {
	return dto.WorkspaceResponse{
		ID:        ws.ID.String(),
		Name:      ws.Name,
		Slug:      ws.Slug,
		LogoURL:   ws.LogoURL,
		CreatedAt: ws.CreatedAt,
		UpdatedAt: ws.UpdatedAt,
	}
}
