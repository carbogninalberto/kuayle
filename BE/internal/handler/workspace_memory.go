package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/kuayle/kuayle-backend/pkg/response"
	"github.com/labstack/echo/v5"
)

// A lesson is a sentence or two; a reorder names at most a repository's
// approved lessons.
const memoryRequestBytes = 16 << 10

// WorkspaceMemoryHandler serves the workspace memory: the lessons of the
// workspace's repositories and the proposals waiting for a decision.
type WorkspaceMemoryHandler struct {
	svc *service.WorkspaceMemoryService
}

func NewWorkspaceMemoryHandler(svc *service.WorkspaceMemoryService) *WorkspaceMemoryHandler {
	return &WorkspaceMemoryHandler{svc: svc}
}

func memoryError(c *echo.Context, err error) error {
	switch {
	case errors.Is(err, service.ErrMemoryDenied), errors.Is(err, repository.ErrLessonDenied):
		return response.Forbidden(c)
	case errors.Is(err, repository.ErrLessonNotFound):
		return response.NotFound(c, "Lesson")
	case errors.Is(err, domain.ErrLessonEmpty):
		return response.Error(c, http.StatusBadRequest, "INVALID_LESSON", "A lesson needs some text")
	case errors.Is(err, domain.ErrLessonTooLong):
		return response.Error(c, http.StatusBadRequest, "INVALID_LESSON", "A lesson is at most 400 characters")
	case errors.Is(err, repository.ErrLessonRepository):
		return response.Error(c, http.StatusBadRequest, "LESSON_REPOSITORY", "This repository is not connected to the workspace")
	case errors.Is(err, repository.ErrLessonOrder):
		return response.Error(c, http.StatusBadRequest, "LESSON_ORDER", "The order must name each approved lesson of the repository once")
	case errors.Is(err, repository.ErrLessonState):
		return response.Error(c, http.StatusConflict, "LESSON_STATE", "The lesson is not in a state that allows this any more")
	case errors.Is(err, repository.ErrLessonLimit):
		return response.Error(c, http.StatusConflict, "LESSON_LIMIT", "This repository has its maximum of approved lessons; retire one first")
	case errors.Is(err, repository.ErrLessonPendingLimit):
		return response.Error(c, http.StatusConflict, "LESSON_PENDING_LIMIT", "Too many proposals wait on this repository")
	case errors.Is(err, repository.ErrLessonDuplicate):
		return response.Error(c, http.StatusConflict, "LESSON_DUPLICATE", "This lesson already exists for the repository")
	default:
		return response.Error(c, http.StatusInternalServerError, "INTERNAL_ERROR", "Internal server error")
	}
}

func decodeMemoryJSON(c *echo.Context, target any) bool {
	decoder := json.NewDecoder(io.LimitReader(c.Request().Body, memoryRequestBytes))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target) == nil
}

func invalidMemoryBody(c *echo.Context) error {
	return response.Error(c, http.StatusBadRequest, "INVALID_LESSON", "Invalid request body")
}

func lessonID(c *echo.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("lessonId"))
	return id, err == nil
}

// List returns the lessons the caller can see: ?repository=owner/name narrows
// to one repository and ?state= to proposed, approved, retired or dropped.
func (h *WorkspaceMemoryHandler) List(c *echo.Context) error {
	state := c.QueryParam("state")
	switch state {
	case "", domain.LessonProposed, domain.LessonApproved, domain.LessonRetired, domain.LessonDropped:
	default:
		return response.Error(c, http.StatusBadRequest, "INVALID_LESSON", "Unknown state")
	}
	result, err := h.svc.List(c.Request().Context(), workspaceID(c), c.QueryParam("repository"), state)
	if err != nil {
		return memoryError(c, err)
	}
	return response.Success(c, http.StatusOK, result)
}

// Add records a lesson the caller wrote by hand.
func (h *WorkspaceMemoryHandler) Add(c *echo.Context) error {
	var in struct {
		Repository string `json:"repository"`
		Text       string `json:"text"`
	}
	if !decodeMemoryJSON(c, &in) {
		return invalidMemoryBody(c)
	}
	result, err := h.svc.Add(c.Request().Context(), workspaceID(c), in.Repository, in.Text)
	if err != nil {
		return memoryError(c, err)
	}
	return response.Success(c, http.StatusCreated, result)
}

// Approve approves a proposal, optionally with edited text.
func (h *WorkspaceMemoryHandler) Approve(c *echo.Context) error {
	id, ok := lessonID(c)
	if !ok {
		return response.NotFound(c, "Lesson")
	}
	var in struct {
		Text *string `json:"text"`
	}
	if c.Request().ContentLength != 0 && !decodeMemoryJSON(c, &in) {
		return invalidMemoryBody(c)
	}
	result, err := h.svc.Approve(c.Request().Context(), workspaceID(c), id, in.Text)
	if err != nil {
		return memoryError(c, err)
	}
	return response.Success(c, http.StatusOK, result)
}

// Edit changes a lesson's text or shares a private team's lesson with the
// whole workspace.
func (h *WorkspaceMemoryHandler) Edit(c *echo.Context) error {
	id, ok := lessonID(c)
	if !ok {
		return response.NotFound(c, "Lesson")
	}
	var in struct {
		Text          *string `json:"text"`
		WorkspaceWide bool    `json:"workspace_wide"`
	}
	if !decodeMemoryJSON(c, &in) || in.Text == nil && !in.WorkspaceWide {
		return invalidMemoryBody(c)
	}
	result, err := h.svc.Edit(c.Request().Context(), workspaceID(c), id, in.Text, in.WorkspaceWide)
	if err != nil {
		return memoryError(c, err)
	}
	return response.Success(c, http.StatusOK, result)
}

func (h *WorkspaceMemoryHandler) decide(c *echo.Context, decide func(ctx context.Context, ws, id uuid.UUID) (*domain.Lesson, error)) error {
	id, ok := lessonID(c)
	if !ok {
		return response.NotFound(c, "Lesson")
	}
	result, err := decide(c.Request().Context(), workspaceID(c), id)
	if err != nil {
		return memoryError(c, err)
	}
	return response.Success(c, http.StatusOK, result)
}

// Retire stops an approved lesson from reaching agents.
func (h *WorkspaceMemoryHandler) Retire(c *echo.Context) error { return h.decide(c, h.svc.Retire) }

// Drop refuses a proposal.
func (h *WorkspaceMemoryHandler) Drop(c *echo.Context) error { return h.decide(c, h.svc.Drop) }

// Reorder sets the prompt order of a repository's approved lessons.
func (h *WorkspaceMemoryHandler) Reorder(c *echo.Context) error {
	var in struct {
		Repository string      `json:"repository"`
		IDs        []uuid.UUID `json:"ids"`
	}
	if !decodeMemoryJSON(c, &in) || in.Repository == "" {
		return invalidMemoryBody(c)
	}
	lessons, err := h.svc.Reorder(c.Request().Context(), workspaceID(c), in.Repository, in.IDs)
	if err != nil {
		return memoryError(c, err)
	}
	return response.Success(c, http.StatusOK, map[string]any{"lessons": lessons})
}

// Audit lists a lesson's audit trail for owners and admins.
func (h *WorkspaceMemoryHandler) Audit(c *echo.Context) error {
	id, ok := lessonID(c)
	if !ok {
		return response.NotFound(c, "Lesson")
	}
	events, err := h.svc.Audit(c.Request().Context(), workspaceID(c), id)
	if err != nil {
		return memoryError(c, err)
	}
	return response.Success(c, http.StatusOK, map[string]any{"events": events})
}
