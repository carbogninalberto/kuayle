package handler

import (
	"errors"
	"io"
	"net/http"

	"github.com/gabriel-vasile/mimetype"
	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/kuayle/kuayle-backend/pkg/response"
	"github.com/labstack/echo/v5"
)

// UploadLogo stores an uploaded image (multipart field "file") as the
// workspace logo.
func (h *WorkspaceHandler) UploadLogo(c *echo.Context) error {
	c.Request().Body = http.MaxBytesReader(c.Response(), c.Request().Body, domain.WorkspaceLogoMaxBytes+64*1024)

	file, err := c.FormFile("file")
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			return response.Error(c, http.StatusRequestEntityTooLarge, "WORKSPACE_LOGO_TOO_LARGE", "Image too large (max 2MB)")
		}
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "No file provided")
	}
	if file.Size > domain.WorkspaceLogoMaxBytes {
		return response.Error(c, http.StatusRequestEntityTooLarge, "WORKSPACE_LOGO_TOO_LARGE", "Image too large (max 2MB)")
	}
	src, err := file.Open()
	if err != nil {
		return response.InternalError(c)
	}
	defer src.Close()

	detected, err := mimetype.DetectReader(src)
	if err != nil {
		return response.InternalError(c)
	}
	ext, ok := domain.AvatarImageTypes[detected.String()]
	if !ok {
		return response.Error(c, http.StatusBadRequest, "UNSUPPORTED_IMAGE", "Use a PNG, JPEG, WebP or GIF image")
	}
	if _, err := src.Seek(0, io.SeekStart); err != nil {
		return response.InternalError(c)
	}

	ws, err := h.workspaceSvc.UploadLogo(c.Request().Context(), c.Param("slug"), middleware.GetUserID(c), src, detected.String(), ext)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrWorkspaceNotFound):
			return response.NotFound(c, "Workspace")
		case errors.Is(err, service.ErrNotWorkspaceOwner):
			return response.Error(c, http.StatusForbidden, "FORBIDDEN", "Only the workspace owner can edit workspace settings")
		case errors.Is(err, service.ErrWorkspaceLogoUnavailable):
			return response.Error(c, http.StatusServiceUnavailable, "UNAVAILABLE", err.Error())
		default:
			return response.InternalError(c)
		}
	}
	return response.Success(c, http.StatusOK, h.toWorkspaceResponse(c, *ws))
}

// Logo serves a workspace's stored logo. It is public because the invite
// preview shows the logo before sign-in.
func (h *WorkspaceHandler) Logo(c *echo.Context) error {
	workspaceID, err := uuid.Parse(c.Param("workspaceId"))
	if err != nil {
		return response.NotFound(c, "Logo")
	}
	rc, contentType, err := h.workspaceSvc.Logo(c.Request().Context(), workspaceID, c.Param("file"))
	if err != nil {
		return response.NotFound(c, "Logo")
	}
	defer rc.Close()
	c.Response().Header().Set("Cache-Control", "public, max-age=86400, immutable")
	c.Response().Header().Set("X-Content-Type-Options", "nosniff")
	return c.Stream(http.StatusOK, contentType, rc)
}
