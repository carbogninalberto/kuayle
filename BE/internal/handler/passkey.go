package handler

import (
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/kuayle/kuayle-backend/pkg/response"
	"github.com/kuayle/kuayle-backend/pkg/validate"
	"github.com/labstack/echo/v5"
	log "github.com/sirupsen/logrus"
)

// PasskeyHandler serves /api/auth/passkey. A successful sign-in sets the same
// cookies, through the same code, as a password sign-in.
type PasskeyHandler struct {
	passkeys *service.PasskeyService
	auth     *AuthHandler
}

func NewPasskeyHandler(passkeys *service.PasskeyService, auth *AuthHandler) *PasskeyHandler {
	return &PasskeyHandler{passkeys: passkeys, auth: auth}
}

// BeginRegistration handles POST /api/auth/passkey/register/begin.
func (h *PasskeyHandler) BeginRegistration(c *echo.Context) error {
	var req dto.BeginPasskeyRegistrationRequest
	if err := c.Bind(&req); err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid request body")
	}
	if err := validate.Struct(&req); err != nil {
		return response.ValidationError(c, validationDetails(err))
	}
	userID := middleware.GetUserID(c)
	// Password re-entry is a password check like sign-in and is throttled the
	// same way, per account. A fresh session's password is never compared, so
	// it neither meets the lock nor clears it.
	throttleKey := "passkey-reauth:" + userID.String()
	authenticatedAt := middleware.GetSessionAuthTime(c)
	passwordChecked := req.Password != "" && !h.passkeys.SessionFresh(authenticatedAt)
	if passwordChecked && h.auth.loginThrottle.IsLocked(throttleKey) {
		return response.Error(c, http.StatusTooManyRequests, "ACCOUNT_LOCKED", "Too many failed attempts, please try again later")
	}
	options, handle, err := h.passkeys.BeginRegistration(c.Request().Context(), userID, authenticatedAt, req.Password, c.Request().Header.Get("Origin"))
	if err != nil {
		if errors.Is(err, service.ErrInvalidCredentials) {
			h.auth.loginThrottle.RecordFailure(throttleKey)
			log.WithFields(log.Fields{"event": "auth.passkey_reauth_failed", "user_id": userID, "ip": c.RealIP()}).Warn("passkey registration: wrong password")
			return response.Error(c, http.StatusForbidden, "INVALID_PASSWORD", "The password is not correct")
		}
		return h.fail(c, "auth.passkey_register_failed", err)
	}
	if passwordChecked {
		h.auth.loginThrottle.RecordSuccess(throttleKey)
	}
	h.setChallengeCookie(c, handle)
	return response.Success(c, http.StatusOK, options)
}

// FinishRegistration handles POST /api/auth/passkey/register/complete.
func (h *PasskeyHandler) FinishRegistration(c *echo.Context) error {
	var req dto.FinishPasskeyRegistrationRequest
	if err := c.Bind(&req); err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid request body")
	}
	if err := validate.Struct(&req); err != nil {
		return response.ValidationError(c, validationDetails(err))
	}
	userID := middleware.GetUserID(c)
	handle := h.challengeHandle(c)
	h.clearChallengeCookie(c)
	passkey, err := h.passkeys.FinishRegistration(c.Request().Context(), userID, handle, req.Name, req.Credential)
	if err != nil {
		return h.fail(c, "auth.passkey_register_failed", err)
	}
	log.WithFields(log.Fields{"event": "auth.passkey_registered", "user_id": userID, "passkey_id": passkey.ID, "ip": c.RealIP()}).Info("passkey registered")
	return response.Success(c, http.StatusCreated, h.passkeyResponse(c, passkey))
}

// BeginLogin handles POST /api/auth/passkey/login/begin.
func (h *PasskeyHandler) BeginLogin(c *echo.Context) error {
	options, handle, err := h.passkeys.BeginLogin(c.Request().Context(), h.challengeHandle(c), c.Request().Header.Get("Origin"))
	if err != nil {
		return h.fail(c, "auth.passkey_login_failed", err)
	}
	h.setChallengeCookie(c, handle)
	return response.Success(c, http.StatusOK, options)
}

// FinishLogin handles POST /api/auth/passkey/login/complete.
func (h *PasskeyHandler) FinishLogin(c *echo.Context) error {
	var req dto.FinishPasskeyLoginRequest
	if err := c.Bind(&req); err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid request body")
	}
	if err := validate.Struct(&req); err != nil {
		return response.ValidationError(c, validationDetails(err))
	}
	handle := h.challengeHandle(c)
	h.clearChallengeCookie(c)
	user, accessToken, refreshToken, err := h.passkeys.FinishLogin(c.Request().Context(), handle, req.Credential)
	if err != nil {
		return h.fail(c, "auth.passkey_login_failed", err)
	}
	log.WithFields(log.Fields{"event": "auth.login", "method": "passkey", "user_id": user.ID, "email": user.Email, "ip": c.RealIP()}).Info("user logged in")
	h.auth.setAuthCookies(c, accessToken, refreshToken)
	return response.Success(c, http.StatusOK, h.auth.userResponse(user))
}

// List handles GET /api/auth/passkey/credentials.
func (h *PasskeyHandler) List(c *echo.Context) error {
	passkeys, err := h.passkeys.List(c.Request().Context(), middleware.GetUserID(c))
	if err != nil {
		return response.InternalError(c)
	}
	out := make([]dto.PasskeyResponse, 0, len(passkeys))
	for i := range passkeys {
		out = append(out, h.passkeyResponse(c, &passkeys[i]))
	}
	return response.Success(c, http.StatusOK, out)
}

// Rename handles PATCH /api/auth/passkey/credentials/:id.
func (h *PasskeyHandler) Rename(c *echo.Context) error {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return response.NotFound(c, "Passkey")
	}
	var req dto.RenamePasskeyRequest
	if err := c.Bind(&req); err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid request body")
	}
	if err := validate.Struct(&req); err != nil {
		return response.ValidationError(c, validationDetails(err))
	}
	passkey, err := h.passkeys.Rename(c.Request().Context(), middleware.GetUserID(c), id, req.Name)
	if err != nil {
		return h.fail(c, "auth.passkey_rename_failed", err)
	}
	return response.Success(c, http.StatusOK, h.passkeyResponse(c, passkey))
}

// Delete handles DELETE /api/auth/passkey/credentials/:id.
func (h *PasskeyHandler) Delete(c *echo.Context) error {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return response.NotFound(c, "Passkey")
	}
	userID := middleware.GetUserID(c)
	if err := h.passkeys.Delete(c.Request().Context(), userID, id); err != nil {
		return h.fail(c, "auth.passkey_delete_failed", err)
	}
	log.WithFields(log.Fields{"event": "auth.passkey_deleted", "user_id": userID, "passkey_id": id, "ip": c.RealIP()}).Info("passkey removed")
	return response.Success(c, http.StatusOK, map[string]string{"status": "deleted"})
}

// fail maps a service error to its response. Refused ceremonies are written to
// the audit log with what is known about them; the client gets no detail.
func (h *PasskeyHandler) fail(c *echo.Context, event string, err error) error {
	fields := log.Fields{"event": event, "ip": c.RealIP()}
	if userID := middleware.GetUserID(c); userID != uuid.Nil {
		fields["user_id"] = userID
	}
	var rejection *service.PasskeyRejection
	if errors.As(err, &rejection) {
		fields["detail"] = rejection.Detail
		if rejection.UserID != uuid.Nil {
			fields["user_id"] = rejection.UserID
		}
		if rejection.PasskeyID != uuid.Nil {
			fields["passkey_id"] = rejection.PasskeyID
		}
	}
	warn := func(reason string) {
		log.WithFields(fields).WithField("reason", reason).Warn("passkey request refused")
	}
	switch {
	case errors.Is(err, service.ErrPasskeysDisabled):
		return response.Error(c, http.StatusForbidden, "PASSKEYS_DISABLED", "Passkeys are not available on this instance")
	case errors.Is(err, service.ErrPasskeyOrigin):
		warn("origin_mismatch")
		return response.Error(c, http.StatusBadRequest, "PASSKEY_ORIGIN_MISMATCH", "Passkeys are not set up for the address you are using to reach this instance")
	case errors.Is(err, service.ErrPasskeyReauthRequired):
		return response.Error(c, http.StatusForbidden, "REAUTH_REQUIRED", "Enter your password to add a passkey")
	case errors.Is(err, service.ErrPasskeyChallenge):
		warn("challenge_missing_or_expired")
		return response.Error(c, http.StatusBadRequest, "PASSKEY_CHALLENGE_EXPIRED", "This passkey request has expired or was already used. Please try again")
	case errors.Is(err, service.ErrPasskeyCloneSuspected):
		log.WithFields(fields).WithField("event", "auth.passkey_clone_suspected").Error("passkey signature counter regressed; the passkey may have been cloned")
		return response.Error(c, http.StatusUnauthorized, "PASSKEY_INVALID", "This passkey could not be verified")
	case errors.Is(err, service.ErrPasskeyUnknownCredential):
		warn("unknown_credential")
		return response.Error(c, http.StatusUnauthorized, "PASSKEY_UNKNOWN", "This passkey is not registered with any account")
	case errors.Is(err, service.ErrPasskeyVerification):
		warn("verification_failed")
		if middleware.GetUserID(c) != uuid.Nil {
			return response.Error(c, http.StatusBadRequest, "PASSKEY_INVALID", "This passkey could not be verified")
		}
		return response.Error(c, http.StatusUnauthorized, "PASSKEY_INVALID", "This passkey could not be verified")
	case errors.Is(err, service.ErrPasskeyAlreadyRegistered):
		return response.Error(c, http.StatusConflict, "PASSKEY_ALREADY_REGISTERED", "This passkey is already registered")
	case errors.Is(err, service.ErrPasskeyLimit):
		return response.Error(c, http.StatusConflict, "PASSKEY_LIMIT", "You have reached the maximum number of passkeys")
	case errors.Is(err, service.ErrLastPasskey):
		return response.Error(c, http.StatusConflict, "LAST_PASSKEY", "This account has no password, so its last passkey cannot be removed")
	case errors.Is(err, service.ErrPasskeyName):
		return response.ValidationError(c, []dto.ErrorDetail{{Field: "Name", Message: err.Error()}})
	case errors.Is(err, service.ErrPasskeyNotFound):
		return response.NotFound(c, "Passkey")
	}
	log.WithFields(fields).WithError(err).Error("passkey request failed")
	return response.InternalError(c)
}

func (h *PasskeyHandler) passkeyResponse(c *echo.Context, p *domain.PasskeyCredential) dto.PasskeyResponse {
	transports := p.Transports
	if transports == nil {
		transports = []string{}
	}
	aaguid := ""
	if id, err := uuid.FromBytes(p.AAGUID); err == nil {
		aaguid = id.String()
	}
	current, _ := c.Get(middleware.SessionPasskeyKey).(uuid.UUID)
	return dto.PasskeyResponse{
		ID: p.ID, Name: p.Name, Transports: transports, AAGUID: aaguid, BackedUp: p.BackupState,
		CreatedAt: p.CreatedAt, LastUsedAt: p.LastUsedAt, Current: current == p.ID,
	}
}

// The challenge cookie binds a ceremony to the browser that started it. It
// holds an opaque handle; the challenge itself stays on the server.
func (h *PasskeyHandler) challengeCookieName() string {
	if h.auth.hostCookies {
		return "__Host-kuayle-passkey"
	}
	return "passkey_challenge"
}

func (h *PasskeyHandler) challengeCookiePath() string {
	if h.auth.hostCookies {
		return "/"
	}
	return "/api/auth/passkey"
}

func (h *PasskeyHandler) challengeHandle(c *echo.Context) string {
	cookie, err := c.Cookie(h.challengeCookieName())
	if err != nil {
		return ""
	}
	return cookie.Value
}

func (h *PasskeyHandler) setChallengeCookie(c *echo.Context, handle string) {
	c.SetCookie(&http.Cookie{
		Name: h.challengeCookieName(), Value: handle, Path: h.challengeCookiePath(),
		HttpOnly: true, Secure: h.auth.secureCookie, SameSite: http.SameSiteStrictMode,
		MaxAge: int(service.PasskeyChallengeTTL / time.Second),
	})
}

func (h *PasskeyHandler) clearChallengeCookie(c *echo.Context) {
	c.SetCookie(&http.Cookie{
		Name: h.challengeCookieName(), Value: "", Path: h.challengeCookiePath(),
		HttpOnly: true, Secure: h.auth.secureCookie, SameSite: http.SameSiteStrictMode, MaxAge: -1,
	})
}
