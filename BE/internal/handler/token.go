package handler

import (
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/kuayle/kuayle-backend/pkg/response"
	"github.com/kuayle/kuayle-backend/pkg/validate"
	"github.com/labstack/echo/v4"
)

type TokenHandler struct {
	tokenSvc *service.TokenService
}

func NewTokenHandler(tokenSvc *service.TokenService) *TokenHandler {
	return &TokenHandler{tokenSvc: tokenSvc}
}

func (h *TokenHandler) List(c echo.Context) error {
	if err := rejectPATCaller(c); err != nil {
		return err
	}
	tokens, err := h.tokenSvc.List(c.Request().Context(), middleware.GetUserID(c))
	if err != nil {
		return response.InternalError(c)
	}
	resp := make([]dto.PersonalAccessTokenResponse, len(tokens))
	for i, t := range tokens {
		resp[i] = toPersonalAccessTokenResponse(t, "")
	}
	return response.Success(c, http.StatusOK, resp)
}

func (h *TokenHandler) Create(c echo.Context) error {
	if err := rejectPATCaller(c); err != nil {
		return err
	}
	var req dto.CreateTokenRequest
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

	token, plaintext, err := h.tokenSvc.Create(c.Request().Context(), middleware.GetUserID(c), req)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidTokenScope):
			return response.ValidationError(c, []dto.ErrorDetail{{Field: "scopes", Message: err.Error()}})
		case errors.Is(err, service.ErrInvalidTokenWorkspace):
			return response.ValidationError(c, []dto.ErrorDetail{{Field: "workspace_slugs", Message: err.Error()}})
		case errors.Is(err, service.ErrInvalidTokenExpiry):
			return response.ValidationError(c, []dto.ErrorDetail{{Field: "expires_at", Message: err.Error()}})
		}
		return response.InternalError(c)
	}
	return response.Success(c, http.StatusCreated, toPersonalAccessTokenResponse(*token, plaintext))
}

func (h *TokenHandler) Revoke(c echo.Context) error {
	if err := rejectPATCaller(c); err != nil {
		return err
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid token ID")
	}
	if err := h.tokenSvc.Revoke(c.Request().Context(), id, middleware.GetUserID(c)); err != nil {
		return response.InternalError(c)
	}
	return c.NoContent(http.StatusNoContent)
}

// rejectPATCaller keeps token management interactive-session-only, so a
// leaked token cannot mint or inspect other tokens.
func rejectPATCaller(c echo.Context) error {
	if c.Get("token_scopes") != nil {
		return response.Forbidden(c)
	}
	return nil
}

func toPersonalAccessTokenResponse(t domain.PersonalAccessToken, plaintext string) dto.PersonalAccessTokenResponse {
	return dto.PersonalAccessTokenResponse{
		ID:             t.ID,
		Name:           t.Name,
		Token:          plaintext,
		TokenPrefix:    t.TokenPrefix,
		Scopes:         t.Scopes,
		WorkspaceSlugs: t.WorkspaceSlugs,
		ExpiresAt:      t.ExpiresAt,
		LastUsedAt:     t.LastUsedAt,
		CreatedAt:      t.CreatedAt,
	}
}
