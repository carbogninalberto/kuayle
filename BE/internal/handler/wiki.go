package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/kuayle/kuayle-backend/pkg/response"
	"github.com/labstack/echo/v5"
)

// WikiHandler serves the team and project wikis. Each method returns the
// handler for one scope kind; the scope ID always comes from the route.
type WikiHandler struct {
	wikiSvc *service.WikiService
	ai      wikiAI
}

// wikiAI rewrites a selection of a page with the workspace's AI settings.
type wikiAI interface {
	ReworkWikiSelection(ctx context.Context, workspaceID uuid.UUID, title, content, selectedText string) (string, error)
}

func NewWikiHandler(wikiSvc *service.WikiService, ai wikiAI) *WikiHandler {
	return &WikiHandler{wikiSvc: wikiSvc, ai: ai}
}

// wikiScopeParam names the route parameter that carries each scope's ID.
var wikiScopeParam = map[domain.WikiScopeKind]string{
	domain.WikiScopeTeam:    "teamId",
	domain.WikiScopeProject: "id",
}

func wikiScopeFromRoute(c *echo.Context, kind domain.WikiScopeKind) (domain.WikiScope, bool) {
	id, err := uuid.Parse(c.Param(wikiScopeParam[kind]))
	scope := domain.WikiScope{Kind: kind, ID: id}
	return scope, err == nil && scope.Valid()
}

func (h *WikiHandler) List(kind domain.WikiScopeKind) echo.HandlerFunc {
	return func(c *echo.Context) error {
		scope, ok := wikiScopeFromRoute(c, kind)
		if !ok {
			return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid "+string(kind)+" ID")
		}
		pages, err := h.wikiSvc.List(c.Request().Context(), middleware.GetWorkspace(c).ID, scope)
		if err != nil {
			return wikiErrorResponse(c, err)
		}
		resp := make([]dto.WikiPageSummaryResponse, len(pages))
		for i, page := range pages {
			resp[i] = toWikiPageSummary(page)
		}
		return response.Success(c, http.StatusOK, resp)
	}
}

func (h *WikiHandler) Create(kind domain.WikiScopeKind) echo.HandlerFunc {
	return func(c *echo.Context) error {
		scope, ok := wikiScopeFromRoute(c, kind)
		if !ok {
			return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid "+string(kind)+" ID")
		}
		var req dto.CreateWikiPageRequest
		if err := c.Bind(&req); err != nil {
			return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid request body")
		}
		page, err := h.wikiSvc.Create(c.Request().Context(), middleware.GetWorkspace(c).ID, scope, middleware.GetUserID(c), req)
		if err != nil {
			return wikiErrorResponse(c, err)
		}
		return response.Success(c, http.StatusCreated, toWikiPage(*page))
	}
}

func (h *WikiHandler) Get(kind domain.WikiScopeKind) echo.HandlerFunc {
	return func(c *echo.Context) error {
		scope, pageID, ok := wikiPageFromRoute(c, kind)
		if !ok {
			return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid wiki page ID")
		}
		page, err := h.wikiSvc.Get(c.Request().Context(), middleware.GetWorkspace(c).ID, scope, pageID)
		if err != nil {
			return wikiErrorResponse(c, err)
		}
		return response.Success(c, http.StatusOK, toWikiPage(*page))
	}
}

func (h *WikiHandler) Update(kind domain.WikiScopeKind) echo.HandlerFunc {
	return func(c *echo.Context) error {
		scope, pageID, ok := wikiPageFromRoute(c, kind)
		if !ok {
			return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid wiki page ID")
		}
		var req dto.UpdateWikiPageRequest
		if err := c.Bind(&req); err != nil {
			return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid request body")
		}
		page, err := h.wikiSvc.Update(c.Request().Context(), middleware.GetWorkspace(c).ID, scope, pageID, middleware.GetUserID(c), req)
		if err != nil {
			return wikiErrorResponse(c, err)
		}
		return response.Success(c, http.StatusOK, toWikiPage(*page))
	}
}

func (h *WikiHandler) Delete(kind domain.WikiScopeKind) echo.HandlerFunc {
	return func(c *echo.Context) error {
		scope, pageID, ok := wikiPageFromRoute(c, kind)
		if !ok {
			return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid wiki page ID")
		}
		if err := h.wikiSvc.Delete(c.Request().Context(), middleware.GetWorkspace(c).ID, scope, pageID, middleware.GetUserID(c)); err != nil {
			return wikiErrorResponse(c, err)
		}
		return response.Success(c, http.StatusOK, map[string]string{"status": "deleted"})
	}
}

// Revisions lists what earlier saves changed on a page, newest first.
func (h *WikiHandler) Revisions(kind domain.WikiScopeKind) echo.HandlerFunc {
	return func(c *echo.Context) error {
		scope, pageID, ok := wikiPageFromRoute(c, kind)
		if !ok {
			return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid wiki page ID")
		}
		revisions, err := h.wikiSvc.Revisions(c.Request().Context(), middleware.GetWorkspace(c).ID, scope, pageID)
		if err != nil {
			return wikiErrorResponse(c, err)
		}
		resp := make([]dto.WikiRevisionResponse, len(revisions))
		for i, revision := range revisions {
			resp[i] = dto.WikiRevisionResponse{
				ID:           revision.ID.String(),
				Version:      revision.Version,
				OldTitle:     revision.OldTitle,
				NewTitle:     revision.NewTitle,
				OldContent:   revision.OldContent,
				NewContent:   revision.NewContent,
				EditedBy:     optionalUUIDString(revision.EditedBy),
				EditedByName: revision.EditedByName,
				CreatedAt:    revision.CreatedAt,
				UpdatedAt:    revision.UpdatedAt,
			}
		}
		return response.Success(c, http.StatusOK, resp)
	}
}

// Rework rewrites selected text of a saved page with the workspace's AI
// model, like an issue description's rework. The page is read through the
// wiki service first, so only callers who can see it reach the model.
func (h *WikiHandler) Rework(kind domain.WikiScopeKind) echo.HandlerFunc {
	return func(c *echo.Context) error {
		scope, pageID, ok := wikiPageFromRoute(c, kind)
		if !ok {
			return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid wiki page ID")
		}
		var req dto.WikiReworkRequest
		if ok, err := bindExpansionRequest(c, &req); !ok {
			return err
		}
		ws := middleware.GetWorkspace(c)
		page, err := h.wikiSvc.Get(c.Request().Context(), ws.ID, scope, pageID)
		if err != nil {
			return wikiErrorResponse(c, err)
		}
		text, err := h.ai.ReworkWikiSelection(c.Request().Context(), ws.ID, page.Title, page.Content, req.SelectedText)
		return expansionResponse(c, text, err)
	}
}

// Settings reports the wiki's opt-in features.
func (h *WikiHandler) Settings(kind domain.WikiScopeKind) echo.HandlerFunc {
	return func(c *echo.Context) error {
		scope, ok := wikiScopeFromRoute(c, kind)
		if !ok {
			return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid "+string(kind)+" ID")
		}
		enabled, err := h.wikiSvc.RegistryEnabled(c.Request().Context(), middleware.GetWorkspace(c).ID, scope)
		if err != nil {
			return wikiErrorResponse(c, err)
		}
		return response.Success(c, http.StatusOK, dto.WikiSettingsResponse{RegistryEnabled: enabled})
	}
}

// UpdateSettings turns the wiki's registry on or off.
func (h *WikiHandler) UpdateSettings(kind domain.WikiScopeKind) echo.HandlerFunc {
	return func(c *echo.Context) error {
		scope, ok := wikiScopeFromRoute(c, kind)
		if !ok {
			return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid "+string(kind)+" ID")
		}
		var req dto.WikiSettingsRequest
		if err := c.Bind(&req); err != nil {
			return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid request body")
		}
		if req.RegistryEnabled == nil {
			return response.ValidationError(c, []dto.ErrorDetail{{Field: "registry_enabled", Message: "is required"}})
		}
		ws := middleware.GetWorkspace(c)
		if err := h.wikiSvc.SetRegistryEnabled(c.Request().Context(), ws.ID, scope, middleware.GetUserID(c), *req.RegistryEnabled); err != nil {
			return wikiErrorResponse(c, err)
		}
		return response.Success(c, http.StatusOK, dto.WikiSettingsResponse{RegistryEnabled: *req.RegistryEnabled})
	}
}

// AppendRegistry adds an entry to today's (UTC) registry page.
func (h *WikiHandler) AppendRegistry(kind domain.WikiScopeKind) echo.HandlerFunc {
	return func(c *echo.Context) error {
		scope, ok := wikiScopeFromRoute(c, kind)
		if !ok {
			return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid "+string(kind)+" ID")
		}
		var req dto.WikiRegistryRequest
		if err := c.Bind(&req); err != nil {
			return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid request body")
		}
		page, err := h.wikiSvc.AppendRegistry(c.Request().Context(), middleware.GetWorkspace(c).ID, scope, middleware.GetUserID(c), req.Text)
		if err != nil {
			return wikiErrorResponse(c, err)
		}
		return response.Success(c, http.StatusOK, toWikiPage(*page))
	}
}

// RegistryFeed lists registry pages, newest day first, a page of days at a
// time: ?before=YYYY-MM-DD (exclusive) and ?limit=1..31 (default 7).
func (h *WikiHandler) RegistryFeed(kind domain.WikiScopeKind) echo.HandlerFunc {
	return func(c *echo.Context) error {
		scope, ok := wikiScopeFromRoute(c, kind)
		if !ok {
			return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid "+string(kind)+" ID")
		}
		var before *time.Time
		if raw := c.QueryParam("before"); raw != "" {
			day, err := time.Parse(time.DateOnly, raw)
			if err != nil {
				return response.ValidationError(c, []dto.ErrorDetail{{Field: "before", Message: "must be a date (YYYY-MM-DD)"}})
			}
			before = &day
		}
		limit := 0
		if raw := c.QueryParam("limit"); raw != "" {
			value, err := strconv.Atoi(raw)
			if err != nil || value < 1 {
				return response.ValidationError(c, []dto.ErrorDetail{{Field: "limit", Message: "must be 1 to 31"}})
			}
			limit = value
		}
		pages, next, err := h.wikiSvc.RegistryDays(c.Request().Context(), middleware.GetWorkspace(c).ID, scope, before, limit)
		if err != nil {
			return wikiErrorResponse(c, err)
		}
		resp := dto.WikiRegistryFeedResponse{Days: make([]dto.WikiRegistryDay, len(pages)), NextBefore: registryDate(next)}
		for i, page := range pages {
			resp.Days[i] = dto.WikiRegistryDay{Date: *registryDate(page.RegistryDate), PageID: page.ID.String(), Title: page.Title, Content: page.Content, Version: page.Version, UpdatedAt: page.UpdatedAt}
		}
		return response.Success(c, http.StatusOK, resp)
	}
}

func registryDate(day *time.Time) *string {
	if day == nil {
		return nil
	}
	value := day.UTC().Format(time.DateOnly)
	return &value
}

func wikiPageFromRoute(c *echo.Context, kind domain.WikiScopeKind) (domain.WikiScope, uuid.UUID, bool) {
	scope, ok := wikiScopeFromRoute(c, kind)
	pageID, err := uuid.Parse(c.Param("pageId"))
	return scope, pageID, ok && err == nil
}

func wikiErrorResponse(c *echo.Context, err error) error {
	var invalid *service.WikiValidationError
	switch {
	case errors.As(err, &invalid):
		return response.ValidationError(c, []dto.ErrorDetail{{Field: invalid.Field, Message: invalid.Message}})
	case errors.Is(err, service.ErrWikiNotFound):
		return response.NotFound(c, "Wiki page")
	case errors.Is(err, service.ErrWikiRegistryDisabled):
		return response.Error(c, http.StatusConflict, "WIKI_REGISTRY_DISABLED", "The registry of this wiki is turned off.")
	case errors.Is(err, service.ErrWikiConflict):
		return response.Error(c, http.StatusConflict, "WIKI_VERSION_CONFLICT", "This page was changed by someone else. Reload it before saving.")
	}
	return response.InternalError(c)
}

func optionalUUIDString(id *uuid.UUID) *string {
	if id == nil {
		return nil
	}
	value := id.String()
	return &value
}

func toWikiPageSummary(page domain.WikiPage) dto.WikiPageSummaryResponse {
	return dto.WikiPageSummaryResponse{
		ID:            page.ID.String(),
		TeamID:        optionalUUIDString(page.TeamID),
		ProjectID:     optionalUUIDString(page.ProjectID),
		Title:         page.Title,
		Version:       page.Version,
		UpdatedBy:     optionalUUIDString(page.UpdatedBy),
		UpdatedByName: page.UpdatedByName,
		RegistryDate:  registryDate(page.RegistryDate),
		CreatedAt:     page.CreatedAt,
		UpdatedAt:     page.UpdatedAt,
	}
}

func toWikiPage(page domain.WikiPage) dto.WikiPageResponse {
	return dto.WikiPageResponse{
		WikiPageSummaryResponse: toWikiPageSummary(page),
		Content:                 page.Content,
		CreatedBy:               optionalUUIDString(page.CreatedBy),
	}
}
