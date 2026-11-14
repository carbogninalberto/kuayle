package handler

import (
	"net/http"

	"github.com/kuayle/kuayle-backend/pkg/response"
	"github.com/labstack/echo/v5"
)

// ConfigHandler exposes public instance configuration for the frontend
// bootstrap (no auth required).
type ConfigHandler struct {
	registrationEnabled bool
	passkeysEnabled     bool
	ssoInstanceEnabled  bool
}

func NewConfigHandler(registrationEnabled bool) *ConfigHandler {
	return &ConfigHandler{registrationEnabled: registrationEnabled}
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

// Get handles GET /api/config
func (h *ConfigHandler) Get(c *echo.Context) error {
	return response.Success(c, http.StatusOK, map[string]bool{
		"registration_enabled": h.registrationEnabled,
		"passkeys_enabled":     h.passkeysEnabled,
		"sso_enabled_instance": h.ssoInstanceEnabled,
	})
}
