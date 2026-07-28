package handler

import (
	"errors"
	"net/http"
	"strings"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/kuayle/kuayle-backend/pkg/response"
	"github.com/kuayle/kuayle-backend/pkg/validate"
	"github.com/labstack/echo/v4"
)

type InviteLinkHandler struct {
	inviteLinkSvc *service.InviteLinkService
	frontendURL   string
}

func NewInviteLinkHandler(inviteLinkSvc *service.InviteLinkService, frontendURL string) *InviteLinkHandler {
	return &InviteLinkHandler{inviteLinkSvc: inviteLinkSvc, frontendURL: strings.TrimRight(frontendURL, "/")}
}

// Create handles POST /api/workspaces/:slug/invite-links
func (h *InviteLinkHandler) Create(c echo.Context) error {
	var req dto.CreateInviteLinkRequest
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

	link, rawToken, err := h.inviteLinkSvc.Create(c.Request().Context(), ws.ID, userID, req)
	if err != nil {
		return response.InternalError(c)
	}

	resp := toInviteLinkResponse(link)
	resp.InviteURL = h.frontendURL + "/invite/" + rawToken
	return response.Success(c, http.StatusCreated, resp)
}

// List handles GET /api/workspaces/:slug/invite-links
func (h *InviteLinkHandler) List(c echo.Context) error {
	ws := c.Get("workspace").(*domain.Workspace)
	links, err := h.inviteLinkSvc.List(c.Request().Context(), ws.ID)
	if err != nil {
		return response.InternalError(c)
	}

	resp := make([]dto.InviteLinkResponse, 0, len(links))
	for i := range links {
		resp = append(resp, toInviteLinkResponse(&links[i]))
	}
	return response.Success(c, http.StatusOK, resp)
}

// Revoke handles DELETE /api/workspaces/:slug/invite-links/:id
func (h *InviteLinkHandler) Revoke(c echo.Context) error {
	ws := c.Get("workspace").(*domain.Workspace)
	linkID, err := parseUUID(c.Param("id"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid invite link ID")
	}

	userID := middleware.GetUserID(c)
	if err := h.inviteLinkSvc.Revoke(c.Request().Context(), ws.ID, linkID, userID); err != nil {
		if errors.Is(err, service.ErrInviteLinkInvalid) {
			return response.NotFound(c, "Invite link")
		}
		return response.InternalError(c)
	}
	return response.Success(c, http.StatusOK, map[string]string{"status": "revoked"})
}

// Preview handles GET /api/invite/:token (public, no auth)
func (h *InviteLinkHandler) Preview(c echo.Context) error {
	preview, err := h.inviteLinkSvc.Preview(c.Request().Context(), c.Param("token"))
	if err != nil {
		if errors.Is(err, service.ErrInviteLinkInvalid) {
			return response.NotFound(c, "Invite link")
		}
		return response.InternalError(c)
	}
	return response.Success(c, http.StatusOK, preview)
}

// Accept handles POST /api/invite/:token/accept (requires auth)
func (h *InviteLinkHandler) Accept(c echo.Context) error {
	userID := middleware.GetUserID(c)
	ws, role, err := h.inviteLinkSvc.Accept(c.Request().Context(), c.Param("token"), userID)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInviteLinkInvalid):
			return response.NotFound(c, "Invite link")
		case errors.Is(err, service.ErrInviteLinkExpired):
			return response.Error(c, http.StatusGone, "INVITE_EXPIRED", "This invite link has expired")
		case errors.Is(err, service.ErrInviteLinkRevoked):
			return response.Error(c, http.StatusGone, "INVITE_REVOKED", "This invite link has been revoked")
		case errors.Is(err, service.ErrInviteLinkExhausted):
			return response.Error(c, http.StatusGone, "INVITE_EXHAUSTED", "This invite link has reached its maximum uses")
		default:
			return response.InternalError(c)
		}
	}
	return response.Success(c, http.StatusOK, dto.AcceptInviteResponse{
		WorkspaceID:   ws.ID.String(),
		WorkspaceName: ws.Name,
		WorkspaceSlug: ws.Slug,
		Role:          role,
	})
}

func toInviteLinkResponse(link *domain.WorkspaceInviteLink) dto.InviteLinkResponse {
	return dto.InviteLinkResponse{
		ID:        link.ID.String(),
		Role:      link.Role,
		CreatedBy: link.CreatedBy.String(),
		ExpiresAt: link.ExpiresAt,
		MaxUses:   link.MaxUses,
		UseCount:  link.UseCount,
		RevokedAt: link.RevokedAt,
		CreatedAt: link.CreatedAt,
	}
}
