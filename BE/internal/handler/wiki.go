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
)

// WikiHandler serves the team and project wikis. Each method returns the
// handler for one scope kind; the scope ID always comes from the route.
type WikiHandler struct {
	wikiSvc *service.WikiService
}

func NewWikiHandler(wikiSvc *service.WikiService) *WikiHandler {
	return &WikiHandler{wikiSvc: wikiSvc}
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
		if err := h.wikiSvc.Delete(c.Request().Context(), middleware.GetWorkspace(c).ID, scope, pageID); err != nil {
			return wikiErrorResponse(c, err)
		}
		return response.Success(c, http.StatusOK, map[string]string{"status": "deleted"})
	}
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
