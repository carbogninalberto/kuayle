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

// Skill bodies are at most 64 KiB; JSON escaping may grow them.
const assistantSkillRequestBytes = 512 << 10

// AssistantSkillHandler serves Settings → Assistant → Skills.
type AssistantSkillHandler struct {
	svc *service.AssistantSkillService
}

func NewAssistantSkillHandler(svc *service.AssistantSkillService) *AssistantSkillHandler {
	return &AssistantSkillHandler{svc: svc}
}

func skillError(c echo.Context, err error) error {
	var invalid *service.ErrInvalidAssistantSkill
	switch {
	case errors.As(err, &invalid):
		return response.Error(c, http.StatusBadRequest, "INVALID_ASSISTANT_SKILL", invalid.Message)
	case errors.Is(err, service.ErrAssistantSkillsDenied), errors.Is(err, repository.ErrAssistantSkillDenied):
		return response.Forbidden(c)
	case errors.Is(err, repository.ErrAssistantSkillNotFound):
		return response.NotFound(c, "Assistant skill")
	case errors.Is(err, repository.ErrAssistantSkillNameTaken):
		return response.Error(c, http.StatusConflict, "ASSISTANT_SKILL_NAME_TAKEN", "A skill with this name already exists in this scope")
	case errors.Is(err, repository.ErrAssistantSkillNotDraft):
		return response.Error(c, http.StatusConflict, "ASSISTANT_SKILL_NOT_DRAFT", "There is no draft to activate")
	default:
		return response.InternalError(c)
	}
}

// decodeSkillJSON rejects unknown fields and oversized bodies.
func decodeSkillJSON(c echo.Context, target any) bool {
	decoder := json.NewDecoder(io.LimitReader(c.Request().Body, assistantSkillRequestBytes))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target) == nil
}

func invalidSkillBody(c echo.Context) error {
	return response.Error(c, http.StatusBadRequest, "INVALID_ASSISTANT_SKILL", "Invalid request body")
}

func skillID(c echo.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("skillId"))
	return id, err == nil
}

func (h *AssistantSkillHandler) List(c echo.Context) error {
	result, err := h.svc.List(c.Request().Context(), middleware.GetWorkspace(c).ID)
	if err != nil {
		return skillError(c, err)
	}
	return response.Success(c, http.StatusOK, result)
}

func (h *AssistantSkillHandler) Get(c echo.Context) error {
	id, ok := skillID(c)
	if !ok {
		return response.NotFound(c, "Assistant skill")
	}
	result, err := h.svc.Get(c.Request().Context(), middleware.GetWorkspace(c).ID, id)
	if err != nil {
		return skillError(c, err)
	}
	return response.Success(c, http.StatusOK, result)
}

func (h *AssistantSkillHandler) Versions(c echo.Context) error {
	id, ok := skillID(c)
	if !ok {
		return response.NotFound(c, "Assistant skill")
	}
	result, err := h.svc.Versions(c.Request().Context(), middleware.GetWorkspace(c).ID, id)
	if err != nil {
		return skillError(c, err)
	}
	return response.Success(c, http.StatusOK, map[string]any{"versions": result})
}

func (h *AssistantSkillHandler) Create(c echo.Context) error {
	var input service.AssistantSkillRequest
	if !decodeSkillJSON(c, &input) {
		return invalidSkillBody(c)
	}
	result, err := h.svc.Create(c.Request().Context(), middleware.GetWorkspace(c).ID, input)
	if err != nil {
		return skillError(c, err)
	}
	return response.Success(c, http.StatusCreated, result)
}

// Update saves a new version; the team scope is fixed at creation.
func (h *AssistantSkillHandler) Update(c echo.Context) error {
	id, ok := skillID(c)
	if !ok {
		return response.NotFound(c, "Assistant skill")
	}
	var input struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Body        string `json:"body"`
		Status      string `json:"status"`
	}
	if !decodeSkillJSON(c, &input) {
		return invalidSkillBody(c)
	}
	result, err := h.svc.Update(c.Request().Context(), middleware.GetWorkspace(c).ID, id, service.AssistantSkillRequest{Name: input.Name, Description: input.Description, Body: input.Body, Status: input.Status})
	if err != nil {
		return skillError(c, err)
	}
	return response.Success(c, http.StatusOK, result)
}

func (h *AssistantSkillHandler) SetEnabled(c echo.Context) error {
	id, ok := skillID(c)
	if !ok {
		return response.NotFound(c, "Assistant skill")
	}
	var input struct {
		Enabled *bool `json:"enabled"`
	}
	if !decodeSkillJSON(c, &input) {
		return invalidSkillBody(c)
	}
	if input.Enabled == nil {
		return response.Error(c, http.StatusBadRequest, "INVALID_ASSISTANT_SKILL", "enabled must be true or false")
	}
	result, err := h.svc.SetEnabled(c.Request().Context(), middleware.GetWorkspace(c).ID, id, *input.Enabled)
	if err != nil {
		return skillError(c, err)
	}
	return response.Success(c, http.StatusOK, result)
}

// Activate puts a draft into effect: {"version": n}, or the newest draft when
// the body is empty.
func (h *AssistantSkillHandler) Activate(c echo.Context) error {
	id, ok := skillID(c)
	if !ok {
		return response.NotFound(c, "Assistant skill")
	}
	var input struct {
		Version *int `json:"version"`
	}
	raw, err := io.ReadAll(io.LimitReader(c.Request().Body, 1024))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "INVALID_ASSISTANT_SKILL", "Invalid request body")
	}
	if len(bytes.TrimSpace(raw)) > 0 {
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&input) != nil {
			return response.Error(c, http.StatusBadRequest, "INVALID_ASSISTANT_SKILL", "Invalid request body")
		}
	}
	result, err := h.svc.Activate(c.Request().Context(), middleware.GetWorkspace(c).ID, id, input.Version)
	if err != nil {
		return skillError(c, err)
	}
	return response.Success(c, http.StatusOK, result)
}

func (h *AssistantSkillHandler) Restore(c echo.Context) error {
	id, ok := skillID(c)
	version, err := strconv.Atoi(c.Param("version"))
	if !ok || err != nil || version < 1 {
		return response.NotFound(c, "Assistant skill version")
	}
	result, err := h.svc.Restore(c.Request().Context(), middleware.GetWorkspace(c).ID, id, version)
	if err != nil {
		return skillError(c, err)
	}
	return response.Success(c, http.StatusOK, result)
}

func (h *AssistantSkillHandler) Delete(c echo.Context) error {
	id, ok := skillID(c)
	if !ok {
		return response.NotFound(c, "Assistant skill")
	}
	if err := h.svc.Delete(c.Request().Context(), middleware.GetWorkspace(c).ID, id); err != nil {
		return skillError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}
