package handler

import (
	"crypto/sha256"
	"encoding/hex"
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

// SSOBindCookieName is the prefix of the short-lived, HttpOnly cookie that
// proves a callback belongs to the browser that started that flow. Each flow
// gets its own cookie so two tabs (or workspaces) cannot overwrite each other.
const SSOBindCookieName = "kuayle_sso_bind"

// SSOHandler serves workspace SSO settings and the public sign-in flow.
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

func (h *SSOHandler) Metadata(c *echo.Context) error {
	data, err := h.sso.SAMLMetadata(c.Request().Context(), c.Param("slug"))
	if err != nil {
		return h.configFailure(c, err)
	}
	return c.Blob(http.StatusOK, "application/samlmetadata+xml", data)
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
	h.setBindCookie(c, c.Param("slug"), authURL, bind)
	return c.Redirect(http.StatusFound, authURL)
}

// StartLink handles POST /api/auth/sso/:slug/link. It links the IdP subject to
// the signed-in account and returns the authorization URL. It deliberately
// lives outside the workspace group so require-SSO cannot block a member who is
// trying to link; the service checks membership.
func (h *SSOHandler) StartLink(c *echo.Context) error {
	authURL, bind, err := h.sso.StartLink(c.Request().Context(), c.Param("slug"), middleware.GetUserID(c), c.QueryParam("redirect_after"))
	if err != nil {
		return h.startFailure(c, err)
	}
	h.setBindCookie(c, c.Param("slug"), authURL, bind)
	return response.Success(c, http.StatusOK, map[string]string{"auth_url": authURL})
}

// setBindCookie stores the per-flow binding value. The cookie name carries a
// digest of the state and its path is the flow's callback, so concurrent flows
// in two tabs or two workspaces never share a binding.
func (h *SSOHandler) setBindCookie(c *echo.Context, slug, authURL, bind string) {
	parsed, err := url.Parse(authURL)
	if err != nil {
		return
	}
	isSAML := parsed.Query().Has("SAMLRequest")
	state := parsed.Query().Get("state")
	if isSAML {
		state = parsed.Query().Get("RelayState")
	}
	h.writeBindCookie(c, slug, state, bind, int(service.SSOLoginStateTTL/time.Second), isSAML)
}

func (h *SSOHandler) writeBindCookie(c *echo.Context, slug, state, bind string, maxAge int, isSAML bool) {
	sameSite := http.SameSiteLaxMode
	if isSAML {
		sameSite = http.SameSiteNoneMode
	}
	if state == "" {
		return
	}
	c.SetCookie(&http.Cookie{
		Name:     ssoBindCookieName(state),
		Value:    bind,
		Path:     ssoBindCookiePath(slug),
		HttpOnly: true,
		Secure:   h.secureCookie || isSAML,
		SameSite: sameSite,
		MaxAge:   maxAge,
	})
}

func ssoBindCookieName(state string) string {
	sum := sha256.Sum256([]byte(state))
	return SSOBindCookieName + "_" + hex.EncodeToString(sum[:])[:16]
}

func ssoBindCookiePath(slug string) string {
	return "/api/auth/sso/" + url.PathEscape(slug) + "/callback"
}

func (h *SSOHandler) startFailure(c *echo.Context, err error) error {
	code := ssoErrorCode(err)
	log.WithFields(log.Fields{
		"event": "sso.start_failed", "workspace_slug": c.Param("slug"), "reason": code, "ip": c.RealIP(),
	}).Warn("SSO sign-in could not start")
	switch {
	case errors.Is(err, service.ErrSSONotConfigured):
		return response.Error(c, http.StatusNotFound, "SSO_NOT_CONFIGURED", "This workspace does not offer single sign-on")
	case errors.Is(err, service.ErrSSONotMember):
		return response.Error(c, http.StatusForbidden, "SSO_NOT_MEMBER", "You are not a member of this workspace")
	default:
		return response.Error(c, http.StatusBadRequest, "SSO_START_FAILED", "The sign-in could not be started")
	}
}

// Callback handles the OIDC GET and SAML POST to /api/auth/sso/:slug/callback. Every outcome is a
// redirect back to the login page; no token, code or IdP error text is put in
// the URL or the logs.
func (h *SSOHandler) Callback(c *echo.Context) error {
	slug := c.Param("slug")
	state := c.QueryParam("state")
	complete := h.sso.CallbackRedirect
	code := c.QueryParam("code")
	if c.Request().Method == http.MethodPost {
		req := c.Request()
		req.Body = http.MaxBytesReader(c.Response(), req.Body, 2<<20)
		if req.ParseForm() != nil {
			return h.redirectLogin(c, "token_invalid")
		}
		state = req.PostForm.Get("RelayState")
		code = req.PostForm.Get("SAMLResponse")
		complete = h.sso.SAMLCallback
	}
	bind := ""
	if state != "" {
		if cookie, err := c.Cookie(ssoBindCookieName(state)); err == nil {
			bind = cookie.Value
		}
	}
	h.writeBindCookie(c, slug, state, "", -1, c.Request().Method == http.MethodPost)

	if idpError := strings.TrimSpace(c.QueryParam("error")); idpError != "" {
		log.WithFields(log.Fields{
			"event": "sso.callback_failed", "workspace_slug": slug, "reason": "provider_error", "ip": c.RealIP(),
		}).Warn("SSO callback refused")
		return h.redirectLogin(c, "provider_error")
	}

	user, accessToken, refreshToken, redirectAfter, err := complete(
		c.Request().Context(), slug, state, code, bind)
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

	// redirectAfter was stored through SanitizeRedirectAfter: empty or a
	// same-origin path.
	target := "/" + url.PathEscape(slug)
	if redirectAfter != "" {
		target = redirectAfter
	}
	return c.Redirect(http.StatusFound, target)
}

func (h *SSOHandler) redirectLogin(c *echo.Context, code string) error {
	target := "/login?sso_error=" + url.QueryEscape(code)
	if slug := c.Param("slug"); slug != "" {
		target += "&workspace=" + url.QueryEscape(slug)
	}
	return c.Redirect(http.StatusFound, target)
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
	case errors.Is(err, service.ErrSSOLinkRequired):
		return "link_required"
	case errors.Is(err, service.ErrSSONotMember):
		return "not_member"
	case errors.Is(err, service.ErrSSOIdentityTaken):
		return "identity_taken"
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
