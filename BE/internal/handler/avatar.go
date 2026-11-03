package handler

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"

	"github.com/gabriel-vasile/mimetype"
	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/pkg/response"
	"github.com/kuayle/kuayle-backend/pkg/storage"
	"github.com/labstack/echo/v4"
	log "github.com/sirupsen/logrus"
)

const maxAvatarSize = 2 * 1024 * 1024

// avatarTypes lists the raster formats accepted for profile pictures. SVG is
// excluded because it can carry script.
var avatarTypes = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
	"image/webp": ".webp",
	"image/gif":  ".gif",
}

var (
	avatarFilePattern = regexp.MustCompile(`^[0-9a-f-]{36}\.(png|jpg|webp|gif)$`)
	avatarPathPattern = regexp.MustCompile(`^/api/users/([0-9a-f-]{36})/avatar/([0-9a-f-]{36}\.(?:png|jpg|webp|gif))$`)
)

// SetAvatarStore enables profile picture uploads.
func (h *AuthHandler) SetAvatarStore(store storage.Backend) { h.avatarStore = store }

func avatarKey(userID uuid.UUID, file string) string { return "avatar-" + userID.String() + "-" + file }

// UploadAvatar stores the caller's profile picture and points their avatar_url at it.
func (h *AuthHandler) UploadAvatar(c echo.Context) error {
	userID := middleware.GetUserID(c)
	c.Request().Body = http.MaxBytesReader(c.Response().Writer, c.Request().Body, maxAvatarSize+64*1024)

	file, err := c.FormFile("file")
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			return response.Error(c, http.StatusRequestEntityTooLarge, "BAD_REQUEST", "Image too large (max 2MB)")
		}
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "No file provided")
	}
	if file.Size > maxAvatarSize {
		return response.Error(c, http.StatusRequestEntityTooLarge, "BAD_REQUEST", "Image too large (max 2MB)")
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
	ext, ok := avatarTypes[detected.String()]
	if !ok {
		return response.Error(c, http.StatusBadRequest, "UNSUPPORTED_IMAGE", "Use a PNG, JPEG, WebP or GIF image")
	}
	if _, err := src.Seek(0, io.SeekStart); err != nil {
		return response.InternalError(c)
	}

	name := uuid.New().String() + ext
	if h.avatarStore == nil {
		return response.Error(c, http.StatusServiceUnavailable, "UNAVAILABLE", "Avatar uploads are not available")
	}
	ctx := c.Request().Context()
	if _, err := h.avatarStore.Put(ctx, avatarKey(userID, name), src, detected.String()); err != nil {
		return response.InternalError(c)
	}

	previous, err := h.authService.GetUserByID(ctx, userID)
	if err != nil || previous == nil {
		_ = h.avatarStore.Delete(ctx, avatarKey(userID, name))
		return response.NotFound(c, "User")
	}
	var previousURL *string
	if previous.AvatarURL != nil {
		v := *previous.AvatarURL
		previousURL = &v
	}
	url := fmt.Sprintf("/api/users/%s/avatar/%s", userID, name)
	user, err := h.authService.UpdateProfile(ctx, userID, dto.UpdateProfileRequest{AvatarURL: dto.OptionalString{Set: true, Value: &url}})
	if err != nil || user == nil {
		_ = h.avatarStore.Delete(ctx, avatarKey(userID, name))
		return response.InternalError(c)
	}
	h.deleteStored(c, userID, previousURL)
	return response.Success(c, http.StatusOK, h.userResponse(user))
}

// GetAvatar serves a stored profile picture to any signed-in user.
func (h *AuthHandler) GetAvatar(c echo.Context) error {
	if h.avatarStore == nil {
		return response.NotFound(c, "Avatar")
	}
	userID, err := uuid.Parse(c.Param("userId"))
	file := c.Param("file")
	if err != nil || !avatarFilePattern.MatchString(file) {
		return response.NotFound(c, "Avatar")
	}
	rc, err := h.avatarStore.Get(c.Request().Context(), avatarKey(userID, file))
	if err != nil {
		return response.NotFound(c, "Avatar")
	}
	defer rc.Close()
	contentType := "image/png"
	for t, ext := range avatarTypes {
		if strings.HasSuffix(file, ext) {
			contentType = t
		}
	}
	c.Response().Header().Set("Cache-Control", "private, max-age=86400, immutable")
	c.Response().Header().Set("X-Content-Type-Options", "nosniff")
	return c.Stream(http.StatusOK, contentType, rc)
}

// deleteStored removes a previously uploaded avatar owned by the user.
func (h *AuthHandler) deleteStored(c echo.Context, userID uuid.UUID, avatarURL *string) {
	if avatarURL == nil {
		return
	}
	m := avatarPathPattern.FindStringSubmatch(*avatarURL)
	if m == nil || m[1] != userID.String() {
		return
	}
	if err := h.avatarStore.Delete(c.Request().Context(), avatarKey(userID, m[2])); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.WithError(err).Warn("failed to delete previous avatar")
	}
}

// isOwnAvatarPath reports whether rawURL is an uploaded avatar path of userID.
func isOwnAvatarPath(rawURL string, userID uuid.UUID) bool {
	m := avatarPathPattern.FindStringSubmatch(strings.TrimSpace(rawURL))
	return m != nil && m[1] == userID.String()
}
