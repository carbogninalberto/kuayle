package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/kuayle/kuayle-backend/pkg/response"
	"github.com/labstack/echo/v5"
)

// Workflow graphs are at most 256 KiB; JSON around them adds little.
const workflowRequestBytes = 320 << 10

// WorkflowHandler serves Settings → Agents profiles, Settings → Workflows and
// the task workflow view and decisions.
type WorkflowHandler struct {
	svc *service.WorkflowService
}

func NewWorkflowHandler(svc *service.WorkflowService) *WorkflowHandler {
	return &WorkflowHandler{svc: svc}
}

func workflowError(c *echo.Context, err error) error {
	var invalidInput *service.ErrInvalidWorkflowInput
	var invalidGraph *domain.WorkflowInvalidError
	switch {
	case errors.As(err, &invalidGraph):
		return c.JSON(http.StatusUnprocessableEntity, map[string]any{"error": map[string]any{"code": "INVALID_WORKFLOW", "message": invalidGraph.Error(), "issues": invalidGraph.Issues}})
	case errors.As(err, &invalidInput):
		return response.Error(c, http.StatusBadRequest, "INVALID_WORKFLOW", invalidInput.Message)
	case errors.Is(err, service.ErrWorkflowsDenied), errors.Is(err, repository.ErrWorkflowDenied):
		return response.Forbidden(c)
	case errors.Is(err, repository.ErrWorkflowNotFound):
		return response.NotFound(c, "Workflow")
	case errors.Is(err, repository.ErrAgentProfileNotFound):
		return response.NotFound(c, "Agent profile")
	case errors.Is(err, repository.ErrWorkflowScopeNotFound):
		return response.NotFound(c, "Scope")
	case errors.Is(err, repository.ErrWorkflowNameTaken):
		return response.Error(c, http.StatusConflict, "WORKFLOW_NAME_TAKEN", "A workflow with this name already exists")
	case errors.Is(err, repository.ErrAgentProfileNameTaken):
		return response.Error(c, http.StatusConflict, "PROFILE_NAME_TAKEN", "An agent profile with this name already exists")
	case errors.Is(err, repository.ErrAgentProfileBehaviourInUse):
		return response.Error(c, http.StatusConflict, "PROFILE_BEHAVIOUR_IN_USE", "Workflows use this profile, so what it does cannot change; create a new profile for the other behaviour")
	case errors.Is(err, repository.ErrAgentProfileSkill):
		return response.Error(c, http.StatusBadRequest, "PROFILE_SKILL_INVALID", "Only active skills shared with the whole workspace can be added to an agent profile")
	case errors.Is(err, repository.ErrAgentProfileInUse):
		return response.Error(c, http.StatusConflict, "PROFILE_IN_USE", "A workflow uses this profile; change the workflow first")
	case errors.Is(err, repository.ErrWorkflowInUse):
		return response.Error(c, http.StatusConflict, "WORKFLOW_IN_USE", "A task that is still running uses this workflow; wait for it to finish or cancel it first")
	case errors.Is(err, repository.ErrWorkflowStale):
		return response.Error(c, http.StatusConflict, "WORKFLOW_STALE", "Someone saved a newer version; reload it and edit again")
	case errors.Is(err, service.ErrWorkflowDecision):
		return response.Error(c, http.StatusConflict, "WORKFLOW_CONFLICT", "The workflow is not waiting for this decision any more")
	default:
		return agentTaskError(c, err)
	}
}

func decodeWorkflowJSON(c *echo.Context, target any) bool {
	decoder := json.NewDecoder(io.LimitReader(c.Request().Body, workflowRequestBytes))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target) == nil
}

func invalidWorkflowBody(c *echo.Context) error {
	return response.Error(c, http.StatusBadRequest, "INVALID_WORKFLOW", "Invalid request body")
}

func workspaceID(c *echo.Context) uuid.UUID { return middleware.GetWorkspace(c).ID }

func (h *WorkflowHandler) Profiles(c *echo.Context) error {
	result, err := h.svc.Profiles(c.Request().Context(), workspaceID(c))
	if err != nil {
		return workflowError(c, err)
	}
	return response.Success(c, http.StatusOK, result)
}

func (h *WorkflowHandler) CreateProfile(c *echo.Context) error {
	var in service.AgentProfileRequest
	if !decodeWorkflowJSON(c, &in) {
		return invalidWorkflowBody(c)
	}
	result, err := h.svc.CreateProfile(c.Request().Context(), workspaceID(c), in)
	if err != nil {
		return workflowError(c, err)
	}
	return response.Success(c, http.StatusCreated, result)
}

func profileID(c *echo.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("profileId"))
	return id, err == nil
}

func (h *WorkflowHandler) UpdateProfile(c *echo.Context) error {
	id, ok := profileID(c)
	if !ok {
		return response.NotFound(c, "Agent profile")
	}
	var in service.AgentProfileRequest
	if !decodeWorkflowJSON(c, &in) {
		return invalidWorkflowBody(c)
	}
	result, err := h.svc.UpdateProfile(c.Request().Context(), workspaceID(c), id, in)
	if err != nil {
		return workflowError(c, err)
	}
	return response.Success(c, http.StatusOK, result)
}

func (h *WorkflowHandler) ArchiveProfile(c *echo.Context) error {
	id, ok := profileID(c)
	if !ok {
		return response.NotFound(c, "Agent profile")
	}
	if err := h.svc.ArchiveProfile(c.Request().Context(), workspaceID(c), id); err != nil {
		return workflowError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *WorkflowHandler) List(c *echo.Context) error {
	result, err := h.svc.List(c.Request().Context(), workspaceID(c))
	if err != nil {
		return workflowError(c, err)
	}
	return response.Success(c, http.StatusOK, result)
}

func (h *WorkflowHandler) Get(c *echo.Context) error {
	result, err := h.svc.Get(c.Request().Context(), workspaceID(c), c.Param("workflowId"))
	if err != nil {
		return workflowError(c, err)
	}
	return response.Success(c, http.StatusOK, result)
}

func (h *WorkflowHandler) Create(c *echo.Context) error {
	var in service.WorkflowRequest
	if !decodeWorkflowJSON(c, &in) {
		return invalidWorkflowBody(c)
	}
	result, err := h.svc.Create(c.Request().Context(), workspaceID(c), in)
	if err != nil {
		return workflowError(c, err)
	}
	return response.Success(c, http.StatusCreated, result)
}

func (h *WorkflowHandler) Save(c *echo.Context) error {
	var in service.WorkflowRequest
	if !decodeWorkflowJSON(c, &in) {
		return invalidWorkflowBody(c)
	}
	result, err := h.svc.Save(c.Request().Context(), workspaceID(c), c.Param("workflowId"), in)
	if err != nil {
		return workflowError(c, err)
	}
	return response.Success(c, http.StatusOK, result)
}

func (h *WorkflowHandler) Duplicate(c *echo.Context) error {
	var in struct {
		Name string `json:"name"`
	}
	if c.Request().ContentLength != 0 && !decodeWorkflowJSON(c, &in) {
		return invalidWorkflowBody(c)
	}
	result, err := h.svc.Duplicate(c.Request().Context(), workspaceID(c), c.Param("workflowId"), in.Name)
	if err != nil {
		return workflowError(c, err)
	}
	return response.Success(c, http.StatusCreated, result)
}

func (h *WorkflowHandler) Archive(c *echo.Context) error {
	if err := h.svc.Archive(c.Request().Context(), workspaceID(c), c.Param("workflowId")); err != nil {
		return workflowError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *WorkflowHandler) Delete(c *echo.Context) error {
	if err := h.svc.Delete(c.Request().Context(), workspaceID(c), c.Param("workflowId")); err != nil {
		return workflowError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *WorkflowHandler) SetDefault(c *echo.Context) error {
	var in service.WorkflowDefaultRequest
	if !decodeWorkflowJSON(c, &in) {
		return invalidWorkflowBody(c)
	}
	result, err := h.svc.SetDefault(c.Request().Context(), workspaceID(c), in)
	if err != nil {
		return workflowError(c, err)
	}
	return response.Success(c, http.StatusOK, result)
}

func (h *WorkflowHandler) Validate(c *echo.Context) error {
	var in struct {
		Graph json.RawMessage `json:"graph"`
	}
	if !decodeWorkflowJSON(c, &in) {
		return invalidWorkflowBody(c)
	}
	issues, err := h.svc.Validate(c.Request().Context(), workspaceID(c), in.Graph)
	if err != nil {
		return workflowError(c, err)
	}
	// Warnings never refuse the graph; the editor shows them beside issues.
	warnings, err := h.svc.Warnings(c.Request().Context(), workspaceID(c), in.Graph)
	if err != nil {
		return workflowError(c, err)
	}
	return response.Success(c, http.StatusOK, map[string]any{"issues": issues, "warnings": warnings})
}

func (h *WorkflowHandler) Audit(c *echo.Context) error {
	id, err := uuid.Parse(c.Param("workflowId"))
	if err != nil {
		return response.NotFound(c, "Workflow")
	}
	events, err := h.svc.Audit(c.Request().Context(), workspaceID(c), "workflow", id)
	if err != nil {
		return workflowError(c, err)
	}
	return response.Success(c, http.StatusOK, map[string]any{"events": events})
}

func (h *WorkflowHandler) TaskWorkflow(c *echo.Context) error {
	id, err := taskID(c, "taskId")
	if err != nil {
		return agentTaskError(c, err)
	}
	result, err := h.svc.TaskWorkflow(c.Request().Context(), workspaceID(c), id)
	if err != nil {
		return workflowError(c, err)
	}
	return response.Success(c, http.StatusOK, result)
}

func (h *WorkflowHandler) Decide(c *echo.Context) error {
	id, err := taskID(c, "taskId")
	if err != nil {
		return agentTaskError(c, err)
	}
	var in struct {
		Sequence int    `json:"sequence"`
		Decision string `json:"decision"`
	}
	if !decodeWorkflowJSON(c, &in) || in.Sequence < 1 {
		return invalidWorkflowBody(c)
	}
	task, err := h.svc.Decide(c.Request().Context(), workspaceID(c), id, in.Sequence, in.Decision)
	if err != nil {
		return workflowError(c, err)
	}
	return response.Success(c, http.StatusOK, map[string]any{"task_id": task.ID, "state": task.State, "workflow_sequence": task.WorkflowSequence})
}
