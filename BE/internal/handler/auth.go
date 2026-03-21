package handler

import (
	"errors"
	"net/http"
	"time"

	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/kuayle/kuayle-backend/pkg/response"
	"github.com/kuayle/kuayle-backend/pkg/validate"
	"github.com/labstack/echo/v4"
	log "github.com/sirupsen/logrus"
)

type AuthHandler struct {
	authService    *service.AuthService
	secureCookie   bool
	loginThrottle  *middleware.LoginThrottle
}

func NewAuthHandler(authService *service.AuthService, secureCookie bool, loginThrottle *middleware.LoginThrottle) *AuthHandler {
	return &AuthHandler{authService: authService, secureCookie: secureCookie, loginThrottle: loginThrottle}
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
		if errors.Is(err, service.ErrWeakPassword) {
			return response.Error(c, http.StatusBadRequest, "WEAK_PASSWORD", err.Error())
		}
		if errors.Is(err, service.ErrEmailTaken) {
			log.WithFields(log.Fields{"event": "auth.register_failed", "email": req.Email, "reason": "email_taken", "ip": c.RealIP()}).Warn("registration failed")
			return response.Error(c, http.StatusConflict, "EMAIL_TAKEN", "Email already registered")
		}
		return response.InternalError(c)
	}

	log.WithFields(log.Fields{"event": "auth.register", "user_id": user.ID, "email": user.Email, "ip": c.RealIP()}).Info("user registered")
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

	if h.loginThrottle.IsLocked(req.Email) {
		log.WithFields(log.Fields{"event": "auth.login_locked", "email": req.Email, "ip": c.RealIP()}).Warn("login attempt while locked")
		return response.Error(c, http.StatusTooManyRequests, "ACCOUNT_LOCKED", "Too many failed attempts, please try again later")
	}

	user, accessToken, refreshToken, err := h.authService.Login(c.Request().Context(), req)
	if err != nil {
		if errors.Is(err, service.ErrInvalidCredentials) {
			h.loginThrottle.RecordFailure(req.Email)
			log.WithFields(log.Fields{"event": "auth.login_failed", "email": req.Email, "ip": c.RealIP()}).Warn("login failed")
			return response.Error(c, http.StatusUnauthorized, "INVALID_CREDENTIALS", "Invalid email or password")
		}
		return response.InternalError(c)
	}

	h.loginThrottle.RecordSuccess(req.Email)
	log.WithFields(log.Fields{"event": "auth.login", "user_id": user.ID, "email": user.Email, "ip": c.RealIP()}).Info("user logged in")
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
		log.WithFields(log.Fields{"event": "auth.refresh_failed", "ip": c.RealIP()}).Warn("token refresh failed")
		clearAuthCookies(c)
		return response.Unauthorized(c)
	}

	log.WithFields(log.Fields{"event": "auth.refresh", "ip": c.RealIP()}).Info("token refreshed")
	h.setAuthCookies(c, accessToken, refreshToken)
	return response.Success(c, http.StatusOK, map[string]string{"status": "refreshed"})
}

func (h *AuthHandler) Logout(c echo.Context) error {
	cookie, err := c.Cookie("refresh_token")
	if err == nil && cookie.Value != "" {
		_ = h.authService.Logout(c.Request().Context(), cookie.Value)
	}
	log.WithFields(log.Fields{"event": "auth.logout", "ip": c.RealIP()}).Info("user logged out")
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
		SameSite: http.SameSiteStrictMode,
		MaxAge:   900, // 15 min
	})
	c.SetCookie(&http.Cookie{
		Name:     "refresh_token",
		Value:    refreshToken,
		Path:     "/api/auth",
		HttpOnly: true,
		Secure:   h.secureCookie,
		SameSite: http.SameSiteStrictMode,
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
