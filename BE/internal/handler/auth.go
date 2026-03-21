package handler

import (
	"errors"
	"net/http"
	"time"

	"github.com/carbon/carbon-backend/internal/dto"
	"github.com/carbon/carbon-backend/internal/middleware"
	"github.com/carbon/carbon-backend/internal/service"
	"github.com/carbon/carbon-backend/pkg/response"
	"github.com/carbon/carbon-backend/pkg/validate"
	"github.com/labstack/echo/v4"
)

type AuthHandler struct {
	authService *service.AuthService
	secureCookie bool
}

func NewAuthHandler(authService *service.AuthService, secureCookie bool) *AuthHandler {
	return &AuthHandler{authService: authService, secureCookie: secureCookie}
}

func (h *AuthHandler) Register(c echo.Context) error {
	var req dto.RegisterRequest
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

	user, accessToken, refreshToken, err := h.authService.Register(c.Request().Context(), req)
	if err != nil {
		if errors.Is(err, service.ErrEmailTaken) {
			return response.Error(c, http.StatusConflict, "EMAIL_TAKEN", "Email already registered")
		}
		return response.InternalError(c)
	}

	h.setAuthCookies(c, accessToken, refreshToken)

	return response.Success(c, http.StatusCreated, dto.UserResponse{
		ID:          user.ID.String(),
		Email:       user.Email,
		Name:        user.Name,
		DisplayName: user.DisplayName,
		AvatarURL:   user.AvatarURL,
	})
}

func (h *AuthHandler) Login(c echo.Context) error {
	var req dto.LoginRequest
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

	user, accessToken, refreshToken, err := h.authService.Login(c.Request().Context(), req)
	if err != nil {
		if errors.Is(err, service.ErrInvalidCredentials) {
			return response.Error(c, http.StatusUnauthorized, "INVALID_CREDENTIALS", "Invalid email or password")
		}
		return response.InternalError(c)
	}

	h.setAuthCookies(c, accessToken, refreshToken)

	return response.Success(c, http.StatusOK, dto.UserResponse{
		ID:          user.ID.String(),
		Email:       user.Email,
		Name:        user.Name,
		DisplayName: user.DisplayName,
		AvatarURL:   user.AvatarURL,
	})
}

func (h *AuthHandler) Refresh(c echo.Context) error {
	cookie, err := c.Cookie("refresh_token")
	if err != nil || cookie.Value == "" {
		return response.Unauthorized(c)
	}

	accessToken, refreshToken, err := h.authService.RefreshTokens(c.Request().Context(), cookie.Value)
	if err != nil {
		clearAuthCookies(c)
		return response.Unauthorized(c)
	}

	h.setAuthCookies(c, accessToken, refreshToken)
	return response.Success(c, http.StatusOK, map[string]string{"status": "refreshed"})
}

func (h *AuthHandler) Logout(c echo.Context) error {
	cookie, err := c.Cookie("refresh_token")
	if err == nil && cookie.Value != "" {
		_ = h.authService.Logout(c.Request().Context(), cookie.Value)
	}
	clearAuthCookies(c)
	return response.Success(c, http.StatusOK, map[string]string{"status": "logged out"})
}

func (h *AuthHandler) Me(c echo.Context) error {
	userID := middleware.GetUserID(c)
	user, err := h.authService.GetUserByID(c.Request().Context(), userID)
	if err != nil || user == nil {
		return response.NotFound(c, "User")
	}
	return response.Success(c, http.StatusOK, dto.UserResponse{
		ID:          user.ID.String(),
		Email:       user.Email,
		Name:        user.Name,
		DisplayName: user.DisplayName,
		AvatarURL:   user.AvatarURL,
	})
}

func (h *AuthHandler) setAuthCookies(c echo.Context, accessToken, refreshToken string) {
	c.SetCookie(&http.Cookie{
		Name:     "access_token",
		Value:    accessToken,
		Path:     "/",
		HttpOnly: true,
		Secure:   h.secureCookie,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   900, // 15 min
	})
	c.SetCookie(&http.Cookie{
		Name:     "refresh_token",
		Value:    refreshToken,
		Path:     "/api/auth",
		HttpOnly: true,
		Secure:   h.secureCookie,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(7 * 24 * time.Hour / time.Second),
	})
}

func clearAuthCookies(c echo.Context) {
	c.SetCookie(&http.Cookie{
		Name:     "access_token",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})
	c.SetCookie(&http.Cookie{
		Name:     "refresh_token",
		Value:    "",
		Path:     "/api/auth",
		HttpOnly: true,
		MaxAge:   -1,
	})
}
