package handler

import (
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/config"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/kuayle/kuayle-backend/pkg/response"
	"github.com/labstack/echo/v5"
	log "github.com/sirupsen/logrus"
)

// InstanceConfigHandler serves Settings -> Instance configuration: every
// variable the API reads, for instance sysadmins (SYSADMINS) only, and the
// runtime overrides of the few a running server reads per use.
type InstanceConfigHandler struct {
	svc        *service.InstanceConfigService
	isSysAdmin func(uuid.UUID) bool
}

func NewInstanceConfigHandler(svc *service.InstanceConfigService, isSysAdmin func(uuid.UUID) bool) *InstanceConfigHandler {
	if isSysAdmin == nil {
		isSysAdmin = func(uuid.UUID) bool { return false }
	}
	return &InstanceConfigHandler{svc: svc, isSysAdmin: isSysAdmin}
}

// allowed marks every response uncacheable, then admits sysadmins only.
func (h *InstanceConfigHandler) allowed(c *echo.Context) (uuid.UUID, bool) {
	c.Response().Header().Set("Cache-Control", "private, no-store")
	user := middleware.GetUserID(c)
	return user, h.isSysAdmin(user)
}

func instanceConfigError(c *echo.Context, err error) error {
	var invalid *service.InstanceConfigInvalidError
	switch {
	case errors.As(err, &invalid):
		return response.Error(c, http.StatusBadRequest, "INVALID_CONFIG_VALUE", invalid.Message)
	case errors.Is(err, service.ErrInstanceConfigReadOnly):
		return response.Error(c, http.StatusConflict, "CONFIG_READ_ONLY", "This variable is set in .env and needs a restart")
	case errors.Is(err, service.ErrInstanceConfigUnknown):
		return response.NotFound(c, "Configuration variable")
	default:
		log.WithError(err).Error("instance configuration request failed")
		return response.InternalError(c)
	}
}

func (h *InstanceConfigHandler) List(c *echo.Context) error {
	if _, ok := h.allowed(c); !ok {
		return response.Forbidden(c)
	}
	entries, err := h.svc.List(c.Request().Context())
	if err != nil {
		return instanceConfigError(c, err)
	}
	out := dto.InstanceConfigResponse{Groups: config.Groups(), Variables: make([]dto.InstanceConfigVariableResponse, 0, len(entries))}
	for _, entry := range entries {
		out.Variables = append(out.Variables, instanceConfigVariable(entry))
	}
	return response.Success(c, http.StatusOK, out)
}

// Update overrides a runtime variable: PUT {"value": "..."}.
func (h *InstanceConfigHandler) Update(c *echo.Context) error {
	user, ok := h.allowed(c)
	if !ok {
		return response.Forbidden(c)
	}
	var req dto.UpdateInstanceConfigRequest
	if err := bindConversation(c, &req); err != nil || req.Value == nil {
		return response.Error(c, http.StatusBadRequest, "INVALID_CONFIG_VALUE", "A value is required")
	}
	entry, err := h.svc.Set(c.Request().Context(), user, c.Param("name"), *req.Value)
	if err != nil {
		return instanceConfigError(c, err)
	}
	return response.Success(c, http.StatusOK, instanceConfigVariable(*entry))
}

// Reset returns a runtime variable to its environment value or default.
func (h *InstanceConfigHandler) Reset(c *echo.Context) error {
	user, ok := h.allowed(c)
	if !ok {
		return response.Forbidden(c)
	}
	entry, err := h.svc.Reset(c.Request().Context(), user, c.Param("name"))
	if err != nil {
		return instanceConfigError(c, err)
	}
	return response.Success(c, http.StatusOK, instanceConfigVariable(*entry))
}

func instanceConfigVariable(entry service.InstanceConfigEntry) dto.InstanceConfigVariableResponse {
	out := dto.InstanceConfigVariableResponse{
		Name: entry.Name, Group: entry.Group, Description: entry.Description, Kind: string(entry.Kind),
		Secret: entry.Secret, Set: entry.Set, Withheld: entry.Withheld, Source: string(entry.Source),
		Tunable: entry.Runtime != nil, UpdatedAt: entry.UpdatedAt,
	}
	if entry.Secret {
		// Belt and braces: the registry never fills a secret's value.
		return out
	}
	out.Value, out.Default = entry.Value, entry.Default
	if entry.Runtime != nil {
		out.BaseValue, out.BaseSource = entry.BaseValue, string(entry.BaseSource)
		if entry.Runtime.Kind == config.KindDuration {
			out.Min, out.Max = config.FormatDuration(entry.Runtime.Min), config.FormatDuration(entry.Runtime.Max)
		}
	}
	return out
}
