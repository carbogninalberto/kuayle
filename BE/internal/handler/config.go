package handler

import (
	"net/http"

	"github.com/kuayle/kuayle-backend/pkg/response"
	"github.com/labstack/echo/v4"
)

// ConfigHandler exposes public instance configuration for the frontend
// bootstrap (no auth required).
type ConfigHandler struct {
	registrationEnabled bool
}

func NewConfigHandler(registrationEnabled bool) *ConfigHandler {
	return &ConfigHandler{registrationEnabled: registrationEnabled}
}

// Get handles GET /api/config
func (h *ConfigHandler) Get(c echo.Context) error {
	return response.Success(c, http.StatusOK, map[string]bool{
		"registration_enabled": h.registrationEnabled,
	})
}
