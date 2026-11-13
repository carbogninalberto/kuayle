package handler

import (
	"errors"
	"io"
	"net/http"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/kuayle/kuayle-backend/pkg/response"
	"github.com/labstack/echo/v5"
)

// Forge webhook payloads are bounded like GitHub's 25 MB limit.
const maxForgeWebhookBytes = 25 * 1024 * 1024

type ForgeHandler struct {
	forgeSvc *service.ForgeService
}

func NewForgeHandler(forgeSvc *service.ForgeService) *ForgeHandler {
	return &ForgeHandler{forgeSvc: forgeSvc}
}

// ListConnections returns the workspace's forge connections without secrets.
func (h *ForgeHandler) ListConnections(c *echo.Context) error {
	ws := c.Get("workspace").(*domain.Workspace)
	connections, err := h.forgeSvc.ListConnections(c.Request().Context(), ws.ID)
	if err != nil {
		return response.InternalError(c)
	}
	return response.Success(c, http.StatusOK, connections)
}

// CreateConnection adds a GitLab, Gitea or Forgejo connection.
func (h *ForgeHandler) CreateConnection(c *echo.Context) error {
	ws := c.Get("workspace").(*domain.Workspace)
	userID := c.Get("user_id").(uuid.UUID)
	var req dto.CreateForgeConnectionRequest
	if err := c.Bind(&req); err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid request body")
	}
	connection, err := h.forgeSvc.CreateConnection(c.Request().Context(), ws.ID, userID, req)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", err.Error())
	}
	return response.Success(c, http.StatusCreated, connection)
}

// UpdateConnection changes the connection's settings. Provider is immutable.
func (h *ForgeHandler) UpdateConnection(c *echo.Context) error {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid connection ID")
	}
	ws := c.Get("workspace").(*domain.Workspace)
	var req dto.UpdateForgeConnectionRequest
	if err := c.Bind(&req); err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid request body")
	}
	connection, err := h.forgeSvc.UpdateConnection(c.Request().Context(), ws.ID, id, req)
	if err != nil {
		if errors.Is(err, service.ErrForgeConnectionNotFound) {
			return response.NotFound(c, "Connection")
		}
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", err.Error())
	}
	return response.Success(c, http.StatusOK, connection)
}

// DeleteConnection removes a connection and its linked repositories.
func (h *ForgeHandler) DeleteConnection(c *echo.Context) error {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid connection ID")
	}
	ws := c.Get("workspace").(*domain.Workspace)
	if err := h.forgeSvc.DeleteConnection(c.Request().Context(), ws.ID, id); err != nil {
		if errors.Is(err, service.ErrForgeConnectionNotFound) {
			return response.NotFound(c, "Connection")
		}
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", err.Error())
	}
	return response.Success(c, http.StatusOK, map[string]string{"status": "deleted"})
}

// ListRepos lists the repositories linked to one connection.
func (h *ForgeHandler) ListRepos(c *echo.Context) error {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid connection ID")
	}
	ws := c.Get("workspace").(*domain.Workspace)
	repos, err := h.forgeSvc.ListRepos(c.Request().Context(), ws.ID, id)
	if err != nil {
		if errors.Is(err, service.ErrForgeConnectionNotFound) {
			return response.NotFound(c, "Connection")
		}
		return response.InternalError(c)
	}
	return response.Success(c, http.StatusOK, repos)
}

// LinkRepo links a repository the admin identifies by its external ID.
func (h *ForgeHandler) LinkRepo(c *echo.Context) error {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid connection ID")
	}
	ws := c.Get("workspace").(*domain.Workspace)
	var req dto.LinkForgeRepoRequest
	if err := c.Bind(&req); err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid request body")
	}
	repo, err := h.forgeSvc.LinkRepo(c.Request().Context(), ws.ID, id, req)
	if err != nil {
		if errors.Is(err, service.ErrForgeConnectionNotFound) {
			return response.NotFound(c, "Connection")
		}
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", err.Error())
	}
	return response.Success(c, http.StatusCreated, repo)
}

// UnlinkRepo removes a repository from a connection.
func (h *ForgeHandler) UnlinkRepo(c *echo.Context) error {
	connectionID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid connection ID")
	}
	repoID, err := uuid.Parse(c.Param("repoId"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid repository ID")
	}
	ws := c.Get("workspace").(*domain.Workspace)
	if err := h.forgeSvc.UnlinkRepo(c.Request().Context(), ws.ID, connectionID, repoID); err != nil {
		if errors.Is(err, service.ErrForgeRepoNotFound) {
			return response.NotFound(c, "Repository")
		}
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", err.Error())
	}
	return response.Success(c, http.StatusOK, map[string]string{"status": "unlinked"})
}

// IssueForgeActivity returns forge merge requests, branches and commits linked
// to an issue.
func (h *ForgeHandler) IssueForgeActivity(c *echo.Context) error {
	ws := c.Get("workspace").(*domain.Workspace)
	activity, err := h.forgeSvc.GetIssueActivity(c.Request().Context(), ws.ID, c.Param("identifier"))
	if err != nil {
		return response.Error(c, http.StatusNotFound, "NOT_FOUND", err.Error())
	}
	return response.Success(c, http.StatusOK, activity)
}

// HandleWebhook receives a forge webhook. The connection ID selects the secret;
// no payload field is trusted before verification.
func (h *ForgeHandler) HandleWebhook(c *echo.Context) error {
	connectionID, err := uuid.Parse(c.Param("connectionId"))
	if err != nil {
		return c.NoContent(http.StatusUnauthorized)
	}
	body, err := io.ReadAll(http.MaxBytesReader(c.Response(), c.Request().Body, maxForgeWebhookBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return c.NoContent(http.StatusRequestEntityTooLarge)
		}
		return c.NoContent(http.StatusBadRequest)
	}
	if err := h.forgeSvc.HandleWebhook(c.Request().Context(), connectionID, c.Request().Header, body); err != nil {
		switch {
		case errors.Is(err, service.ErrForgeUnauthorized):
			return c.NoContent(http.StatusUnauthorized)
		case errors.Is(err, service.ErrForgeDeliveryID):
			return c.NoContent(http.StatusBadRequest)
		default:
			return c.NoContent(http.StatusInternalServerError)
		}
	}
	return c.NoContent(http.StatusOK)
}
