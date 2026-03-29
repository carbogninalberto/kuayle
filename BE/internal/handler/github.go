package handler

import (
	"io"
	"net/http"
	"strconv"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/kuayle/kuayle-backend/pkg/response"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

type GitHubHandler struct {
	ghSvc *service.GitHubService
}

func NewGitHubHandler(ghSvc *service.GitHubService) *GitHubHandler {
	return &GitHubHandler{ghSvc: ghSvc}
}

// Status returns the GitHub integration status for the workspace.
func (h *GitHubHandler) Status(c echo.Context) error {
	ws := c.Get("workspace").(*domain.Workspace)
	status, err := h.ghSvc.GetStatus(c.Request().Context(), ws.ID)
	if err != nil {
		return response.InternalError(c)
	}
	return response.Success(c, http.StatusOK, status)
}

// Setup returns the manifest data as JSON. The frontend handles the form POST.
func (h *GitHubHandler) Setup(c echo.Context) error {
	ws := c.Get("workspace").(*domain.Workspace)
	manifest, submitURL := h.ghSvc.GetManifest(ws.ID, ws.Slug)
	return response.Success(c, http.StatusOK, map[string]any{
		"manifest":   manifest,
		"submit_url": submitURL,
	})
}

// SetupCallback handles the redirect from GitHub after app creation via manifest.
func (h *GitHubHandler) SetupCallback(c echo.Context) error {
	ws := c.Get("workspace").(*domain.Workspace)
	code := c.QueryParam("code")
	if code == "" {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Missing code parameter")
	}

	appCfg, err := h.ghSvc.HandleManifestCallback(c.Request().Context(), ws.ID, code)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", err.Error())
	}

	appSlug := ""
	if appCfg.AppSlug != nil {
		appSlug = *appCfg.AppSlug
	}

	return response.Success(c, http.StatusOK, map[string]any{
		"app_id":   appCfg.AppID,
		"app_slug": appSlug,
	})
}

// InstallURL returns the URL to install the GitHub App.
func (h *GitHubHandler) InstallURL(c echo.Context) error {
	ws := c.Get("workspace").(*domain.Workspace)
	url, err := h.ghSvc.GetInstallURL(c.Request().Context(), ws.ID)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", err.Error())
	}
	return response.Success(c, http.StatusOK, dto.GitHubInstallURLResponse{URL: url})
}

// Callback handles the GitHub App installation callback.
func (h *GitHubHandler) Callback(c echo.Context) error {
	ws := c.Get("workspace").(*domain.Workspace)
	userID := c.Get("user_id").(uuid.UUID)

	installationIDStr := c.QueryParam("installation_id")
	installationID, err := strconv.ParseInt(installationIDStr, 10, 64)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid installation_id")
	}

	inst, err := h.ghSvc.HandleInstallationCallback(c.Request().Context(), ws.ID, userID, installationID)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", err.Error())
	}

	return response.Success(c, http.StatusOK, map[string]any{
		"id":            inst.ID,
		"account_login": inst.AccountLogin,
	})
}

// Disconnect removes the GitHub installation (keeps app config).
func (h *GitHubHandler) Disconnect(c echo.Context) error {
	ws := c.Get("workspace").(*domain.Workspace)
	if err := h.ghSvc.Disconnect(c.Request().Context(), ws.ID); err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", err.Error())
	}
	return response.Success(c, http.StatusOK, map[string]string{"status": "disconnected"})
}

// DeleteApp removes the entire GitHub App configuration.
func (h *GitHubHandler) DeleteApp(c echo.Context) error {
	ws := c.Get("workspace").(*domain.Workspace)
	if err := h.ghSvc.DeleteApp(c.Request().Context(), ws.ID); err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", err.Error())
	}
	return response.Success(c, http.StatusOK, map[string]string{"status": "deleted"})
}

// ListRepos lists available GitHub repos for the installation.
func (h *GitHubHandler) ListRepos(c echo.Context) error {
	ws := c.Get("workspace").(*domain.Workspace)
	repos, err := h.ghSvc.ListAvailableRepos(c.Request().Context(), ws.ID)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", err.Error())
	}
	if repos == nil {
		repos = []dto.GitHubAvailableRepoResponse{}
	}
	return response.Success(c, http.StatusOK, repos)
}

// LinkRepos links selected GitHub repos to the workspace.
func (h *GitHubHandler) LinkRepos(c echo.Context) error {
	ws := c.Get("workspace").(*domain.Workspace)
	var req dto.LinkGitHubReposRequest
	if err := c.Bind(&req); err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid request body")
	}
	if err := h.ghSvc.LinkRepos(c.Request().Context(), ws.ID, req); err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", err.Error())
	}
	return response.Success(c, http.StatusOK, map[string]string{"status": "linked"})
}

// UnlinkRepo removes a linked repo.
func (h *GitHubHandler) UnlinkRepo(c echo.Context) error {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid repo ID")
	}
	if err := h.ghSvc.UnlinkRepo(c.Request().Context(), id); err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", err.Error())
	}
	return response.Success(c, http.StatusOK, map[string]string{"status": "unlinked"})
}

// ListAutoTransitions returns the auto-transition rules.
func (h *GitHubHandler) ListAutoTransitions(c echo.Context) error {
	ws := c.Get("workspace").(*domain.Workspace)
	transitions, err := h.ghSvc.ListAutoTransitions(c.Request().Context(), ws.ID)
	if err != nil {
		return response.InternalError(c)
	}
	if transitions == nil {
		transitions = []dto.GitHubAutoTransitionResponse{}
	}
	return response.Success(c, http.StatusOK, transitions)
}

// UpdateAutoTransitions updates the auto-transition rules.
func (h *GitHubHandler) UpdateAutoTransitions(c echo.Context) error {
	ws := c.Get("workspace").(*domain.Workspace)
	var req dto.UpdateAutoTransitionsRequest
	if err := c.Bind(&req); err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid request body")
	}
	if err := h.ghSvc.UpdateAutoTransitions(c.Request().Context(), ws.ID, req); err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", err.Error())
	}
	return response.Success(c, http.StatusOK, map[string]string{"status": "updated"})
}

// IssueGitHubActivity returns GitHub PRs, branches, commits linked to an issue.
func (h *GitHubHandler) IssueGitHubActivity(c echo.Context) error {
	ws := c.Get("workspace").(*domain.Workspace)
	identifier := c.Param("identifier")
	activity, err := h.ghSvc.GetIssueActivity(c.Request().Context(), ws.ID, identifier)
	if err != nil {
		return response.Error(c, http.StatusNotFound, "NOT_FOUND", err.Error())
	}
	return response.Success(c, http.StatusOK, activity)
}

// AgentIssueLinks returns issue-PR/branch/commit links for agent consumption.
func (h *GitHubHandler) AgentIssueLinks(c echo.Context) error {
	ws := c.Get("workspace").(*domain.Workspace)
	identifier := c.QueryParam("identifier")
	if identifier == "" {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "identifier query param required")
	}
	activity, err := h.ghSvc.GetIssueActivity(c.Request().Context(), ws.ID, identifier)
	if err != nil {
		return response.Error(c, http.StatusNotFound, "NOT_FOUND", err.Error())
	}
	return response.Success(c, http.StatusOK, activity)
}

// HandleWebhook receives incoming GitHub webhook events (public endpoint).
func (h *GitHubHandler) HandleWebhook(c echo.Context) error {
	signature := c.Request().Header.Get("X-Hub-Signature-256")
	eventType := c.Request().Header.Get("X-GitHub-Event")

	body, err := io.ReadAll(c.Request().Body)
	if err != nil {
		return c.NoContent(http.StatusBadRequest)
	}

	if !h.ghSvc.VerifyWebhookSignature(c.Request().Context(), body, signature) {
		return c.NoContent(http.StatusUnauthorized)
	}

	if err := h.ghSvc.HandleWebhookEvent(c.Request().Context(), eventType, body); err != nil {
		return c.NoContent(http.StatusInternalServerError)
	}

	return c.NoContent(http.StatusOK)
}
