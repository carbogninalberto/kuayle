package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path"
	"regexp"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/kuayle/kuayle-backend/pkg/response"
	"github.com/labstack/echo/v4"
)

type AgentTaskHandler struct{ svc *service.AgentTaskService }

func NewAgentTaskHandler(svc *service.AgentTaskService) *AgentTaskHandler {
	return &AgentTaskHandler{svc}
}
func agentTaskError(c echo.Context, err error) error {
	switch {
	case errors.Is(err, repository.ErrConversationNotFound), errors.Is(err, repository.ErrAgentTaskUnauthorized):
		return response.NotFound(c, "Agent task")
	case errors.Is(err, repository.ErrAgentTaskConflict), errors.Is(err, repository.ErrConversationConflict):
		return response.Error(c, http.StatusConflict, "TASK_CONFLICT", "This request conflicts with the current task")
	case errors.Is(err, repository.ErrConversationInvalid):
		return response.Error(c, http.StatusBadRequest, "INVALID_TASK", "Invalid task request")
	default:
		return machineError(c, err)
	}
}
func taskID(c echo.Context, name string) (uuid.UUID, error) {
	id, err := uuid.Parse(c.Param(name))
	if err != nil {
		return uuid.Nil, repository.ErrConversationNotFound
	}
	return id, nil
}
func taskBefore(c echo.Context) (*uuid.UUID, error) {
	raw := c.QueryParam("before")
	if raw == "" {
		return nil, nil
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return nil, repository.ErrConversationInvalid
	}
	return &id, nil
}
func bindTask(c echo.Context, target any) error {
	body := http.MaxBytesReader(c.Response(), c.Request().Body, 64<<10)
	decoder := json.NewDecoder(body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return repository.ErrConversationInvalid
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return repository.ErrConversationInvalid
	}
	return nil
}
func (h *AgentTaskHandler) Options(c echo.Context) error {
	id, err := conversationID(c)
	if err != nil {
		return agentTaskError(c, err)
	}
	result, err := h.svc.Options(c.Request().Context(), middleware.GetWorkspace(c).ID, id)
	if err != nil {
		return agentTaskError(c, err)
	}
	return response.Success(c, http.StatusOK, result)
}
func (h *AgentTaskHandler) Create(c echo.Context) error {
	id, err := conversationID(c)
	if err != nil {
		return agentTaskError(c, err)
	}
	var input domain.AgentTaskRequest
	if err = bindTask(c, &input); err != nil {
		return agentTaskError(c, err)
	}
	result, err := h.svc.Create(c.Request().Context(), middleware.GetWorkspace(c).ID, id, input)
	if err != nil {
		return agentTaskError(c, err)
	}
	return response.Success(c, http.StatusAccepted, result)
}
func (h *AgentTaskHandler) List(c echo.Context) error {
	id, err := conversationID(c)
	if err != nil {
		return agentTaskError(c, err)
	}
	before, err := taskBefore(c)
	if err != nil {
		return agentTaskError(c, err)
	}
	result, err := h.svc.Repo.Page(c.Request().Context(), middleware.GetWorkspace(c).ID, id, before)
	if err != nil {
		return agentTaskError(c, err)
	}
	return response.Success(c, http.StatusOK, result)
}
func (h *AgentTaskHandler) Get(c echo.Context) error {
	id, err := taskID(c, "taskId")
	if err != nil {
		return agentTaskError(c, err)
	}
	result, err := h.svc.Repo.Get(c.Request().Context(), middleware.GetWorkspace(c).ID, id)
	if err != nil {
		return agentTaskError(c, err)
	}
	return response.Success(c, http.StatusOK, result)
}
func (h *AgentTaskHandler) Cancel(c echo.Context) error {
	id, err := taskID(c, "taskId")
	if err != nil {
		return agentTaskError(c, err)
	}
	if err = h.svc.Repo.RequestCancel(c.Request().Context(), middleware.GetWorkspace(c).ID, id); err != nil {
		return agentTaskError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}
func (h *AgentTaskHandler) Retry(c echo.Context) error {
	id, err := taskID(c, "taskId")
	if err != nil {
		return agentTaskError(c, err)
	}
	var input domain.AgentTaskRetryRequest
	if err = bindTask(c, &input); err != nil {
		return agentTaskError(c, err)
	}
	result, err := h.svc.Retry(c.Request().Context(), middleware.GetWorkspace(c).ID, id, input)
	if err != nil {
		return agentTaskError(c, err)
	}
	return response.Success(c, http.StatusAccepted, result)
}
func (h *AgentTaskHandler) History(c echo.Context) error {
	id, err := taskID(c, "taskId")
	if err != nil {
		return agentTaskError(c, err)
	}
	after, err := conversationCursor(c, "after")
	if err != nil {
		return agentTaskError(c, err)
	}
	result, err := h.svc.Repo.History(c.Request().Context(), middleware.GetWorkspace(c).ID, id, after)
	if err != nil {
		return agentTaskError(c, err)
	}
	return response.Success(c, http.StatusOK, result)
}
func (h *AgentTaskHandler) Runs(c echo.Context) error {
	id, err := taskID(c, "taskId")
	if err != nil {
		return agentTaskError(c, err)
	}
	before, err := taskBefore(c)
	if err != nil {
		return agentTaskError(c, err)
	}
	result, err := h.svc.Repo.Runs(c.Request().Context(), middleware.GetWorkspace(c).ID, id, before)
	if err != nil {
		return agentTaskError(c, err)
	}
	return response.Success(c, http.StatusOK, result)
}
func (h *AgentTaskHandler) Logs(c echo.Context) error {
	id, err := taskID(c, "taskId")
	if err != nil {
		return agentTaskError(c, err)
	}
	run, err := taskID(c, "runId")
	if err != nil {
		return agentTaskError(c, err)
	}
	after, err := conversationCursor(c, "after")
	if err != nil {
		return agentTaskError(c, err)
	}
	result, err := h.svc.Repo.Logs(c.Request().Context(), middleware.GetWorkspace(c).ID, id, run, after)
	if err != nil {
		return agentTaskError(c, err)
	}
	return response.Success(c, http.StatusOK, result)
}

func (h *AgentTaskHandler) Validation(c echo.Context) error {
	id, err := taskID(c, "taskId")
	if err != nil {
		return agentTaskError(c, err)
	}
	run, err := taskID(c, "runId")
	if err != nil {
		return agentTaskError(c, err)
	}
	result, err := h.svc.Repo.Validation(c.Request().Context(), middleware.GetWorkspace(c).ID, id, run)
	if err != nil {
		return agentTaskError(c, err)
	}
	return response.Success(c, http.StatusOK, result)
}

func (h *AgentTaskHandler) Publication(c echo.Context) error {
	id, err := taskID(c, "taskId")
	if err != nil {
		return agentTaskError(c, err)
	}
	run, err := taskID(c, "runId")
	if err != nil {
		return agentTaskError(c, err)
	}
	result, err := h.svc.Repo.Publication(c.Request().Context(), middleware.GetWorkspace(c).ID, id, run)
	if err != nil {
		return agentTaskError(c, err)
	}
	return response.Success(c, http.StatusOK, result)
}

func (h *AgentTaskHandler) Signals(c echo.Context) error {
	id, err := taskID(c, "taskId")
	if err != nil {
		return agentTaskError(c, err)
	}
	result, err := h.svc.Repo.Signals(c.Request().Context(), middleware.GetWorkspace(c).ID, id)
	if err != nil {
		return agentTaskError(c, err)
	}
	return response.Success(c, http.StatusOK, result)
}

// Queue lists the session user's recent agent tasks across projects.
func (h *AgentTaskHandler) Queue(c echo.Context) error {
	result, err := h.svc.Repo.Queue(c.Request().Context(), middleware.GetWorkspace(c).ID)
	if err != nil {
		return agentTaskError(c, err)
	}
	return response.Success(c, http.StatusOK, result)
}

func (h *AgentTaskHandler) Artifacts(c echo.Context) error {
	id, err := taskID(c, "taskId")
	if err != nil {
		return agentTaskError(c, err)
	}
	run, err := taskID(c, "runId")
	if err != nil {
		return agentTaskError(c, err)
	}
	result, err := h.svc.Repo.ValidationArtifacts(c.Request().Context(), middleware.GetWorkspace(c).ID, id, run)
	if err != nil {
		return agentTaskError(c, err)
	}
	return response.Success(c, http.StatusOK, result)
}

var unsafeArtifactName = regexp.MustCompile(`[^A-Za-z0-9._-]`)

// Artifact serves a QA report file as untrusted content: never sniffed or
// scripted, and downloaded rather than rendered unless it is an image.
func (h *AgentTaskHandler) Artifact(c echo.Context) error {
	id, err := taskID(c, "taskId")
	if err != nil {
		return agentTaskError(c, err)
	}
	run, err := taskID(c, "runId")
	if err != nil {
		return agentTaskError(c, err)
	}
	artifactID, err := taskID(c, "artifactId")
	if err != nil {
		return agentTaskError(c, err)
	}
	artifact, err := h.svc.Repo.ValidationArtifact(c.Request().Context(), middleware.GetWorkspace(c).ID, id, run, artifactID)
	if err != nil {
		return agentTaskError(c, err)
	}
	headers := c.Response().Header()
	headers.Set("X-Content-Type-Options", "nosniff")
	headers.Set("Content-Security-Policy", "sandbox; default-src 'none'")
	headers.Set("Cache-Control", "private, no-store")
	name := unsafeArtifactName.ReplaceAllString(path.Base(artifact.Path), "_")
	disposition := "attachment"
	if artifact.ContentType == "image/png" || artifact.ContentType == "image/jpeg" {
		disposition = "inline"
	}
	headers.Set("Content-Disposition", disposition+`; filename="`+name+`"`)
	return c.Blob(http.StatusOK, artifact.ContentType, artifact.Data)
}
