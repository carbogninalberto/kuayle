package handler

import (
	"context"
	"net/http"

	"github.com/kuayle/kuayle-backend/pkg/response"
	"github.com/labstack/echo/v5"
	log "github.com/sirupsen/logrus"
)

// ConfigHandler exposes public instance configuration for the frontend
// bootstrap (no auth required).
type ConfigHandler struct {
	registrationEnabled func() bool
	passkeysEnabled     bool
	ssoInstanceEnabled  bool
	ssoAvailable        func(context.Context) (bool, error)
}

func NewConfigHandler(registrationEnabled bool) *ConfigHandler {
	return &ConfigHandler{registrationEnabled: func() bool { return registrationEnabled }}
}

// WithRegistration reads whether sign-up is open on every request, so a
// runtime change to DISABLE_REGISTRATION reaches the sign-in page.
func (h *ConfigHandler) WithRegistration(enabled func() bool) *ConfigHandler {
	h.registrationEnabled = enabled
	return h
}

// WithPasskeys reports whether this instance can run passkey ceremonies.
func (h *ConfigHandler) WithPasskeys(enabled bool) *ConfigHandler {
	h.passkeysEnabled = enabled
	return h
}

// WithSSOInstance reports whether the instance pins SSO for a workspace.
func (h *ConfigHandler) WithSSOInstance(enabled bool) *ConfigHandler {
	h.ssoInstanceEnabled = enabled
	return h
}

func (h *ConfigHandler) WithSSOAvailability(available func(context.Context) (bool, error)) *ConfigHandler {
	h.ssoAvailable = available
	return h
}

// Get handles GET /api/config
func (h *ConfigHandler) Get(c *echo.Context) error {
	var ssoEnabled bool
	if h.ssoAvailable != nil {
		enabled, err := h.ssoAvailable(c.Request().Context())
		if err != nil {
			log.WithError(err).Warn("failed to determine SSO availability")
		} else {
			ssoEnabled = enabled
		}
	}
	return response.Success(c, http.StatusOK, map[string]bool{
		"registration_enabled": h.registrationEnabled(),
		"passkeys_enabled":     h.passkeysEnabled,
		"sso_enabled_instance": h.ssoInstanceEnabled,
		"sso_enabled":          ssoEnabled,
	})
}
