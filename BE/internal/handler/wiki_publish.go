package handler

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/kuayle/kuayle-backend/pkg/response"
	"github.com/labstack/echo/v5"
)

// WikiPublishHandler publishes wiki pages and serves public wiki links.
type WikiPublishHandler struct {
	svc         *service.WikiPublishService
	frontendURL string
}

func NewWikiPublishHandler(svc *service.WikiPublishService, frontendURL string) *WikiPublishHandler {
	return &WikiPublishHandler{svc: svc, frontendURL: strings.TrimRight(frontendURL, "/")}
}

func (h *WikiPublishHandler) publication(state *domain.WikiPublication) dto.WikiPublicationResponse {
	resp := dto.WikiPublicationResponse{Mode: state.Mode, Children: state.Children}
	if state.Mode == domain.WikiPublishPublic && state.Token != nil {
		url := h.frontendURL + "/share/wiki/" + *state.Token
		resp.URL = &url
	}
	return resp
}

// State shows how a page is published and its public link.
func (h *WikiPublishHandler) State(kind domain.WikiScopeKind) echo.HandlerFunc {
	return func(c *echo.Context) error {
		scope, pageID, ok := wikiPageFromRoute(c, kind)
		if !ok {
			return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid wiki page ID")
		}
		state, err := h.svc.State(c.Request().Context(), middleware.GetWorkspace(c).ID, scope, pageID)
		if err != nil {
			return wikiErrorResponse(c, err)
		}
		return response.Success(c, http.StatusOK, h.publication(state))
	}
}

// Publish sets a page's publication: off, internal or public.
func (h *WikiPublishHandler) Publish(kind domain.WikiScopeKind) echo.HandlerFunc {
	return func(c *echo.Context) error {
		scope, pageID, ok := wikiPageFromRoute(c, kind)
		if !ok {
			return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid wiki page ID")
		}
		var req dto.WikiPublishRequest
		if err := c.Bind(&req); err != nil {
			return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid request body")
		}
		role, _ := c.Get("workspace_role").(string)
		state, err := h.svc.Publish(c.Request().Context(), middleware.GetWorkspace(c), role, middleware.GetUserID(c), scope, pageID, req.Mode, req.Children, req.Rotate)
		switch {
		case errors.Is(err, service.ErrWikiPublishForbidden):
			return response.Error(c, http.StatusForbidden, "WIKI_PUBLISH_FORBIDDEN", err.Error())
		case errors.Is(err, service.ErrWikiPublishPrivate):
			return response.Error(c, http.StatusConflict, "WIKI_PUBLISH_PRIVATE", err.Error())
		case err != nil:
			return wikiErrorResponse(c, err)
		}
		return response.Success(c, http.StatusOK, h.publication(state))
	}
}

type publicWikiPageResponse struct {
	ID        string    `json:"id"`
	ParentID  *string   `json:"parent_id"`
	Title     string    `json:"title"`
	Content   string    `json:"content"`
	Position  int       `json:"position"`
	UpdatedAt time.Time `json:"updated_at"`
}

type publicWikiResponse struct {
	WorkspaceName string                   `json:"workspace_name"`
	RootID        string                   `json:"root_id"`
	Pages         []publicWikiPageResponse `json:"pages"`
}

// Public serves a public wiki link: the page and, if published with it, its
// nested pages. Nothing else is exposed: no authors, emails or other pages.
func (h *WikiPublishHandler) Public(c *echo.Context) error {
	c.Response().Header().Set("Cache-Control", "private, no-store")
	wiki, err := h.svc.ResolvePublic(c.Request().Context(), c.Param("token"))
	if err != nil {
		return response.NotFound(c, "Page")
	}
	resp := publicWikiResponse{WorkspaceName: wiki.WorkspaceName, RootID: wiki.RootID.String(), Pages: make([]publicWikiPageResponse, len(wiki.Pages))}
	for i, page := range wiki.Pages {
		resp.Pages[i] = publicWikiPageResponse{ID: page.ID.String(), ParentID: optionalUUIDString(page.ParentID), Title: page.Title, Content: page.Content, Position: page.Position, UpdatedAt: page.UpdatedAt}
	}
	return response.Success(c, http.StatusOK, resp)
}
