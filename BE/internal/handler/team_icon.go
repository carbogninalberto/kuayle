package handler

import (
	"errors"
	"io"
	"net/http"

	"github.com/gabriel-vasile/mimetype"
	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/kuayle/kuayle-backend/pkg/response"
	"github.com/labstack/echo/v5"
)

// UploadIcon stores an uploaded image (multipart field "file") as the team's icon.
func (h *TeamHandler) UploadIcon(c *echo.Context) error {
	teamID, err := uuid.Parse(c.Param("teamId"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid team ID")
	}
	c.Request().Body = http.MaxBytesReader(c.Response(), c.Request().Body, domain.TeamIconMaxBytes+64*1024)

	file, err := c.FormFile("file")
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			return response.Error(c, http.StatusRequestEntityTooLarge, "BAD_REQUEST", "Image too large (max 1MB)")
		}
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "No file provided")
	}
	if file.Size > domain.TeamIconMaxBytes {
		return response.Error(c, http.StatusRequestEntityTooLarge, "BAD_REQUEST", "Image too large (max 1MB)")
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
	ext, ok := domain.TeamIconImageTypes[detected.String()]
	if !ok {
		return response.Error(c, http.StatusBadRequest, "UNSUPPORTED_IMAGE", "Use a PNG, JPEG, WebP, GIF or ICO image")
	}
	if _, err := src.Seek(0, io.SeekStart); err != nil {
		return response.InternalError(c)
	}

	ws := c.Get("workspace").(*domain.Workspace)
	team, err := h.teamSvc.UploadIcon(c.Request().Context(), ws.ID, teamID, src, detected.String(), ext)
	if err != nil {
		if errors.Is(err, service.ErrTeamIconUnavailable) {
			return response.Error(c, http.StatusServiceUnavailable, "UNAVAILABLE", err.Error())
		}
		if errors.Is(err, service.ErrTeamNotFound) {
			return response.NotFound(c, "Team")
		}
		return response.InternalError(c)
	}
	return response.Success(c, http.StatusOK, toTeamResponse(*team))
}

// Icon serves a team's stored icon image to members who can see the team.
func (h *TeamHandler) Icon(c *echo.Context) error {
	teamID, err := uuid.Parse(c.Param("teamId"))
	if err != nil {
		return response.NotFound(c, "Icon")
	}
	ws := c.Get("workspace").(*domain.Workspace)
	file := c.Param("file")
	rc, err := h.teamSvc.Icon(c.Request().Context(), ws.ID, teamID, file)
	if err != nil {
		return response.NotFound(c, "Icon")
	}
	defer rc.Close()
	c.Response().Header().Set("Cache-Control", "private, max-age=86400, immutable")
	c.Response().Header().Set("X-Content-Type-Options", "nosniff")
	return c.Stream(http.StatusOK, domain.TeamIconContentType(file), rc)
}
