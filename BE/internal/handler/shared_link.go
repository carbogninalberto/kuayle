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

type SharedLinkHandler struct {
	sharedLinkSvc *service.SharedLinkService
	frontendURL   string
}

func NewSharedLinkHandler(sharedLinkSvc *service.SharedLinkService, frontendURL string) *SharedLinkHandler {
	return &SharedLinkHandler{sharedLinkSvc: sharedLinkSvc, frontendURL: frontendURL}
}

func (h *SharedLinkHandler) Create(c echo.Context) error {
	var req dto.CreateSharedLinkRequest
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
	userRole := c.Get("workspace_role").(string)

	if !service.CanCreateSharedLink(userRole, ws.ShareLinkMinRole) {
		return response.Forbidden(c)
	}

	link, err := h.sharedLinkSvc.Create(c.Request().Context(), ws.ID, userID, req)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", err.Error())
	}

	return response.Success(c, http.StatusCreated, h.toSharedLinkResponse(*link))
}

func (h *SharedLinkHandler) List(c echo.Context) error {
	ws := c.Get("workspace").(*domain.Workspace)
	userRole := c.Get("workspace_role").(string)

	if !service.CanCreateSharedLink(userRole, ws.ShareLinkMinRole) {
		return response.Forbidden(c)
	}

	links, err := h.sharedLinkSvc.List(c.Request().Context(), ws.ID)
	if err != nil {
		return response.InternalError(c)
	}

	resp := make([]dto.SharedLinkResponse, len(links))
	for i, link := range links {
		resp[i] = h.toSharedLinkResponse(link)
	}

	return response.Success(c, http.StatusOK, resp)
}

func (h *SharedLinkHandler) Update(c echo.Context) error {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid shared link ID")
	}

	var req dto.UpdateSharedLinkRequest
	if err := c.Bind(&req); err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid request body")
	}

	ws := c.Get("workspace").(*domain.Workspace)
	userID := middleware.GetUserID(c)
	userRole := c.Get("workspace_role").(string)

	if !service.CanCreateSharedLink(userRole, ws.ShareLinkMinRole) {
		return response.Forbidden(c)
	}

	link, err := h.sharedLinkSvc.Update(c.Request().Context(), id, userID, req)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", err.Error())
	}

	return response.Success(c, http.StatusOK, h.toSharedLinkResponse(*link))
}

func (h *SharedLinkHandler) Delete(c echo.Context) error {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid shared link ID")
	}

	ws := c.Get("workspace").(*domain.Workspace)
	userRole := c.Get("workspace_role").(string)

	if !service.CanCreateSharedLink(userRole, ws.ShareLinkMinRole) {
		return response.Forbidden(c)
	}

	if err := h.sharedLinkSvc.Delete(c.Request().Context(), id); err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", err.Error())
	}

	return response.Success(c, http.StatusOK, map[string]string{"status": "deleted"})
}

// --- Public endpoints (no auth required) ---

func (h *SharedLinkHandler) GetPublicMeta(c echo.Context) error {
	token := c.Param("token")
	if token == "" {
		return response.NotFound(c, "Share link")
	}

	meta, err := h.sharedLinkSvc.GetPublicMeta(c.Request().Context(), token)
	if err != nil {
		return response.NotFound(c, "Share link")
	}

	return response.Success(c, http.StatusOK, meta)
}

func (h *SharedLinkHandler) ListPublicIssues(c echo.Context) error {
	token := c.Param("token")
	if token == "" {
		return response.NotFound(c, "Share link")
	}

	var params dto.IssueFilterParams
	if err := c.Bind(&params); err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid parameters")
	}

	result, err := h.sharedLinkSvc.ListPublicIssues(c.Request().Context(), token, params)
	if err != nil {
		return response.NotFound(c, "Share link")
	}

	return response.Success(c, http.StatusOK, result)
}

func (h *SharedLinkHandler) toSharedLinkResponse(link domain.SharedLink) dto.SharedLinkResponse {
	resp := dto.SharedLinkResponse{
		ID:                 link.ID.String(),
		Token:              link.Token,
		WorkspaceID:        link.WorkspaceID.String(),
		CreatedBy:          link.CreatedBy.String(),
		Scope:              string(link.Scope),
		Filters:            link.Filters,
		IncludeDescription: link.IncludeDescription,
		IsActive:           link.IsActive,
		ExpiresAt:          link.ExpiresAt,
		URL:                h.frontendURL + "/share/" + link.Token,
		CreatedAt:          link.CreatedAt,
		UpdatedAt:          link.UpdatedAt,
	}
	if link.ScopeID != nil {
		s := link.ScopeID.String()
		resp.ScopeID = &s
	}
	return resp
}
