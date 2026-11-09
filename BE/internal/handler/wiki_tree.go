package handler

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/pkg/response"
	"github.com/labstack/echo/v5"
)

func toWikiTree(nodes []domain.WikiTreeNode) []dto.WikiTreeNodeResponse {
	out := make([]dto.WikiTreeNodeResponse, 0, len(nodes))
	for _, node := range nodes {
		if node.Registry {
			continue
		}
		out = append(out, dto.WikiTreeNodeResponse{ID: node.ID.String(), ParentID: optionalUUIDString(node.ParentID), Position: node.Position, Kind: node.Kind})
	}
	return out
}

func parseOptionalUUID(raw *string) (*uuid.UUID, bool) {
	if raw == nil || *raw == "" {
		return nil, true
	}
	id, err := uuid.Parse(*raw)
	return &id, err == nil
}

// Tree lists where each page sits: parent, position and kind. Registry days
// are not part of the tree.
func (h *WikiHandler) Tree(kind domain.WikiScopeKind) echo.HandlerFunc {
	return func(c *echo.Context) error {
		scope, ok := wikiScopeFromRoute(c, kind)
		if !ok {
			return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid "+string(kind)+" ID")
		}
		nodes, err := h.wikiSvc.Tree(c.Request().Context(), middleware.GetWorkspace(c).ID, scope)
		if err != nil {
			return wikiErrorResponse(c, err)
		}
		return response.Success(c, http.StatusOK, toWikiTree(nodes))
	}
}

// Move reorders a page or moves it into or out of a folder or page of the
// same scope, and returns the new tree.
func (h *WikiHandler) Move(kind domain.WikiScopeKind) echo.HandlerFunc {
	return func(c *echo.Context) error {
		scope, pageID, ok := wikiPageFromRoute(c, kind)
		if !ok {
			return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid wiki page ID")
		}
		var req dto.MoveWikiPageRequest
		if err := c.Bind(&req); err != nil {
			return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid request body")
		}
		parent, ok := parseOptionalUUID(req.ParentID)
		if !ok {
			return response.ValidationError(c, []dto.ErrorDetail{{Field: "parent_id", Message: "must be a page ID or null"}})
		}
		nodes, err := h.wikiSvc.Move(c.Request().Context(), middleware.GetWorkspace(c).ID, scope, pageID, middleware.GetUserID(c), parent, req.Index)
		if err != nil {
			return wikiErrorResponse(c, err)
		}
		return response.Success(c, http.StatusOK, toWikiTree(nodes))
	}
}

// CreateInTree creates a page or folder placed last under an optional parent.
func (h *WikiHandler) CreateInTree(kind domain.WikiScopeKind) echo.HandlerFunc {
	return func(c *echo.Context) error {
		scope, ok := wikiScopeFromRoute(c, kind)
		if !ok {
			return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid "+string(kind)+" ID")
		}
		var req dto.CreateWikiTreePageRequest
		if err := c.Bind(&req); err != nil {
			return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid request body")
		}
		parent, ok := parseOptionalUUID(req.ParentID)
		if !ok {
			return response.ValidationError(c, []dto.ErrorDetail{{Field: "parent_id", Message: "must be a page ID or null"}})
		}
		page, err := h.wikiSvc.CreateIn(c.Request().Context(), middleware.GetWorkspace(c).ID, scope, middleware.GetUserID(c),
			dto.CreateWikiPageRequest{Title: req.Title, Content: req.Content}, req.Kind, parent)
		if err != nil {
			return wikiErrorResponse(c, err)
		}
		return response.Success(c, http.StatusCreated, toWikiPage(*page))
	}
}
