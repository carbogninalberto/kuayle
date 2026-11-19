package handler

import (
	"encoding/json"
	"errors"
	"mime"
	"net/http"

	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/kuayle/kuayle-backend/pkg/response"
	"github.com/labstack/echo/v5"
)

func (h *AuthHandler) ExchangeSignInToken(c *echo.Context) error {
	c.Response().Header().Set("Referrer-Policy", "no-referrer")
	c.Response().Header().Set("Cache-Control", "no-store")

	r := c.Request()
	mediaType, _, _ := mime.ParseMediaType(r.Header.Get(echo.HeaderContentType))
	if mediaType != echo.MIMEApplicationJSON {
		return response.Error(c, http.StatusUnsupportedMediaType, "BAD_REQUEST", "Expected a JSON request body")
	}
	var req struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(c.Response(), r.Body, 1024)).Decode(&req); err != nil || req.Token == "" {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid request body")
	}

	access, refresh, err := h.authService.ExchangeSignInToken(r.Context(), c.Param("slug"), req.Token,
		requestCookie(r, h.accessCookieName()), requestCookie(r, h.refreshCookieName()))
	switch {
	case errors.Is(err, service.ErrInvalidToken):
		return response.Error(c, http.StatusUnauthorized, "INVALID_SIGN_IN_TOKEN", "This sign-in link is unavailable")
	case errors.Is(err, service.ErrSSORequired):
		return SSORequiredReply(c, err)
	case err != nil:
		return response.InternalError(c)
	}
	if access != "" {
		h.setAuthCookies(c, access, refresh)
	}
	return c.NoContent(http.StatusNoContent)
}

func requestCookie(r *http.Request, name string) string {
	if cookie, err := r.Cookie(name); err == nil {
		return cookie.Value
	}
	return ""
}
