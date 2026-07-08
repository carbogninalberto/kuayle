package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/pkg/response"
	"github.com/labstack/echo/v4"
	log "github.com/sirupsen/logrus"
)

type SystemHandler struct {
	updaterURL   string
	updaterToken string
	isSysAdmin   func(uuid.UUID) bool
	client       *http.Client
}

func NewSystemHandler(updaterURL, updaterToken string, isSysAdmin func(uuid.UUID) bool) *SystemHandler {
	if isSysAdmin == nil {
		isSysAdmin = func(uuid.UUID) bool { return false }
	}
	return &SystemHandler{
		updaterURL:   strings.TrimRight(strings.TrimSpace(updaterURL), "/"),
		updaterToken: strings.TrimSpace(updaterToken),
		isSysAdmin:   isSysAdmin,
		client:       &http.Client{Timeout: 10 * time.Second},
	}
}

func (h *SystemHandler) UpdateStatus(c echo.Context) error {
	if !h.isCurrentUserSysAdmin(c) {
		return response.Forbidden(c)
	}
	if !h.updaterConfigured() {
		return response.Success(c, http.StatusOK, dto.SystemUpdateStatusResponse{
			Enabled: false,
			Running: false,
			Message: "System updater is not configured",
		})
	}

	ctx, cancel := context.WithTimeout(c.Request().Context(), 10*time.Second)
	defer cancel()

	var status dto.SystemUpdateStatusResponse
	statusCode, err := h.callUpdater(ctx, http.MethodGet, "/status", &status)
	if err != nil {
		log.WithError(err).Warn("system updater status request failed")
		return response.Error(c, http.StatusBadGateway, "UPDATER_UNAVAILABLE", "System updater is unavailable")
	}
	if statusCode != http.StatusOK {
		log.WithField("status", statusCode).Warn("system updater status request returned unexpected status")
		return response.Error(c, http.StatusBadGateway, "UPDATER_UNAVAILABLE", "System updater is unavailable")
	}

	status.Enabled = true
	return response.Success(c, http.StatusOK, status)
}

func (h *SystemHandler) StartUpdate(c echo.Context) error {
	userID := middleware.GetUserID(c)
	if !h.isSysAdmin(userID) {
		return response.Forbidden(c)
	}
	if !h.updaterConfigured() {
		return response.Error(c, http.StatusServiceUnavailable, "UPDATER_DISABLED", "System updater is not configured")
	}

	ctx, cancel := context.WithTimeout(c.Request().Context(), 10*time.Second)
	defer cancel()

	var result dto.SystemUpdateStartResponse
	statusCode, err := h.callUpdater(ctx, http.MethodPost, "/update", &result)
	if err != nil {
		log.WithError(err).WithField("user_id", userID).Warn("system update request failed")
		return response.Error(c, http.StatusBadGateway, "UPDATER_UNAVAILABLE", "System updater is unavailable")
	}

	switch statusCode {
	case http.StatusAccepted, http.StatusOK:
		log.WithField("user_id", userID).Warn("system update started")
		if result.Message == "" {
			result.Message = "System update started"
		}
		return response.Success(c, http.StatusAccepted, result)
	case http.StatusConflict:
		return response.Error(c, http.StatusConflict, "UPDATE_RUNNING", fallbackMessage(result.Message, "A system update is already running"))
	case http.StatusUnauthorized, http.StatusForbidden:
		log.WithField("status", statusCode).Error("system updater rejected backend token")
		return response.Error(c, http.StatusBadGateway, "UPDATER_AUTH_FAILED", "System updater authentication failed")
	default:
		log.WithField("status", statusCode).Warn("system updater returned unexpected status")
		return response.Error(c, http.StatusBadGateway, "UPDATER_FAILED", fallbackMessage(result.Message, "System updater failed to start"))
	}
}

func (h *SystemHandler) updaterConfigured() bool {
	return h.updaterURL != "" && h.updaterToken != ""
}

func (h *SystemHandler) isCurrentUserSysAdmin(c echo.Context) bool {
	return h.isSysAdmin(middleware.GetUserID(c))
}

func (h *SystemHandler) callUpdater(ctx context.Context, method, path string, target any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, method, h.updaterURL+path, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+h.updaterToken)

	res, err := h.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()

	if target != nil {
		_ = json.NewDecoder(io.LimitReader(res.Body, 64*1024)).Decode(target)
	}

	return res.StatusCode, nil
}

func fallbackMessage(message, fallback string) string {
	if strings.TrimSpace(message) == "" {
		return fallback
	}
	return message
}
