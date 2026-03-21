package handler

import (
	"fmt"
	"net/http"
	"path/filepath"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/kuayle/kuayle-backend/pkg/response"
	"github.com/kuayle/kuayle-backend/pkg/storage"
)

type UploadHandler struct {
	store storage.Backend
}

func NewUploadHandler(store storage.Backend) *UploadHandler {
	return &UploadHandler{store: store}
}

func (h *UploadHandler) Upload(c echo.Context) error {
	file, err := c.FormFile("file")
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "No file provided")
	}

	// Validate file size (max 10MB)
	if file.Size > 10*1024*1024 {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "File too large (max 10MB)")
	}

	// Validate content type
	contentType := file.Header.Get("Content-Type")
	allowedTypes := map[string]bool{
		"image/jpeg":    true,
		"image/png":     true,
		"image/gif":     true,
		"image/webp":    true,
		"image/svg+xml": true,
	}
	if !allowedTypes[contentType] {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Unsupported file type")
	}

	// Generate unique filename
	ext := filepath.Ext(file.Filename)
	if ext == "" {
		switch contentType {
		case "image/jpeg":
			ext = ".jpg"
		case "image/png":
			ext = ".png"
		case "image/gif":
			ext = ".gif"
		case "image/webp":
			ext = ".webp"
		case "image/svg+xml":
			ext = ".svg"
		}
	}
	key := fmt.Sprintf("%s%s", uuid.New().String(), ext)

	src, err := file.Open()
	if err != nil {
		return response.InternalError(c)
	}
	defer src.Close()

	if _, err := h.store.Put(c.Request().Context(), key, src, contentType); err != nil {
		return response.InternalError(c)
	}

	url, err := h.store.URL(c.Request().Context(), key)
	if err != nil {
		return response.InternalError(c)
	}

	return response.Success(c, http.StatusOK, map[string]string{
		"url":      url,
		"filename": file.Filename,
	})
}
