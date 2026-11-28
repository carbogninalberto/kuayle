package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/kuayle/kuayle-backend/pkg/response"
	"github.com/labstack/echo/v5"
)

type AgentTaskHandler struct{ svc *service.AgentTaskService }

func NewAgentTaskHandler(svc *service.AgentTaskService) *AgentTaskHandler {
	return &AgentTaskHandler{svc}
}
func agentTaskError(c *echo.Context, err error) error {
	var merged *domain.MergedWorkError
	var refused *repository.TaskRoomRefusedError
	switch {
	case errors.As(err, &merged):
		return response.MergedWork(c, merged.Error(), merged.PullRequest)
	case errors.Is(err, repository.ErrConversationNotFound), errors.Is(err, repository.ErrAgentTaskUnauthorized):
		return response.NotFound(c, "Agent task")
	case errors.Is(err, repository.ErrAgentTaskConflict), errors.Is(err, repository.ErrConversationConflict):
		return response.Error(c, http.StatusConflict, "TASK_CONFLICT", "This request conflicts with the current task")
	case errors.Is(err, repository.ErrConversationInvalid):
		return response.Error(c, http.StatusBadRequest, "INVALID_TASK", "Invalid task request")
	case errors.Is(err, repository.ErrPreviewUnavailable):
		return response.Error(c, http.StatusConflict, "PREVIEW_UNAVAILABLE", "The task needs a preview command and a ready checkout on a running machine")
	case errors.Is(err, repository.ErrMachineBusy):
		return response.Error(c, http.StatusConflict, "MACHINE_BUSY", "The agent is working on this machine; try again when it pauses")
	case errors.Is(err, repository.ErrCheckUnavailable):
		return response.Error(c, http.StatusConflict, "CHECK_UNAVAILABLE", "The check needs the task's latest iteration and its checkout on a running machine")
	case errors.Is(err, repository.ErrNoCheckCommand):
		return response.Error(c, http.StatusConflict, "NO_CHECK_COMMAND", "Provide a command to run")
	case errors.Is(err, repository.ErrCheckForbidden):
		return response.Error(c, http.StatusForbidden, "FORBIDDEN", "Only members who can edit development settings can run a custom check command")
	case errors.Is(err, repository.ErrAgentLiveMessageInvalid):
		return response.Error(c, http.StatusBadRequest, "INVALID_LIVE_MESSAGE", "Messages are 1 to 4,000 characters, for an implementing task")
	case errors.Is(err, repository.ErrAgentLiveMessageClosed):
		return response.Error(c, http.StatusConflict, "TASK_CLOSED", "The task no longer takes messages")
	case errors.Is(err, repository.ErrAgentLiveMessageRateLimited):
		return response.Error(c, http.StatusTooManyRequests, "LIVE_MESSAGE_LIMIT", "Too many messages for this task; try again later")
	case errors.Is(err, repository.ErrTaskRoomUnavailable):
		return response.Error(c, http.StatusConflict, "ROOM_UNAVAILABLE", "Only a task on a pipeline workflow has a room")
	case errors.Is(err, repository.ErrTaskRoomRateLimited):
		return response.Error(c, http.StatusTooManyRequests, "ROOM_MESSAGE_LIMIT", "Too many messages for this task; try again later")
	case errors.As(err, &refused):
		return response.Error(c, http.StatusBadRequest, "ROOM_POST_REFUSED", refused.Reason)
	case errors.Is(err, service.ErrTaskTerminalBusy):
		return response.Error(c, http.StatusConflict, "TASK_BUSY", "The agent is working on this task; open it in Claude Code when it stops")
	case errors.Is(err, service.ErrTaskTerminalUnsupported):
		return response.Error(c, http.StatusConflict, "TERMINAL_UNSUPPORTED", "Only Claude Code tasks open in Claude Code")
	case errors.Is(err, service.ErrSignInNotFound):
		return response.Error(c, http.StatusConflict, "SIGN_IN_NOT_FOUND", "No sign-in was found in the machine's terminal yet")
	case errors.Is(err, service.ErrPipelinesDisabled):
		return response.Error(c, http.StatusForbidden, "PIPELINES_DISABLED", "Pipeline workflows are turned off on this server")
	case errors.Is(err, service.ErrGraphWorkflowsRemoved):
		return response.Error(c, http.StatusBadRequest, "GRAPH_WORKFLOWS_REMOVED", "Graph workflows were removed; create a pipeline instead")
	case errors.Is(err, service.ErrTaskFileRef):
		return response.Error(c, http.StatusBadRequest, "INVALID_REF", "ref must be the task's head or a full commit id one of its iterations recorded")
	case errors.Is(err, service.ErrTaskFilePath):
		return response.Error(c, http.StatusBadRequest, "INVALID_PATH", "path must be a clean directory of the repository, such as src/lib")
	case errors.Is(err, service.ErrTaskFileNotFound):
		return response.Error(c, http.StatusNotFound, "FILE_NOT_FOUND", "The commit tracks nothing at that path")
	case errors.Is(err, service.ErrTaskFilesUnavailable):
		return response.Error(c, http.StatusConflict, "FILES_UNAVAILABLE", "Nothing is published yet and the task's checkout is not on a running machine")
	case errors.Is(err, service.ErrTaskFilesBusy):
		return response.Error(c, http.StatusTooManyRequests, "FILES_BUSY", "Too many reads are running on the task's machine; try again shortly")
	case errors.Is(err, service.ErrTaskFilesFailed):
		return response.Error(c, http.StatusBadGateway, "FILES_FAILED", "The files could not be read at this commit; try again")
	default:
		return machineError(c, err)
	}
}
func taskID(c *echo.Context, name string) (uuid.UUID, error) {
	id, err := uuid.Parse(c.Param(name))
	if err != nil {
		return uuid.Nil, repository.ErrConversationNotFound
	}
	return id, nil
}
func taskBefore(c *echo.Context) (*uuid.UUID, error) {
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
func bindTask(c *echo.Context, target any) error {
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
func (h *AgentTaskHandler) Options(c *echo.Context) error {
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
func (h *AgentTaskHandler) Create(c *echo.Context) error {
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
func (h *AgentTaskHandler) List(c *echo.Context) error {
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
func (h *AgentTaskHandler) Get(c *echo.Context) error {
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

// Sessions lists the task's agent session and the review sessions of its
// published work, read-only, under the task's current authorization.
func (h *AgentTaskHandler) Sessions(c *echo.Context) error {
	id, err := taskID(c, "taskId")
	if err != nil {
		return agentTaskError(c, err)
	}
	result, err := h.svc.Sessions(c.Request().Context(), middleware.GetWorkspace(c).ID, id)
	if err != nil {
		return agentTaskError(c, err)
	}
	return response.Success(c, http.StatusOK, result)
}
func (h *AgentTaskHandler) Cancel(c *echo.Context) error {
	id, err := taskID(c, "taskId")
	if err != nil {
		return agentTaskError(c, err)
	}
	if err = h.svc.Repo.RequestCancel(c.Request().Context(), middleware.GetWorkspace(c).ID, id); err != nil {
		return agentTaskError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

// StartPreview queues the task's preview process and app port; StopPreview
// queues its stop. Both answer 202 with the task.
func (h *AgentTaskHandler) StartPreview(c *echo.Context) error { return h.preview(c, true) }
func (h *AgentTaskHandler) StopPreview(c *echo.Context) error  { return h.preview(c, false) }
func (h *AgentTaskHandler) preview(c *echo.Context, start bool) error {
	id, err := taskID(c, "taskId")
	if err != nil {
		return agentTaskError(c, err)
	}
	task, err := h.svc.RequestPreview(c.Request().Context(), middleware.GetWorkspace(c).ID, id, start)
	if err != nil {
		return agentTaskError(c, err)
	}
	return response.Success(c, http.StatusAccepted, task)
}

// RunCheck runs the task's check, or a given command, on the latest attempt's
// checkout without starting an agent attempt. An empty body runs the task's check.
func (h *AgentTaskHandler) RunCheck(c *echo.Context) error {
	id, err := taskID(c, "taskId")
	if err != nil {
		return agentTaskError(c, err)
	}
	var input struct {
		Command []string `json:"command"`
	}
	if c.Request().ContentLength != 0 {
		if err = bindTask(c, &input); err != nil {
			return agentTaskError(c, err)
		}
	}
	step, err := h.svc.RunCheck(c.Request().Context(), middleware.GetWorkspace(c).ID, id, input.Command)
	if err != nil {
		return agentTaskError(c, err)
	}
	return response.Success(c, http.StatusAccepted, step)
}

// ManualChecks lists an attempt's manual checks, oldest first.
func (h *AgentTaskHandler) ManualChecks(c *echo.Context) error {
	id, err := taskID(c, "taskId")
	if err != nil {
		return agentTaskError(c, err)
	}
	run, err := taskID(c, "runId")
	if err != nil {
		return agentTaskError(c, err)
	}
	result, err := h.svc.Repo.ManualChecks(c.Request().Context(), middleware.GetWorkspace(c).ID, id, run)
	if err != nil {
		return agentTaskError(c, err)
	}
	return response.Success(c, http.StatusOK, result)
}

// ContinueAfterSignIn starts a new attempt using the caller’s saved sign-in
// or captures the sign-in they completed in their terminal first.
func (h *AgentTaskHandler) ContinueAfterSignIn(c *echo.Context) error {
	id, err := taskID(c, "taskId")
	if err != nil {
		return agentTaskError(c, err)
	}
	var input struct {
		RequestID uuid.UUID `json:"request_id"`
	}
	if err = bindTask(c, &input); err != nil {
		return agentTaskError(c, err)
	}
	task, err := h.svc.ContinueAfterSignIn(c.Request().Context(), middleware.GetWorkspace(c).ID, id, input.RequestID)
	if err != nil {
		return agentTaskError(c, err)
	}
	return response.Success(c, http.StatusAccepted, task)
}

// OpenInClaudeCode opens the task's Claude Code session in a terminal on its
// machine for the task's creator.
func (h *AgentTaskHandler) OpenInClaudeCode(c *echo.Context) error {
	id, err := taskID(c, "taskId")
	if err != nil {
		return agentTaskError(c, err)
	}
	launch, err := h.svc.OpenInClaudeCode(c.Request().Context(), middleware.GetWorkspace(c).ID, id)
	if err != nil {
		return agentTaskError(c, err)
	}
	return response.Success(c, http.StatusOK, launch)
}

func (h *AgentTaskHandler) Retry(c *echo.Context) error {
	id, err := taskID(c, "taskId")
	if err != nil {
		return agentTaskError(c, err)
	}
	var input domain.AgentTaskRetryRequest
	if err = bindTask(c, &input); err != nil {
		return agentTaskError(c, err)
	}
	// A person retried with these instructions: when they differ from what
	// the task's notebook already holds, they are recorded in it.
	request, _, _ := strings.Cut(input.Prompt, domain.TaskFollowUpMarker)
	input.Note = &domain.NotebookNote{Instruction: &domain.NotebookInstruction{Source: domain.NotebookInstructionRetry, Text: request}}
	result, err := h.svc.Retry(c.Request().Context(), middleware.GetWorkspace(c).ID, id, input)
	if err != nil {
		return agentTaskError(c, err)
	}
	return response.Success(c, http.StatusAccepted, result)
}

// Pause pauses the workflow of the creator's task: "now" stops the agent
// that is working, "after_step" waits for the current step's outcome.
func (h *AgentTaskHandler) Pause(c *echo.Context) error {
	id, err := taskID(c, "taskId")
	if err != nil {
		return agentTaskError(c, err)
	}
	var input struct {
		Mode string `json:"mode"`
	}
	if err = bindTask(c, &input); err != nil {
		return agentTaskError(c, err)
	}
	result, err := h.svc.PauseWorkflow(c.Request().Context(), middleware.GetWorkspace(c).ID, id, input.Mode)
	if err != nil {
		return agentTaskError(c, err)
	}
	return response.Success(c, http.StatusAccepted, result)
}

// WorkflowSwitch previews the creator's requested move.
func (h *AgentTaskHandler) WorkflowSwitch(c *echo.Context) error {
	id, err := taskID(c, "taskId")
	if err != nil {
		return agentTaskError(c, err)
	}
	target := domain.WorkflowSwitchTarget{WorkflowID: c.QueryParam("workflow_id"), Node: c.QueryParam("node")}
	if !validWorkflowSwitchTarget(target) {
		return agentTaskError(c, repository.ErrConversationInvalid)
	}
	plan, _, err := h.svc.SwitchWorkflow(c.Request().Context(), middleware.GetWorkspace(c).ID, id, target, false)
	if err != nil {
		return workflowSwitchError(c, target, err)
	}
	return response.Success(c, http.StatusOK, plan)
}

// SwitchWorkflow applies the creator's requested move.
func (h *AgentTaskHandler) SwitchWorkflow(c *echo.Context) error {
	id, err := taskID(c, "taskId")
	if err != nil {
		return agentTaskError(c, err)
	}
	var target domain.WorkflowSwitchTarget
	if c.Request().ContentLength != 0 {
		if err := bindTask(c, &target); err != nil {
			return agentTaskError(c, err)
		}
	}
	if !validWorkflowSwitchTarget(target) {
		return agentTaskError(c, repository.ErrConversationInvalid)
	}
	plan, task, err := h.svc.SwitchWorkflow(c.Request().Context(), middleware.GetWorkspace(c).ID, id, target, true)
	if err != nil {
		return workflowSwitchError(c, target, err)
	}
	if task == nil {
		return response.Error(c, http.StatusConflict, "WORKFLOW_SWITCH_REFUSED", plan.Refused)
	}
	return response.Success(c, http.StatusOK, map[string]any{"switch": plan, "task": task})
}

func validWorkflowSwitchTarget(target domain.WorkflowSwitchTarget) bool {
	if target.WorkflowID == "" {
		return target.Node == "" && target.ExpectedVersionID == nil
	}
	return strings.TrimSpace(target.Node) != ""
}

func workflowSwitchError(c *echo.Context, target domain.WorkflowSwitchTarget, err error) error {
	if target.WorkflowID != "" && errors.Is(err, repository.ErrConversationNotFound) {
		// Use the same refusal for a missing or inaccessible task.
		return response.Error(c, http.StatusNotFound, "WORKFLOW_SWITCH_REFUSED", "not_creator")
	}
	return agentTaskError(c, err)
}

// ResumeUsageLimit is "Resume" on a paused task: a workflow its creator
// paused, or "Resume now" on a task paused by its agent's usage
// limit: the next attempt starts at once and continues the same session.
func (h *AgentTaskHandler) ResumeUsageLimit(c *echo.Context) error {
	id, err := taskID(c, "taskId")
	if err != nil {
		return agentTaskError(c, err)
	}
	result, err := h.svc.Resume(c.Request().Context(), middleware.GetWorkspace(c).ID, id)
	if err != nil {
		return agentTaskError(c, err)
	}
	return response.Success(c, http.StatusAccepted, result)
}

// CancelUsageLimitResume is "Don't resume": the paused task stays paused.
func (h *AgentTaskHandler) CancelUsageLimitResume(c *echo.Context) error {
	id, err := taskID(c, "taskId")
	if err != nil {
		return agentTaskError(c, err)
	}
	result, err := h.svc.CancelUsageLimitResume(c.Request().Context(), middleware.GetWorkspace(c).ID, id)
	if err != nil {
		return agentTaskError(c, err)
	}
	return response.Success(c, http.StatusOK, result)
}

// Accept completes the creator's task awaiting review without merging its pull
// request. The generation is the attempt the user reviewed; repeating it
// returns the accepted task.
func (h *AgentTaskHandler) Accept(c *echo.Context) error {
	id, err := taskID(c, "taskId")
	if err != nil {
		return agentTaskError(c, err)
	}
	var input struct {
		Generation int64 `json:"generation"`
	}
	if err = bindTask(c, &input); err != nil {
		return agentTaskError(c, err)
	}
	task, err := h.svc.Accept(c.Request().Context(), middleware.GetWorkspace(c).ID, id, input.Generation)
	if err != nil {
		return agentTaskError(c, err)
	}
	return response.Success(c, http.StatusOK, task)
}
func (h *AgentTaskHandler) History(c *echo.Context) error {
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
func (h *AgentTaskHandler) Runs(c *echo.Context) error {
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
func (h *AgentTaskHandler) Logs(c *echo.Context) error {
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

// Notebook returns the task's notebook and its newest changes to the task's
// creator. There is no write route: agents update it through their outbox,
// and people add instructions through the task's message routes.
func (h *AgentTaskHandler) Notebook(c *echo.Context) error {
	id, err := taskID(c, "taskId")
	if err != nil {
		return agentTaskError(c, err)
	}
	result, err := h.svc.Notebook(c.Request().Context(), middleware.GetWorkspace(c).ID, id)
	if err != nil {
		return agentTaskError(c, err)
	}
	c.Response().Header().Set("Cache-Control", "private, no-store")
	return response.Success(c, http.StatusOK, result)
}

// Prompt returns the prompt an attempt received, split by source.
func (h *AgentTaskHandler) Prompt(c *echo.Context) error {
	id, err := taskID(c, "taskId")
	if err != nil {
		return agentTaskError(c, err)
	}
	run, err := taskID(c, "runId")
	if err != nil {
		return agentTaskError(c, err)
	}
	result, err := h.svc.RunPrompt(c.Request().Context(), middleware.GetWorkspace(c).ID, id, run)
	if err != nil {
		return agentTaskError(c, err)
	}
	c.Response().Header().Set("Cache-Control", "private, no-store")
	return response.Success(c, http.StatusOK, result)
}

// DebugTrace downloads the task's full debug trace as one JSON file. The
// document is assembled under the task's access rules and redacted; it is
// not wrapped in the usual response envelope so the file is the document.
func (h *AgentTaskHandler) DebugTrace(c *echo.Context) error {
	id, err := taskID(c, "taskId")
	if err != nil {
		return agentTaskError(c, err)
	}
	result, err := h.svc.DebugTrace(c.Request().Context(), middleware.GetWorkspace(c).ID, id)
	if err != nil {
		return agentTaskError(c, err)
	}
	name := result.Task.Identifier
	if name == "" {
		name = result.Task.ID.String()
	}
	name = "kuayle-trace-" + unsafeArtifactName.ReplaceAllString(name, "_") + ".json"
	headers := c.Response().Header()
	headers.Set("X-Content-Type-Options", "nosniff")
	headers.Set("Cache-Control", "private, no-store")
	headers.Set("Content-Disposition", `attachment; filename="`+name+`"`)
	return c.JSON(http.StatusOK, result)
}

// Diff returns an attempt's captured diff against its base branch.
func (h *AgentTaskHandler) Diff(c *echo.Context) error {
	id, err := taskID(c, "taskId")
	if err != nil {
		return agentTaskError(c, err)
	}
	run, err := taskID(c, "runId")
	if err != nil {
		return agentTaskError(c, err)
	}
	result, err := h.svc.Repo.Diff(c.Request().Context(), middleware.GetWorkspace(c).ID, id, run)
	if err != nil {
		return agentTaskError(c, err)
	}
	c.Response().Header().Set("Cache-Control", "private, no-store")
	return response.Success(c, http.StatusOK, result)
}

// Files lists one directory of the files a commit of the task tracks
// (?ref=, a full commit id, the task's head by default; ?path=, the
// directory, the root by default) for its creator, or, with ?query=, the
// files whose path contains that text. Untracked and ignored files are never
// listed.
func (h *AgentTaskHandler) Files(c *echo.Context) error {
	c.Response().Header().Set("Cache-Control", "private, no-store")
	id, err := taskID(c, "taskId")
	if err != nil {
		return agentTaskError(c, err)
	}
	result, err := h.svc.TaskFiles(c.Request().Context(), middleware.GetWorkspace(c).ID, id,
		service.TaskFilesQuery{Ref: c.QueryParam("ref"), Path: c.QueryParam("path"), Query: c.QueryParam("query")})
	if err != nil {
		return agentTaskError(c, err)
	}
	return response.Success(c, http.StatusOK, result)
}

// FileContent returns lines start to end of one file of that listing
// (?path=&ref=&start=&end=), a page at a time; a binary or too large file
// carries no content.
func (h *AgentTaskHandler) FileContent(c *echo.Context) error {
	c.Response().Header().Set("Cache-Control", "private, no-store")
	id, err := taskID(c, "taskId")
	if err != nil {
		return agentTaskError(c, err)
	}
	name := c.QueryParam("path")
	start, startErr := optionalLine(c.QueryParam("start"))
	end, endErr := optionalLine(c.QueryParam("end"))
	if name == "" || startErr != nil || endErr != nil {
		return agentTaskError(c, repository.ErrConversationInvalid)
	}
	result, err := h.svc.TaskFileContent(c.Request().Context(), middleware.GetWorkspace(c).ID, id, name, c.QueryParam("ref"), start, end)
	if err != nil {
		return agentTaskError(c, err)
	}
	return response.Success(c, http.StatusOK, result)
}

// optionalLine reads a 1-based line number; empty is 0 (unset).
func optionalLine(raw string) (int, error) {
	if raw == "" {
		return 0, nil
	}
	line, err := strconv.Atoi(raw)
	if err != nil || line < 1 {
		return 0, repository.ErrConversationInvalid
	}
	return line, nil
}

func (h *AgentTaskHandler) Publication(c *echo.Context) error {
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

func (h *AgentTaskHandler) Signals(c *echo.Context) error {
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

// LiveMessages lists the newest messages of the task's live channel, both
// directions, oldest first, for the task's creator. Agent text is untrusted.
func (h *AgentTaskHandler) LiveMessages(c *echo.Context) error {
	id, err := taskID(c, "taskId")
	if err != nil {
		return agentTaskError(c, err)
	}
	messages, err := h.svc.LiveMessages(c.Request().Context(), middleware.GetWorkspace(c).ID, id, 100)
	if err != nil {
		return agentTaskError(c, err)
	}
	return response.Success(c, http.StatusOK, map[string]any{"messages": messages})
}

type liveMessageRequest struct {
	AttachmentIDs []uuid.UUID `json:"attachment_ids"`
	Body          string      `json:"body"`
	// Mode is empty, "interrupt" (stop the working agent and resume its
	// session with the message) or "start" (start an attempt for it).
	Mode string `json:"mode"`
	// ConfirmMerged starts that attempt although the issue's work was merged.
	ConfirmMerged bool `json:"confirm_merged"`
}

// SendLiveMessage queues the creator's message for the task's agent: the
// running attempt reads it from its inbox, otherwise the next attempt's
// prompt carries it. With a mode it also interrupts the working agent, or
// starts an attempt, so the message is read now.
func (h *AgentTaskHandler) SendLiveMessage(c *echo.Context) error {
	id, err := taskID(c, "taskId")
	if err != nil {
		return agentTaskError(c, err)
	}
	var input liveMessageRequest
	if err = bindTask(c, &input); err != nil {
		return agentTaskError(c, err)
	}
	message, err := h.svc.SendLiveMessage(c.Request().Context(), middleware.GetWorkspace(c).ID, id, input.Body, input.Mode, input.ConfirmMerged, input.AttachmentIDs...)
	if err != nil {
		return agentTaskError(c, err)
	}
	return response.Success(c, http.StatusCreated, message)
}

// Room lists the room of a pipeline task after a sequence number (?after=),
// oldest first, for its creator. Text from agents is untrusted.
func (h *AgentTaskHandler) Room(c *echo.Context) error {
	id, err := taskID(c, "taskId")
	if err != nil {
		return agentTaskError(c, err)
	}
	after, err := conversationCursor(c, "after")
	if err != nil {
		return agentTaskError(c, err)
	}
	messages, err := h.svc.Repo.RoomMessages(c.Request().Context(), middleware.GetWorkspace(c).ID, id, after)
	if err != nil {
		return agentTaskError(c, err)
	}
	c.Response().Header().Set("Cache-Control", "private, no-store")
	return response.Success(c, http.StatusOK, map[string]any{"messages": messages})
}

type roomPostRequest struct {
	// RequestID makes a repeated post return the first one.
	RequestID *uuid.UUID          `json:"request_id"`
	Kind      string              `json:"kind"`
	Body      string              `json:"body"`
	To        []string            `json:"to"`
	Data      domain.TaskRoomData `json:"data"`
	ReplyTo   *int64              `json:"reply_to"`
}

// PostRoom posts the creator's say, ask, request, waiver or grant to the
// room of a pipeline task.
func (h *AgentTaskHandler) PostRoom(c *echo.Context) error {
	id, err := taskID(c, "taskId")
	if err != nil {
		return agentTaskError(c, err)
	}
	var input roomPostRequest
	if err = bindTask(c, &input); err != nil {
		return agentTaskError(c, err)
	}
	post := domain.TaskRoomPost{Kind: input.Kind, Body: input.Body, ToRoles: input.To, Data: input.Data, ReplyTo: input.ReplyTo}
	if input.RequestID != nil {
		post.DedupeKey = "request:" + input.RequestID.String()
	}
	message, err := h.svc.PostRoomMessage(c.Request().Context(), middleware.GetWorkspace(c).ID, id, post)
	if err != nil {
		return agentTaskError(c, err)
	}
	return response.Success(c, http.StatusCreated, message)
}

// ApproveSplit accepts the proposal already stored at seq. Only the room
// request supplies the children to create.
func (h *AgentTaskHandler) ApproveSplit(c *echo.Context) error {
	id, err := taskID(c, "taskId")
	if err != nil {
		return agentTaskError(c, err)
	}
	seq, err := strconv.ParseInt(c.Param("seq"), 10, 64)
	if err != nil || seq < 1 {
		return agentTaskError(c, repository.ErrConversationInvalid)
	}
	if err := h.svc.ApproveTaskSplit(c.Request().Context(), middleware.GetWorkspace(c).ID, id, seq); err != nil {
		return agentTaskError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

// Queue lists the session user's recent agent tasks across projects.
func (h *AgentTaskHandler) Queue(c *echo.Context) error {
	ctx, ws := c.Request().Context(), middleware.GetWorkspace(c).ID
	var result []repository.AgentQueueItem
	var err error
	if c.QueryParam("needs_me") == "true" {
		result, err = h.svc.Repo.NeedsMe(ctx, ws, c.QueryParam("issue"))
	} else {
		result, err = h.svc.Repo.Queue(ctx, ws)
	}
	if err != nil {
		return agentTaskError(c, err)
	}
	return response.Success(c, http.StatusOK, result)
}

// Board lists the actor's active tasks on one repository in merge order,
// with the files they share (GET /agent-tasks/board?repository=owner/name),
// under the visibility of Queue. It names private repository paths, so it
// is never cached.
func (h *AgentTaskHandler) Board(c *echo.Context) error {
	repository := strings.TrimSpace(c.QueryParam("repository"))
	if repository == "" || len(repository) > 512 {
		return response.Error(c, http.StatusBadRequest, "INVALID_REPOSITORY", "Name the repository as owner/name")
	}
	result, err := h.svc.Board(c.Request().Context(), middleware.GetWorkspace(c).ID, repository)
	if err != nil {
		return agentTaskError(c, err)
	}
	c.Response().Header().Set("Cache-Control", "private, no-store")
	return response.Success(c, http.StatusOK, result)
}

// MachineTasks lists the tasks that used one of the session user's machines.
func (h *AgentTaskHandler) MachineTasks(c *echo.Context) error {
	id, err := taskID(c, "machineId")
	if err != nil {
		return agentTaskError(c, err)
	}
	result, err := h.svc.Repo.MachineTasks(c.Request().Context(), middleware.GetWorkspace(c).ID, id)
	if err != nil {
		return agentTaskError(c, err)
	}
	return response.Success(c, http.StatusOK, result)
}

func (h *AgentTaskHandler) Artifacts(c *echo.Context) error {
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
func (h *AgentTaskHandler) Artifact(c *echo.Context) error {
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
