package handler

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/carbon/carbon-backend/pkg/response"
)

type UploadHandler struct {
	uploadDir string
}

func NewUploadHandler(uploadDir string) *UploadHandler {
	os.MkdirAll(uploadDir, 0755)
	return &UploadHandler{uploadDir: uploadDir}
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
	filename := fmt.Sprintf("%s%s", uuid.New().String(), ext)

	src, err := file.Open()
	if err != nil {
		return response.InternalError(c)
	}
	defer src.Close()

	dstPath := filepath.Join(h.uploadDir, filename)
	dst, err := os.Create(dstPath)
	if err != nil {
		return response.InternalError(c)
	}
	defer dst.Close()

	if _, err = io.Copy(dst, src); err != nil {
		return response.InternalError(c)
	}

	// Build URL - serve from /uploads/
	url := fmt.Sprintf("/uploads/%s", filename)

	return response.Success(c, http.StatusOK, map[string]string{
		"url":      url,
		"filename": file.Filename,
	})
}
