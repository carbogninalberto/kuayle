package handler

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/kuayle/kuayle-backend/pkg/response"
	"github.com/kuayle/kuayle-backend/pkg/validate"
	"github.com/labstack/echo/v5"
	log "github.com/sirupsen/logrus"
)

// SSOBindCookieName is the short-lived, HttpOnly cookie that proves the
// callback belongs to the browser that started the flow.
const SSOBindCookieName = "kuayle_sso_bind"

// SSOHandler serves workspace OIDC settings and the public sign-in flow.
type SSOHandler struct {
	sso          *service.SSOService
	auth         *AuthHandler
	secureCookie bool
}

func NewSSOHandler(sso *service.SSOService, auth *AuthHandler, secureCookie bool) *SSOHandler {
	return &SSOHandler{sso: sso, auth: auth, secureCookie: secureCookie}
}

// Get handles GET /api/workspaces/:slug/sso.
func (h *SSOHandler) Get(c *echo.Context) error {
	config, err := h.sso.GetConfig(c.Request().Context(), c.Param("slug"))
	if err != nil {
		return h.configFailure(c, err)
	}
	return response.Success(c, http.StatusOK, config)
}

// Save handles PUT /api/workspaces/:slug/sso.
func (h *SSOHandler) Save(c *echo.Context) error {
	var req dto.SSOConfigRequest
	if err := c.Bind(&req); err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid request body")
	}
	if err := validate.Struct(&req); err != nil {
		return response.ValidationError(c, validationDetails(err))
	}
	config, err := h.sso.SaveConfig(c.Request().Context(), c.Param("slug"), req)
	if err != nil {
		return h.configFailure(c, err)
	}
	log.WithFields(log.Fields{
		"event": "sso.config_saved", "workspace_slug": c.Param("slug"), "user_id": middleware.GetUserID(c), "ip": c.RealIP(),
	}).Info("workspace SSO configuration saved")
	return response.Success(c, http.StatusOK, config)
}

// Delete handles DELETE /api/workspaces/:slug/sso.
func (h *SSOHandler) Delete(c *echo.Context) error {
	if err := h.sso.DeleteConfig(c.Request().Context(), c.Param("slug")); err != nil {
		return h.configFailure(c, err)
	}
	log.WithFields(log.Fields{
		"event": "sso.config_deleted", "workspace_slug": c.Param("slug"), "user_id": middleware.GetUserID(c), "ip": c.RealIP(),
	}).Info("workspace SSO configuration removed")
	return response.Success(c, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *SSOHandler) configFailure(c *echo.Context, err error) error {
	switch {
	case errors.Is(err, service.ErrSSOInstanceManaged):
		return response.Error(c, http.StatusConflict, "SSO_MANAGED_BY_INSTANCE", err.Error())
	case errors.Is(err, service.ErrSSOInvalidInput):
		return response.Error(c, http.StatusBadRequest, "SSO_INVALID_CONFIG", err.Error())
	case errors.Is(err, service.ErrSSONotConfigured):
		return response.NotFound(c, "SSO configuration")
	default:
		log.WithError(err).WithField("event", "sso.config_failed").Error("workspace SSO configuration failed")
		return response.InternalError(c)
	}
}

// Public handles GET /api/auth/sso/:slug.
func (h *SSOHandler) Public(c *echo.Context) error {
	info, err := h.sso.PublicInfo(c.Request().Context(), c.Param("slug"))
	if err != nil {
		if errors.Is(err, service.ErrSSONotConfigured) {
			return response.NotFound(c, "Workspace")
		}
		return response.InternalError(c)
	}
	return response.Success(c, http.StatusOK, info)
}

// Start handles GET /api/auth/sso/:slug/start.
func (h *SSOHandler) Start(c *echo.Context) error {
	authURL, bind, err := h.sso.Start(c.Request().Context(), c.Param("slug"), c.QueryParam("redirect_after"))
	if err != nil {
		return h.startFailure(c, err)
	}
	c.SetCookie(&http.Cookie{
		Name:     SSOBindCookieName,
		Value:    bind,
		Path:     "/",
		HttpOnly: true,
		Secure:   h.secureCookie,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(service.SSOLoginStateTTL / time.Second),
	})
	return c.Redirect(http.StatusFound, authURL)
}

func (h *SSOHandler) startFailure(c *echo.Context, err error) error {
	code := ssoErrorCode(err)
	log.WithFields(log.Fields{
		"event": "sso.start_failed", "workspace_slug": c.Param("slug"), "reason": code, "ip": c.RealIP(),
	}).Warn("SSO sign-in could not start")
	if errors.Is(err, service.ErrSSONotConfigured) {
		return response.Error(c, http.StatusNotFound, "SSO_NOT_CONFIGURED", "This workspace does not offer single sign-on")
	}
	return response.Error(c, http.StatusBadRequest, "SSO_START_FAILED", "The sign-in could not be started")
}

// Callback handles GET /api/auth/sso/:slug/callback. Every outcome is a
// redirect back to the login page; no token, code or IdP error text is put in
// the URL or the logs.
func (h *SSOHandler) Callback(c *echo.Context) error {
	slug := c.Param("slug")
	bind := ""
	if cookie, err := c.Cookie(SSOBindCookieName); err == nil {
		bind = cookie.Value
	}
	h.clearBindCookie(c)

	if idpError := strings.TrimSpace(c.QueryParam("error")); idpError != "" {
		log.WithFields(log.Fields{
			"event": "sso.callback_failed", "workspace_slug": slug, "reason": "provider_error", "ip": c.RealIP(),
		}).Warn("SSO callback refused")
		return h.redirectLogin(c, "provider_error")
	}

	user, accessToken, refreshToken, redirectAfter, err := h.sso.CallbackRedirect(
		c.Request().Context(), slug, c.QueryParam("state"), c.QueryParam("code"), bind)
	if err != nil {
		code := ssoErrorCode(err)
		fields := log.Fields{"event": "sso.callback_failed", "workspace_slug": slug, "reason": code, "ip": c.RealIP()}
		log.WithFields(fields).Warn("SSO callback refused")
		return h.redirectLogin(c, code)
	}

	h.auth.setAuthCookies(c, accessToken, refreshToken)
	log.WithFields(log.Fields{
		"event": "sso.login", "workspace_slug": slug, "user_id": user.ID, "ip": c.RealIP(),
	}).Info("user signed in with SSO")

	target := "/" + url.PathEscape(slug)
	if redirectAfter != "" {
		target = redirectAfter
	}
	if strings.HasPrefix(target, "/") {
		return c.Redirect(http.StatusFound, target)
	}
	return c.Redirect(http.StatusFound, "/"+url.PathEscape(slug))
}

func (h *SSOHandler) redirectLogin(c *echo.Context, code string) error {
	return c.Redirect(http.StatusFound, "/login?sso_error="+url.QueryEscape(code))
}

func (h *SSOHandler) clearBindCookie(c *echo.Context) {
	c.SetCookie(&http.Cookie{
		Name:     SSOBindCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   h.secureCookie,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

func ssoErrorCode(err error) string {
	switch {
	case errors.Is(err, service.ErrSSONotConfigured):
		return "not_configured"
	case errors.Is(err, service.ErrSSOStateInvalid):
		return "invalid_state"
	case errors.Is(err, service.ErrSSOEmailUnverified):
		return "email_unverified"
	case errors.Is(err, service.ErrSSODomainNotAllowed):
		return "domain_not_allowed"
	case errors.Is(err, service.ErrSSOTokenInvalid):
		return "token_invalid"
	default:
		return "failed"
	}
}

// SSORequiredReply is used by the password and passkey sign-in handlers.
func SSORequiredReply(c *echo.Context, err error) error {
	var required *service.SSORequiredError
	if errors.As(err, &required) {
		return response.SSORequired(c, required.WorkspaceSlug)
	}
	return nil
}
