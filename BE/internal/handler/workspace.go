package handler

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/kuayle/kuayle-backend/pkg/response"
	"github.com/kuayle/kuayle-backend/pkg/validate"
	"github.com/labstack/echo/v4"
)

func parseUUID(s string) (uuid.UUID, error) {
	return uuid.Parse(s)
}

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
		resp[i] = h.toWorkspaceResponse(c, ws)
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
	return response.Success(c, http.StatusCreated, h.toWorkspaceResponse(c, *ws))
}

func (h *WorkspaceHandler) Get(c echo.Context) error {
	ws := c.Get("workspace").(*domain.Workspace)
	return response.Success(c, http.StatusOK, h.toWorkspaceResponse(c, *ws))
}

func (h *WorkspaceHandler) Update(c echo.Context) error {
	var req dto.UpdateWorkspaceRequest
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
	if req.LogoURL.Set && req.LogoURL.Value != nil && strings.TrimSpace(*req.LogoURL.Value) != "" {
		if err := validateLogoURL(*req.LogoURL.Value); err != nil {
			return response.ValidationError(c, []dto.ErrorDetail{{Field: "LogoURL", Message: err.Error()}})
		}
	}

	slug := c.Param("slug")
	userID := middleware.GetUserID(c)
	ws, err := h.workspaceSvc.Update(c.Request().Context(), slug, userID, req)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrWorkspaceNotFound):
			return response.NotFound(c, "Workspace")
		case errors.Is(err, service.ErrNotWorkspaceOwner):
			return response.Error(c, http.StatusForbidden, "FORBIDDEN", "Only the workspace owner can edit workspace settings")
		default:
			return response.InternalError(c)
		}
	}
	return response.Success(c, http.StatusOK, h.toWorkspaceResponse(c, *ws))
}

func (h *WorkspaceHandler) Delete(c echo.Context) error {
	slug := c.Param("slug")
	userID := middleware.GetUserID(c)
	if err := h.workspaceSvc.Delete(c.Request().Context(), slug, userID); err != nil {
		switch {
		case errors.Is(err, service.ErrWorkspaceNotFound):
			return response.NotFound(c, "Workspace")
		case errors.Is(err, service.ErrNotWorkspaceOwner):
			return response.Error(c, http.StatusForbidden, "FORBIDDEN", "Only the workspace owner can delete this workspace")
		case errors.Is(err, service.ErrWorkspaceHasDevMachineRuntimes):
			return response.Error(c, http.StatusConflict, "WORKSPACE_HAS_DEV_MACHINES", "Destroy all dev machines before deleting this workspace")
		case errors.Is(err, service.ErrWorkspaceEnvironmentCleanupPending):
			return response.Error(c, http.StatusConflict, "WORKSPACE_CLEANUP_PENDING", "Development environment image cleanup is in progress; retry workspace deletion shortly")
		default:
			return response.InternalError(c)
		}
	}
	return response.Success(c, http.StatusOK, map[string]string{"status": "deleted"})
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

func (h *WorkspaceHandler) UpdateMemberRole(c echo.Context) error {
	var req dto.UpdateMemberRoleRequest
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
	userIDStr := c.Param("userId")
	userID, err := parseUUID(userIDStr)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid user ID")
	}

	if err := h.workspaceSvc.UpdateMemberRole(c.Request().Context(), ws.ID, userID, req.Role); err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", err.Error())
	}
	return response.Success(c, http.StatusOK, map[string]string{"status": "updated"})
}

func (h *WorkspaceHandler) RemoveMember(c echo.Context) error {
	ws := c.Get("workspace").(*domain.Workspace)
	userIDStr := c.Param("userId")
	userID, err := parseUUID(userIDStr)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid user ID")
	}

	if err := h.workspaceSvc.RemoveMember(c.Request().Context(), ws.ID, userID); err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", err.Error())
	}
	return response.Success(c, http.StatusOK, map[string]string{"status": "removed"})
}

func validateLogoURL(rawURL string) error {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Hostname() == "" {
		return errors.New("must be a valid URL")
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return errors.New("must be an http or https URL")
	}
	return nil
}

// toWorkspaceResponse builds a WorkspaceResponse, attaching the requesting user's
// role (if available) and the workspace owner's profile (best-effort).
func (h *WorkspaceHandler) toWorkspaceResponse(c echo.Context, ws domain.Workspace) dto.WorkspaceResponse {
	resp := dto.WorkspaceResponse{
		ID:               ws.ID.String(),
		Name:             ws.Name,
		Slug:             ws.Slug,
		LogoURL:          ws.LogoURL,
		OwnerID:          ws.OwnerID.String(),
		ShareLinkMinRole: ws.ShareLinkMinRole,
		CurrentUserRole:  middleware.GetWorkspaceRole(c),
		CreatedAt:        ws.CreatedAt,
		UpdatedAt:        ws.UpdatedAt,
	}

	if ws.OwnerID != uuid.Nil {
		if owner, err := h.workspaceSvc.GetOwner(c.Request().Context(), &ws); err == nil && owner != nil {
			avatar := owner.AvatarURL
			resp.Owner = &dto.WorkspaceOwnerResponse{
				ID:        owner.ID.String(),
				Email:     owner.Email,
				Name:      owner.Name,
				AvatarURL: avatar,
			}
		}
	}

	return resp
}
