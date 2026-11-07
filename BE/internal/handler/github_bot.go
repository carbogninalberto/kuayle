package handler

import (
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/kuayle/kuayle-backend/pkg/response"
	"github.com/labstack/echo/v5"
	log "github.com/sirupsen/logrus"
)

// GitHubBotHandler serves the workspace's GitHub bot identity and abilities.
type GitHubBotHandler struct {
	svc *service.GitHubBotService
}

func NewGitHubBotHandler(svc *service.GitHubBotService) *GitHubBotHandler {
	return &GitHubBotHandler{svc: svc}
}

func githubBotError(c *echo.Context, err error) error {
	var invalid *service.GitHubBotValidationError
	switch {
	case errors.As(err, &invalid):
		status := http.StatusBadRequest
		if invalid.Code == "AVATAR_TOO_LARGE" {
			status = http.StatusRequestEntityTooLarge
		}
		return response.Error(c, status, invalid.Code, invalid.Message)
	case errors.Is(err, service.ErrGitHubBotAvatarStorageUnavailable):
		return response.Error(c, http.StatusServiceUnavailable, "UNAVAILABLE", "Picture uploads are not available")
	}
	log.WithError(err).Error("GitHub bot settings request failed")
	return response.InternalError(c)
}

// Get returns the bot's identity and ability toggles to non-guest members.
func (h *GitHubBotHandler) Get(c *echo.Context) error {
	if middleware.GetWorkspaceRole(c) == string(domain.RoleGuest) {
		return response.Forbidden(c)
	}
	settings, err := h.svc.Settings(c.Request().Context(), middleware.GetWorkspace(c).ID)
	if err != nil {
		return githubBotError(c, err)
	}
	return response.Success(c, http.StatusOK, settings)
}

// Update changes the fields the request sets.
func (h *GitHubBotHandler) Update(c *echo.Context) error {
	var req dto.UpdateGitHubBotSettingsRequest
	if err := c.Bind(&req); err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid request body")
	}
	settings, err := h.svc.Update(c.Request().Context(), middleware.GetWorkspace(c).ID, middleware.GetUserID(c), req)
	if err != nil {
		return githubBotError(c, err)
	}
	return response.Success(c, http.StatusOK, settings)
}

// UploadAvatar stores an uploaded picture (multipart field "file") as the bot's avatar.
func (h *GitHubBotHandler) UploadAvatar(c *echo.Context) error {
	c.Request().Body = http.MaxBytesReader(c.Response(), c.Request().Body, domain.GitHubBotAvatarMaxBytes+64*1024)
	file, err := c.FormFile("file")
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return response.Error(c, http.StatusRequestEntityTooLarge, "AVATAR_TOO_LARGE", "Image too large (max 2MB)")
		}
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "No file provided")
	}
	if file.Size > domain.GitHubBotAvatarMaxBytes {
		return response.Error(c, http.StatusRequestEntityTooLarge, "AVATAR_TOO_LARGE", "Image too large (max 2MB)")
	}
	src, err := file.Open()
	if err != nil {
		return response.InternalError(c)
	}
	defer src.Close()
	settings, err := h.svc.UploadAvatar(c.Request().Context(), middleware.GetWorkspace(c).ID, middleware.GetUserID(c), src)
	if err != nil {
		return githubBotError(c, err)
	}
	return response.Success(c, http.StatusOK, settings)
}

// Avatar serves the workspace's uploaded bot picture without authentication:
// GitHub must load it to show it in the bot's comments. Only the picture the
// settings currently reference is served.
func (h *GitHubBotHandler) Avatar(c *echo.Context) error {
	ws, err := uuid.Parse(c.Param("workspaceId"))
	if err != nil {
		return response.NotFound(c, "Avatar")
	}
	rc, contentType, err := h.svc.Avatar(c.Request().Context(), ws, c.Param("file"))
	if err != nil {
		return response.NotFound(c, "Avatar")
	}
	defer rc.Close()
	c.Response().Header().Set("Cache-Control", "public, max-age=3600")
	c.Response().Header().Set("X-Content-Type-Options", "nosniff")
	c.Response().Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	return c.Stream(http.StatusOK, contentType, rc)
}
