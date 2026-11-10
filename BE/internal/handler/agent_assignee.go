package handler

import (
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/kuayle/kuayle-backend/pkg/response"
	"github.com/labstack/echo/v5"
)

// AgentAssigneeHandler serves agent identities and the agents assigned to
// issues. Authorization is the service's and the repository's; the role read
// here only tells the client which controls to show.
type AgentAssigneeHandler struct {
	svc    *service.AgentAssigneeService
	issues *service.IssueService
}

func NewAgentAssigneeHandler(svc *service.AgentAssigneeService, issues *service.IssueService) *AgentAssigneeHandler {
	return &AgentAssigneeHandler{svc: svc, issues: issues}
}

func agentAssigneeError(c *echo.Context, err error) error {
	switch {
	case errors.Is(err, repository.ErrAgentAssignForbidden):
		return response.Error(c, http.StatusForbidden, "AGENT_ASSIGN_FORBIDDEN", "Guests cannot assign agents")
	case errors.Is(err, repository.ErrAgentNotAssignable):
		return response.Error(c, http.StatusForbidden, "AGENT_NOT_ASSIGNABLE", "This agent cannot be assigned in this team")
	case errors.Is(err, repository.ErrAgentIssueNotFound):
		return response.NotFound(c, "Issue")
	case errors.Is(err, repository.ErrAgentIdentityDenied):
		return response.Error(c, http.StatusForbidden, "FORBIDDEN", "Only workspace administrators manage agents")
	case errors.Is(err, repository.ErrAgentIdentityNotFound):
		return response.NotFound(c, "Agent")
	case errors.Is(err, repository.ErrAgentIdentityInvalid):
		return response.Error(c, http.StatusBadRequest, "INVALID_AGENT", "Use a #rrggbb colour and teams of this workspace")
	default:
		return response.InternalError(c)
	}
}

// List returns the workspace's agents. Guests get none: they cannot assign
// or mention agents, and see assigned agents on the issues themselves.
func (h *AgentAssigneeHandler) List(c *echo.Context) error {
	ws := middleware.GetWorkspace(c)
	role, _ := c.Get("workspace_role").(string)
	result := dto.AgentIdentityList{Agents: []domain.AgentIdentity{}, CanManage: role == domain.RoleOwner || role == domain.RoleAdmin, CanAssign: role != "" && role != domain.RoleGuest}
	if !result.CanAssign {
		return response.Success(c, http.StatusOK, result)
	}
	agents, err := h.svc.Identities(c.Request().Context(), ws.ID)
	if err != nil {
		return agentAssigneeError(c, err)
	}
	result.Agents = agents
	return response.Success(c, http.StatusOK, result)
}

// Update changes an agent's avatar colour and the teams it can be assigned in.
func (h *AgentAssigneeHandler) Update(c *echo.Context) error {
	id, err := uuid.Parse(c.Param("agentId"))
	if err != nil {
		return response.NotFound(c, "Agent")
	}
	var input service.AgentIdentityInput
	if err = c.Bind(&input); err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid request body")
	}
	ws := middleware.GetWorkspace(c)
	if err = h.svc.UpdateIdentity(c.Request().Context(), ws.ID, id, input); err != nil {
		return agentAssigneeError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *AgentAssigneeHandler) issue(c *echo.Context) (*domain.Issue, bool) {
	issue, err := h.issues.GetByIdentifier(c.Request().Context(), middleware.GetWorkspace(c).ID, c.Param("identifier"))
	return issue, err == nil && issue != nil
}

// IssueAgents lists the agents assigned to an issue with their task status.
func (h *AgentAssigneeHandler) IssueAgents(c *echo.Context) error {
	issue, ok := h.issue(c)
	if !ok {
		return response.NotFound(c, "Issue")
	}
	assigned, err := h.svc.ForIssues(c.Request().Context(), []uuid.UUID{issue.ID})
	if err != nil {
		return response.InternalError(c)
	}
	agents := assigned[issue.ID]
	if agents == nil {
		agents = []domain.IssueAgentAssignee{}
	}
	return response.Success(c, http.StatusOK, agents)
}

// SetIssueAgents replaces the agents assigned to an issue.
func (h *AgentAssigneeHandler) SetIssueAgents(c *echo.Context) error {
	var input dto.SetIssueAgentsRequest
	if err := c.Bind(&input); err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid request body")
	}
	ids := make([]uuid.UUID, 0, len(input.AgentIDs))
	for _, raw := range input.AgentIDs {
		id, err := uuid.Parse(strings.TrimSpace(raw))
		if err != nil {
			return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "agent_ids must be agent IDs")
		}
		ids = append(ids, id)
	}
	issue, ok := h.issue(c)
	if !ok {
		return response.NotFound(c, "Issue")
	}
	agents, err := h.svc.SetIssueAgents(c.Request().Context(), middleware.GetWorkspace(c).ID, issue.ID, ids)
	if err != nil {
		return agentAssigneeError(c, err)
	}
	return response.Success(c, http.StatusOK, agents)
}
