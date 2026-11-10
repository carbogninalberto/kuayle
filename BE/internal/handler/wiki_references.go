package handler

import (
	"errors"
	"net/http"
	"slices"
	"strconv"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/kuayle/kuayle-backend/pkg/response"
	"github.com/labstack/echo/v5"
)

func toWikiReference(page domain.WikiReference) dto.WikiReferenceResponse {
	return dto.WikiReferenceResponse{
		ID:         page.ID.String(),
		Accessible: true,
		Title:      page.Title,
		TeamID:     optionalUUIDString(page.TeamID),
		ProjectID:  optionalUUIDString(page.ProjectID),
		ScopeName:  page.ScopeName,
	}
}

func toResolvedWikiReferences(resolved []service.ResolvedWikiReference) []dto.WikiReferenceResponse {
	resp := make([]dto.WikiReferenceResponse, len(resolved))
	for i, reference := range resolved {
		if reference.Accessible {
			resp[i] = toWikiReference(reference.Page)
		} else {
			resp[i] = dto.WikiReferenceResponse{ID: reference.ID.String()}
		}
	}
	return resp
}

// SearchReferences suggests pages to mention, across every wiki of the
// workspace the caller can see.
func (h *WikiHandler) SearchReferences(c *echo.Context) error {
	limit, _ := strconv.Atoi(c.QueryParam("limit"))
	pages, err := h.wikiSvc.SearchReferences(c.Request().Context(), middleware.GetWorkspace(c).ID, c.QueryParam("q"), limit)
	if err != nil {
		return wikiErrorResponse(c, err)
	}
	resp := make([]dto.WikiReferenceResponse, len(pages))
	for i, page := range pages {
		resp[i] = toWikiReference(page)
	}
	return response.Success(c, http.StatusOK, resp)
}

// ResolveReferences tells reference chips what to show: a title and scope
// for pages the caller can see, and nothing for any other ID.
func (h *WikiHandler) ResolveReferences(c *echo.Context) error {
	var req dto.ResolveWikiReferencesRequest
	if err := c.Bind(&req); err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid request body")
	}
	ids := make([]uuid.UUID, 0, len(req.IDs))
	for _, raw := range req.IDs {
		id, err := uuid.Parse(raw)
		if err != nil {
			return response.ValidationError(c, []dto.ErrorDetail{{Field: "ids", Message: "must be wiki page IDs"}})
		}
		ids = append(ids, id)
	}
	resolved, err := h.wikiSvc.ResolveReferences(c.Request().Context(), middleware.GetWorkspace(c).ID, ids)
	if err != nil {
		return wikiErrorResponse(c, err)
	}
	return response.Success(c, http.StatusOK, toResolvedWikiReferences(resolved))
}

// Backlinks lists the issues and pages that reference a page.
func (h *WikiHandler) Backlinks(kind domain.WikiScopeKind) echo.HandlerFunc {
	return func(c *echo.Context) error {
		scope, pageID, ok := wikiPageFromRoute(c, kind)
		if !ok {
			return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid wiki page ID")
		}
		// Referencing issues are listed only to callers that may read issues;
		// a wiki-only token still gets the referencing pages.
		links, err := h.wikiSvc.Backlinks(c.Request().Context(), middleware.GetWorkspace(c).ID, scope, pageID, callerMay(c, domain.PermIssuesRead))
		if err != nil {
			return wikiErrorResponse(c, err)
		}
		resp := dto.WikiBacklinksResponse{
			Issues: make([]dto.WikiBacklinkIssueResponse, len(links.Issues)),
			Pages:  make([]dto.WikiReferenceResponse, len(links.Pages)),
		}
		for i, issue := range links.Issues {
			resp.Issues[i] = dto.WikiBacklinkIssueResponse{ID: issue.ID.String(), Identifier: issue.Identifier, Title: issue.Title}
		}
		for i, page := range links.Pages {
			resp.Pages[i] = toWikiReference(page)
		}
		return response.Success(c, http.StatusOK, resp)
	}
}

// IssueReferences lists the pages attached to an issue.
func (h *WikiHandler) IssueReferences(c *echo.Context) error {
	resolved, err := h.wikiSvc.IssueReferences(c.Request().Context(), middleware.GetWorkspace(c).ID, c.Param("identifier"))
	if err != nil {
		return wikiReferenceErrorResponse(c, err)
	}
	return response.Success(c, http.StatusOK, toResolvedWikiReferences(resolved))
}

// AttachIssueReference attaches a page to an issue and returns the issue's
// attached pages.
func (h *WikiHandler) AttachIssueReference(c *echo.Context) error {
	var req dto.AttachWikiReferenceRequest
	if err := c.Bind(&req); err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid request body")
	}
	pageID, err := uuid.Parse(req.PageID)
	if err != nil {
		return response.ValidationError(c, []dto.ErrorDetail{{Field: "page_id", Message: "must be a wiki page ID"}})
	}
	resolved, err := h.wikiSvc.AttachIssueReference(c.Request().Context(), middleware.GetWorkspace(c).ID, c.Param("identifier"), pageID, middleware.GetUserID(c))
	if err != nil {
		return wikiReferenceErrorResponse(c, err)
	}
	return response.Success(c, http.StatusOK, toResolvedWikiReferences(resolved))
}

// DetachIssueReference removes an attached page from an issue.
func (h *WikiHandler) DetachIssueReference(c *echo.Context) error {
	pageID, err := uuid.Parse(c.Param("pageId"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid wiki page ID")
	}
	if err := h.wikiSvc.DetachIssueReference(c.Request().Context(), middleware.GetWorkspace(c).ID, c.Param("identifier"), pageID); err != nil {
		return wikiReferenceErrorResponse(c, err)
	}
	return response.Success(c, http.StatusOK, map[string]string{"status": "deleted"})
}

// callerMay reports whether the caller's token scopes and workspace role
// both grant a permission, as middleware.RequirePermission decides it.
func callerMay(c *echo.Context, permission string) bool {
	if scopes, ok := c.Get(middleware.TokenScopesKey).([]string); ok && !slices.Contains(scopes, permission) {
		return false
	}
	role, ok := c.Get("workspace_role").(string)
	return !ok || domain.HasPermission(role, permission)
}

// wikiReferenceErrorResponse hides whether the issue or the page was the
// one that is missing or invisible.
func wikiReferenceErrorResponse(c *echo.Context, err error) error {
	if errors.Is(err, service.ErrWikiNotFound) {
		return response.NotFound(c, "Issue or wiki page")
	}
	return wikiErrorResponse(c, err)
}
