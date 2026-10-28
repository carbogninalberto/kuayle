package handler

import (
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/kuayle/kuayle-backend/pkg/response"
	"github.com/labstack/echo/v4"
)

// AgentMessageHandler serves the agent messaging registry ("navigator").
type AgentMessageHandler struct{ svc *service.AgentMessageService }

func NewAgentMessageHandler(svc *service.AgentMessageService) *AgentMessageHandler {
	return &AgentMessageHandler{svc}
}

func agentMessageError(c echo.Context, err error) error {
	switch {
	case errors.Is(err, repository.ErrAgentMessageNotFound):
		return response.NotFound(c, "Agent message")
	case errors.Is(err, repository.ErrAgentMessageForbidden):
		return response.Error(c, http.StatusForbidden, "FORBIDDEN", "Only workspace admins and the creators of these tasks can moderate their messages")
	case errors.Is(err, repository.ErrAgentMessageConflict):
		return response.Error(c, http.StatusConflict, "MESSAGE_CONFLICT", "The message is no longer in a state that allows this action")
	case errors.Is(err, repository.ErrAgentMessageInvalid), errors.Is(err, repository.ErrConversationInvalid):
		return response.Error(c, http.StatusBadRequest, "INVALID_MESSAGE_REQUEST", "Invalid agent message request")
	default:
		return response.InternalError(c)
	}
}

func optionalUUID(c echo.Context, name string) (*uuid.UUID, error) {
	raw := c.QueryParam(name)
	if raw == "" {
		return nil, nil
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return nil, repository.ErrAgentMessageInvalid
	}
	return &id, nil
}

func pathUUID(c echo.Context, name string) (uuid.UUID, error) {
	id, err := uuid.Parse(c.Param(name))
	if err != nil {
		return uuid.Nil, repository.ErrAgentMessageNotFound
	}
	return id, nil
}

// List pages messages with optional task_id, issue_id, state, channel and
// before filters.
func (h *AgentMessageHandler) List(c echo.Context) error {
	filter := domain.AgentMessageFilter{State: c.QueryParam("state"), Channel: c.QueryParam("channel"), Issue: strings.ToUpper(strings.TrimSpace(c.QueryParam("issue")))}
	if filter.Issue != "" && !domain.ValidAgentMessageIssue(filter.Issue) {
		return agentMessageError(c, repository.ErrAgentMessageInvalid)
	}
	switch filter.State {
	case "", "queued", "held", "delivered", "dropped", "rejected":
	default:
		return agentMessageError(c, repository.ErrAgentMessageInvalid)
	}
	switch filter.Channel {
	case "", domain.AgentMessageInternal, domain.AgentMessageComment:
	default:
		return agentMessageError(c, repository.ErrAgentMessageInvalid)
	}
	var err error
	for name, target := range map[string]**uuid.UUID{"task_id": &filter.TaskID, "issue_id": &filter.IssueID, "before": &filter.Before} {
		if *target, err = optionalUUID(c, name); err != nil {
			return agentMessageError(c, err)
		}
	}
	page, err := h.svc.List(c.Request().Context(), middleware.GetWorkspace(c).ID, filter)
	if err != nil {
		return agentMessageError(c, err)
	}
	return response.Success(c, http.StatusOK, page)
}

// Get returns one message with its audited actions.
func (h *AgentMessageHandler) Get(c echo.Context) error {
	id, err := pathUUID(c, "messageId")
	if err != nil {
		return agentMessageError(c, err)
	}
	message, actions, err := h.svc.Get(c.Request().Context(), middleware.GetWorkspace(c).ID, id)
	if err != nil {
		return agentMessageError(c, err)
	}
	return response.Success(c, http.StatusOK, map[string]any{"message": message, "actions": actions})
}

func (h *AgentMessageHandler) act(action string) echo.HandlerFunc {
	return func(c echo.Context) error {
		id, err := pathUUID(c, "messageId")
		if err != nil {
			return agentMessageError(c, err)
		}
		message, err := h.svc.Act(c.Request().Context(), middleware.GetWorkspace(c).ID, id, action)
		if err != nil {
			return agentMessageError(c, err)
		}
		return response.Success(c, http.StatusOK, message)
	}
}

func (h *AgentMessageHandler) Hold() echo.HandlerFunc    { return h.act("hold") }
func (h *AgentMessageHandler) Release() echo.HandlerFunc { return h.act("release") }
func (h *AgentMessageHandler) Drop() echo.HandlerFunc    { return h.act("drop") }

// Holds lists held conversations.
func (h *AgentMessageHandler) Holds(c echo.Context) error {
	holds, err := h.svc.Holds(c.Request().Context(), middleware.GetWorkspace(c).ID)
	if err != nil {
		return agentMessageError(c, err)
	}
	return response.Success(c, http.StatusOK, map[string]any{"holds": holds})
}

// HoldConversation holds the messages between two issues' agents.
func (h *AgentMessageHandler) HoldConversation(c echo.Context) error {
	var body struct {
		IssueIDs []uuid.UUID `json:"issue_ids"`
	}
	if err := bindTask(c, &body); err != nil || len(body.IssueIDs) != 2 {
		return agentMessageError(c, repository.ErrAgentMessageInvalid)
	}
	hold, err := h.svc.HoldConversation(c.Request().Context(), middleware.GetWorkspace(c).ID, body.IssueIDs[0], body.IssueIDs[1])
	if err != nil {
		return agentMessageError(c, err)
	}
	return response.Success(c, http.StatusCreated, hold)
}

// ReleaseConversation ends a held conversation and releases its messages.
func (h *AgentMessageHandler) ReleaseConversation(c echo.Context) error {
	id, err := pathUUID(c, "holdId")
	if err != nil {
		return agentMessageError(c, err)
	}
	if err = h.svc.ReleaseConversation(c.Request().Context(), middleware.GetWorkspace(c).ID, id); err != nil {
		return agentMessageError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}
