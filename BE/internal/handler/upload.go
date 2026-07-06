package handler

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/pkg/assettoken"
	"github.com/kuayle/kuayle-backend/pkg/response"
	"github.com/kuayle/kuayle-backend/pkg/storage"
)

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
		"url":      assetURL,
		"asset_id": asset.ID.String(),
		"filename": asset.Filename,
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
	return h.streamAsset(c, asset)
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
