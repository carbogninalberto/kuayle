package handler

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/gabriel-vasile/mimetype"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/pkg/assettoken"
	"github.com/kuayle/kuayle-backend/pkg/response"
	"github.com/kuayle/kuayle-backend/pkg/storage"
)

const maxUploadSize = 10 * 1024 * 1024

var allowedUploadExtensions = map[string]map[string]bool{
	"image/jpeg":    {".jpg": true, ".jpeg": true},
	"image/png":     {".png": true},
	"image/gif":     {".gif": true},
	"image/webp":    {".webp": true},
	"image/svg+xml": {".svg": true},

	"application/pdf":  {".pdf": true},
	"text/plain":       {".txt": true, ".log": true},
	"text/csv":         {".csv": true},
	"text/markdown":    {".md": true, ".markdown": true},
	"application/json": {".json": true},
	"text/rtf":         {".rtf": true},

	"application/msword": {".doc": true},
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document": {".docx": true},
	"application/vnd.ms-excel": {".xls": true},
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":         {".xlsx": true},
	"application/vnd.ms-powerpoint":                                             {".ppt": true},
	"application/vnd.openxmlformats-officedocument.presentationml.presentation": {".pptx": true},

	"application/zip":             {".zip": true},
	"application/gzip":            {".gz": true, ".gzip": true},
	"application/x-tar":           {".tar": true},
	"application/x-7z-compressed": {".7z": true},
	"application/vnd.rar":         {".rar": true},
}

type UploadHandler struct {
	store       storage.Backend
	assetRepo   repository.AssetRepo
	issueRepo   repository.IssueRepo
	tokenSecret string
}

func NewUploadHandler(store storage.Backend, assetRepo repository.AssetRepo, issueRepo repository.IssueRepo, jwtSecret string) *UploadHandler {
	return &UploadHandler{
		store:       store,
		assetRepo:   assetRepo,
		issueRepo:   issueRepo,
		tokenSecret: jwtSecret + ":prompt-assets",
	}
}

func (h *UploadHandler) Upload(c echo.Context) error {
	ws := c.Get("workspace").(*domain.Workspace)
	userID := middleware.GetUserID(c)
	c.Request().Body = http.MaxBytesReader(c.Response().Writer, c.Request().Body, maxUploadSize+1024*1024)

	file, err := c.FormFile("file")
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			return response.Error(c, http.StatusRequestEntityTooLarge, "BAD_REQUEST", "File too large (max 10MB)")
		}
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "No file provided")
	}

	// Validate file size (max 10MB)
	if file.Size > maxUploadSize {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "File too large (max 10MB)")
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
	if _, err := src.Seek(0, io.SeekStart); err != nil {
		return response.InternalError(c)
	}

	ext := strings.ToLower(filepath.Ext(file.Filename))
	contentType, ok := allowedUploadType(detected.String(), ext)
	if !ok {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Unsupported file type")
	}

	// Generate unique filename
	if ext == "" {
		ext = detected.Extension()
	}
	key := fmt.Sprintf("%s%s", uuid.New().String(), ext)

	if _, err := h.store.Put(c.Request().Context(), key, src, contentType); err != nil {
		return response.InternalError(c)
	}

	asset := &domain.Asset{
		ID:          uuid.New(),
		WorkspaceID: ws.ID,
		StorageKey:  key,
		Filename:    filepath.Base(file.Filename),
		ContentType: contentType,
		Size:        file.Size,
		UploadedBy:  userID,
	}
	if err := h.assetRepo.Create(c.Request().Context(), asset); err != nil {
		return response.InternalError(c)
	}

	assetURL := fmt.Sprintf("/api/workspaces/%s/assets/%s", ws.Slug, asset.ID)

	return response.Success(c, http.StatusOK, map[string]string{
		"url":          assetURL,
		"asset_id":     asset.ID.String(),
		"filename":     asset.Filename,
		"content_type": asset.ContentType,
	})
}

func (h *UploadHandler) GetAsset(c echo.Context) error {
	ws := c.Get("workspace").(*domain.Workspace)
	assetID, err := uuid.Parse(c.Param("assetId"))
	if err != nil {
		return response.NotFound(c, "Asset")
	}

	asset, err := h.assetRepo.GetByID(c.Request().Context(), assetID)
	if err != nil || asset == nil || asset.WorkspaceID != ws.ID {
		return response.NotFound(c, "Asset")
	}

	c.Response().Header().Set("Cache-Control", "private, max-age=300")
	h.setAssetDisposition(c, asset)
	return h.streamAsset(c, asset)
}

func (h *UploadHandler) PublicAsset(c echo.Context) error {
	claims, err := assettoken.Validate(c.Param("token"), h.tokenSecret)
	if err != nil {
		return response.NotFound(c, "Asset")
	}

	asset, err := h.assetRepo.GetByID(c.Request().Context(), claims.AssetID)
	if err != nil || asset == nil || asset.WorkspaceID != claims.WorkspaceID {
		return response.NotFound(c, "Asset")
	}

	c.Response().Header().Set("Cache-Control", "private, max-age=3600")
	h.setAssetDisposition(c, asset)
	return h.streamAsset(c, asset)
}

func allowedUploadType(detectedType, ext string) (string, bool) {
	if detectedType == "text/plain" {
		switch ext {
		case ".csv":
			detectedType = "text/csv"
		case ".md", ".markdown":
			detectedType = "text/markdown"
		}
	}
	extensions, ok := allowedUploadExtensions[detectedType]
	if !ok {
		return "", false
	}
	if ext != "" && !extensions[ext] {
		return "", false
	}
	return detectedType, true
}

func (h *UploadHandler) setAssetDisposition(c echo.Context, asset *domain.Asset) {
	if c.QueryParam("download") == "1" || !strings.HasPrefix(asset.ContentType, "image/") {
		c.Response().Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": asset.Filename}))
	}
}

func (h *UploadHandler) SignIssuePromptAssets(c echo.Context) error {
	ws := c.Get("workspace").(*domain.Workspace)
	issue, err := h.issueRepo.GetByIdentifier(c.Request().Context(), ws.ID, c.Param("identifier"))
	if err != nil || issue == nil {
		return response.NotFound(c, "Issue")
	}

	signed := make(map[string]string)
	expiresAt := time.Now().Add(time.Hour)
	if issue.Description != nil {
		for _, source := range extractProtectedAssetSources(*issue.Description) {
			asset, err := h.assetRepo.GetByID(c.Request().Context(), source.assetID)
			if err != nil || asset == nil || asset.WorkspaceID != ws.ID {
				continue
			}

			token, exp, err := assettoken.Generate(h.tokenSecret, asset.ID, ws.ID, issue.ID, time.Hour)
			if err != nil {
				return response.InternalError(c)
			}
			expiresAt = exp
			signed[source.raw] = "/api/public/assets/" + token
		}
	}

	return response.Success(c, http.StatusOK, map[string]interface{}{
		"assets":     signed,
		"expires_at": expiresAt.Format(time.RFC3339),
	})
}

func (h *UploadHandler) streamAsset(c echo.Context, asset *domain.Asset) error {
	rc, err := h.store.Get(c.Request().Context(), asset.StorageKey)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return response.NotFound(c, "Asset")
		}
		return response.NotFound(c, "Asset")
	}
	defer rc.Close()
	c.Response().Header().Set("Content-Type", asset.ContentType)
	c.Response().Header().Set("X-Content-Type-Options", "nosniff")
	return c.Stream(http.StatusOK, asset.ContentType, rc)
}

type protectedAssetSource struct {
	raw     string
	assetID uuid.UUID
}

var (
	imgSrcPattern      = regexp.MustCompile(`(?i)<img[^>]+src=["']([^"']+)["']`)
	protectedPathRegex = regexp.MustCompile(`^/api/workspaces/[^/]+/assets/([0-9a-fA-F-]{36})$`)
)

func extractProtectedAssetSources(html string) []protectedAssetSource {
	matches := imgSrcPattern.FindAllStringSubmatch(html, -1)
	sources := make([]protectedAssetSource, 0, len(matches))
	seen := make(map[string]bool)
	for _, match := range matches {
		if len(match) < 2 || seen[match[1]] {
			continue
		}
		path := match[1]
		if u, err := url.Parse(path); err == nil && u.IsAbs() {
			path = u.Path
		}
		pathMatches := protectedPathRegex.FindStringSubmatch(path)
		if len(pathMatches) != 2 {
			continue
		}
		assetID, err := uuid.Parse(pathMatches[1])
		if err != nil {
			continue
		}
		seen[match[1]] = true
		sources = append(sources, protectedAssetSource{raw: match[1], assetID: assetID})
	}
	return sources
}
