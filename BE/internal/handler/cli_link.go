package handler

import (
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/kuayle/kuayle-backend/pkg/response"
	"github.com/kuayle/kuayle-backend/pkg/validate"
	"github.com/labstack/echo/v5"
)

type CLILinkHandler struct {
	linkSvc *service.CLILinkService
}

func NewCLILinkHandler(linkSvc *service.CLILinkService) *CLILinkHandler {
	return &CLILinkHandler{linkSvc: linkSvc}
}

// Create mints a connection link. Link management is session-only; the
// handler repeats the check so a direct call cannot bypass the route.
func (h *CLILinkHandler) Create(c *echo.Context) error {
	if c.Get(middleware.TokenScopesKey) != nil {
		return response.Forbidden(c)
	}
	var req dto.CreateCLILinkRequest
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

	link, linkURL, err := h.linkSvc.Create(c.Request().Context(), middleware.GetUserID(c), req)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidTokenScope):
			return response.ValidationError(c, []dto.ErrorDetail{{Field: "scopes", Message: err.Error()}})
		case errors.Is(err, service.ErrInvalidTokenWorkspace):
			return response.ValidationError(c, []dto.ErrorDetail{{Field: "workspace_slugs", Message: err.Error()}})
		case errors.Is(err, service.ErrCLILinkInvalidTokenTime):
			return response.ValidationError(c, []dto.ErrorDetail{{Field: "token_expires_at", Message: err.Error()}})
		}
		return response.InternalError(c)
	}
	return response.Success(c, http.StatusCreated, toCLILinkResponse(*link, linkURL))
}

func (h *CLILinkHandler) List(c *echo.Context) error {
	if c.Get(middleware.TokenScopesKey) != nil {
		return response.Forbidden(c)
	}
	links, err := h.linkSvc.List(c.Request().Context(), middleware.GetUserID(c))
	if err != nil {
		return response.InternalError(c)
	}
	resp := make([]dto.CLILinkResponse, len(links))
	for i, link := range links {
		resp[i] = toCLILinkResponse(link, "")
	}
	return response.Success(c, http.StatusOK, resp)
}

func (h *CLILinkHandler) Revoke(c *echo.Context) error {
	if c.Get(middleware.TokenScopesKey) != nil {
		return response.Forbidden(c)
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid link ID")
	}
	if err := h.linkSvc.Revoke(c.Request().Context(), id, middleware.GetUserID(c)); err != nil {
		if errors.Is(err, service.ErrCLILinkNotFound) {
			return response.NotFound(c, "Connection link")
		}
		return response.InternalError(c)
	}
	return c.NoContent(http.StatusNoContent)
}

// Connect redeems a connection-link code. It is public and rate limited, and
// it never echoes the code. The plaintext token is returned once.
func (h *CLILinkHandler) Connect(c *echo.Context) error {
	c.Response().Header().Set("Cache-Control", "no-store")
	var req dto.RedeemCLILinkRequest
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

	token, plaintext, err := h.linkSvc.Redeem(c.Request().Context(), req.Code, req.ClientName)
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrCLILinkInvalid):
			return response.Error(c, http.StatusUnauthorized, "LINK_INVALID", "This connection link is not valid.")
		case errors.Is(err, repository.ErrCLILinkExpired):
			return response.Error(c, http.StatusGone, "LINK_EXPIRED", "This connection link has expired.")
		case errors.Is(err, repository.ErrCLILinkUsed):
			return response.Error(c, http.StatusGone, "LINK_USED", "This connection link has already been used.")
		case errors.Is(err, repository.ErrCLILinkRevoked):
			return response.Error(c, http.StatusGone, "LINK_REVOKED", "This connection link has been revoked.")
		case errors.Is(err, service.ErrCLILinkWorkspace):
			return response.Error(c, http.StatusForbidden, "LINK_WORKSPACE_FORBIDDEN", "You no longer have access to a workspace this link grants.")
		}
		return response.InternalError(c)
	}

	return response.Success(c, http.StatusOK, dto.RedeemCLILinkResponse{
		Token:          plaintext,
		TokenPrefix:    token.TokenPrefix,
		ExpiresAt:      token.ExpiresAt,
		Scopes:         token.Scopes,
		WorkspaceSlugs: token.WorkspaceSlugs,
	})
}

func toCLILinkResponse(link domain.CLIConnectionLink, linkURL string) dto.CLILinkResponse {
	return dto.CLILinkResponse{
		ID:             link.ID,
		Name:           link.Name,
		Link:           linkURL,
		CodePrefix:     link.CodePrefix,
		Scopes:         link.Scopes,
		WorkspaceSlugs: link.WorkspaceSlugs,
		TokenExpiresAt: link.TokenExpiresAt,
		ExpiresAt:      link.ExpiresAt,
		MaxUses:        link.MaxUses,
		Uses:           link.Uses,
		CreatedAt:      link.CreatedAt,
	}
}
