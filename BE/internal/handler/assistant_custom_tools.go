package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/kuayle/kuayle-backend/pkg/response"
	"github.com/labstack/echo/v4"
)

// Scripts are at most 64 KiB; JSON escaping may grow them.
const assistantToolRequestBytes = 512 << 10

// AssistantCustomToolHandler serves Settings → Assistant → Custom tools.
type AssistantCustomToolHandler struct {
	svc *service.AssistantCustomToolService
}

func NewAssistantCustomToolHandler(svc *service.AssistantCustomToolService) *AssistantCustomToolHandler {
	return &AssistantCustomToolHandler{svc: svc}
}

func customToolError(c echo.Context, err error) error {
	var invalid *service.ErrInvalidAssistantTool
	switch {
	case errors.As(err, &invalid):
		return response.Error(c, http.StatusBadRequest, "INVALID_ASSISTANT_TOOL", invalid.Message)
	case errors.Is(err, service.ErrAssistantToolsDenied), errors.Is(err, repository.ErrAssistantToolDenied), errors.Is(err, repository.ErrToolRunForbidden):
		return response.Forbidden(c)
	case errors.Is(err, repository.ErrAssistantToolNotFound), errors.Is(err, repository.ErrConversationNotFound):
		return response.NotFound(c, "Custom tool")
	case errors.Is(err, repository.ErrAssistantToolNameTaken):
		return response.Error(c, http.StatusConflict, "ASSISTANT_TOOL_NAME_TAKEN", "A custom tool with this name already exists")
	case errors.Is(err, repository.ErrAssistantToolNotDraft):
		return response.Error(c, http.StatusConflict, "ASSISTANT_TOOL_NOT_DRAFT", "There is no draft to activate")
	case errors.Is(err, repository.ErrMachineBusy):
		return response.Error(c, http.StatusConflict, "MACHINE_BUSY", "The agent is working on this machine; try again when it pauses")
	case errors.Is(err, repository.ErrToolMachineUnavailable):
		return response.Error(c, http.StatusConflict, "TOOL_MACHINE_UNAVAILABLE", "The tool needs a running machine of yours: the task's machine with its checkout, or one of your machines")
	default:
		return response.InternalError(c)
	}
}

func decodeToolJSON(c echo.Context, target any) bool {
	decoder := json.NewDecoder(io.LimitReader(c.Request().Body, assistantToolRequestBytes))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target) == nil
}

func invalidToolBody(c echo.Context) error {
	return response.Error(c, http.StatusBadRequest, "INVALID_ASSISTANT_TOOL", "Invalid request body")
}

func customToolParam(c echo.Context, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param(name))
	return id, err == nil
}

func (h *AssistantCustomToolHandler) List(c echo.Context) error {
	result, err := h.svc.List(c.Request().Context(), middleware.GetWorkspace(c).ID)
	if err != nil {
		return customToolError(c, err)
	}
	return response.Success(c, http.StatusOK, result)
}

func (h *AssistantCustomToolHandler) Get(c echo.Context) error {
	id, ok := customToolParam(c, "toolId")
	if !ok {
		return response.NotFound(c, "Custom tool")
	}
	result, err := h.svc.Get(c.Request().Context(), middleware.GetWorkspace(c).ID, id)
	if err != nil {
		return customToolError(c, err)
	}
	return response.Success(c, http.StatusOK, result)
}

func (h *AssistantCustomToolHandler) Versions(c echo.Context) error {
	id, ok := customToolParam(c, "toolId")
	if !ok {
		return response.NotFound(c, "Custom tool")
	}
	result, err := h.svc.Versions(c.Request().Context(), middleware.GetWorkspace(c).ID, id)
	if err != nil {
		return customToolError(c, err)
	}
	return response.Success(c, http.StatusOK, map[string]any{"versions": result})
}

func (h *AssistantCustomToolHandler) Create(c echo.Context) error {
	var input service.AssistantCustomToolRequest
	if !decodeToolJSON(c, &input) {
		return invalidToolBody(c)
	}
	result, err := h.svc.Create(c.Request().Context(), middleware.GetWorkspace(c).ID, input)
	if err != nil {
		return customToolError(c, err)
	}
	return response.Success(c, http.StatusCreated, result)
}

// Update saves the request as a new version.
func (h *AssistantCustomToolHandler) Update(c echo.Context) error {
	id, ok := customToolParam(c, "toolId")
	if !ok {
		return response.NotFound(c, "Custom tool")
	}
	var input service.AssistantCustomToolRequest
	if !decodeToolJSON(c, &input) {
		return invalidToolBody(c)
	}
	result, err := h.svc.Update(c.Request().Context(), middleware.GetWorkspace(c).ID, id, input)
	if err != nil {
		return customToolError(c, err)
	}
	return response.Success(c, http.StatusOK, result)
}

func (h *AssistantCustomToolHandler) SetEnabled(c echo.Context) error {
	id, ok := customToolParam(c, "toolId")
	if !ok {
		return response.NotFound(c, "Custom tool")
	}
	var input struct {
		Enabled *bool `json:"enabled"`
	}
	if !decodeToolJSON(c, &input) || input.Enabled == nil {
		return response.Error(c, http.StatusBadRequest, "INVALID_ASSISTANT_TOOL", "enabled must be true or false")
	}
	result, err := h.svc.SetEnabled(c.Request().Context(), middleware.GetWorkspace(c).ID, id, *input.Enabled)
	if err != nil {
		return customToolError(c, err)
	}
	return response.Success(c, http.StatusOK, result)
}

// Activate puts a draft into effect: {"version": n}, or the newest draft when
// the body is empty.
func (h *AssistantCustomToolHandler) Activate(c echo.Context) error {
	id, ok := customToolParam(c, "toolId")
	if !ok {
		return response.NotFound(c, "Custom tool")
	}
	var input struct {
		Version *int `json:"version"`
	}
	raw, err := io.ReadAll(io.LimitReader(c.Request().Body, 1024))
	if err != nil {
		return invalidToolBody(c)
	}
	if len(bytes.TrimSpace(raw)) > 0 {
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&input) != nil {
			return invalidToolBody(c)
		}
	}
	result, err := h.svc.Activate(c.Request().Context(), middleware.GetWorkspace(c).ID, id, input.Version)
	if err != nil {
		return customToolError(c, err)
	}
	return response.Success(c, http.StatusOK, result)
}

func (h *AssistantCustomToolHandler) Restore(c echo.Context) error {
	id, ok := customToolParam(c, "toolId")
	version, err := strconv.Atoi(c.Param("version"))
	if !ok || err != nil || version < 1 {
		return response.NotFound(c, "Custom tool version")
	}
	result, err := h.svc.Restore(c.Request().Context(), middleware.GetWorkspace(c).ID, id, version)
	if err != nil {
		return customToolError(c, err)
	}
	return response.Success(c, http.StatusOK, result)
}

func (h *AssistantCustomToolHandler) Delete(c echo.Context) error {
	id, ok := customToolParam(c, "toolId")
	if !ok {
		return response.NotFound(c, "Custom tool")
	}
	if err := h.svc.Delete(c.Request().Context(), middleware.GetWorkspace(c).ID, id); err != nil {
		return customToolError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

// TestRun queues an owner's or admin's run of a saved version with sample
// inputs: {"inputs":{...},"machine_id"|"identifier":...,"version"?:n}.
func (h *AssistantCustomToolHandler) TestRun(c echo.Context) error {
	id, ok := customToolParam(c, "toolId")
	if !ok {
		return response.NotFound(c, "Custom tool")
	}
	var input service.AssistantToolTestRunRequest
	if !decodeToolJSON(c, &input) {
		return invalidToolBody(c)
	}
	result, err := h.svc.TestRun(c.Request().Context(), middleware.GetWorkspace(c).ID, id, input)
	if err != nil {
		return customToolError(c, err)
	}
	return response.Success(c, http.StatusAccepted, result)
}

func (h *AssistantCustomToolHandler) Runs(c echo.Context) error {
	id, ok := customToolParam(c, "toolId")
	if !ok {
		return response.NotFound(c, "Custom tool")
	}
	result, err := h.svc.Runs(c.Request().Context(), middleware.GetWorkspace(c).ID, id)
	if err != nil {
		return customToolError(c, err)
	}
	return response.Success(c, http.StatusOK, map[string]any{"runs": result})
}

func (h *AssistantCustomToolHandler) Run(c echo.Context) error {
	id, ok := customToolParam(c, "runId")
	if !ok {
		return response.NotFound(c, "Custom tool run")
	}
	result, err := h.svc.Run(c.Request().Context(), middleware.GetWorkspace(c).ID, id)
	if err != nil {
		return customToolError(c, err)
	}
	return response.Success(c, http.StatusOK, result)
}
