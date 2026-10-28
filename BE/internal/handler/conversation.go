package handler

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/kuayle/kuayle-backend/pkg/response"
	"github.com/labstack/echo/v4"
	"io"
	"net/http"
	"net/url"
	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
	"strconv"
	"strings"
	"time"
)

type ConversationHandler struct {
	svc    *service.ConversationService
	origin string
}

func NewConversationHandler(svc *service.ConversationService, frontendURL string) *ConversationHandler {
	u, _ := url.Parse(frontendURL)
	origin := ""
	if u != nil {
		origin = u.Scheme + "://" + u.Host
	}
	return &ConversationHandler{svc: svc, origin: origin}
}
func conversationError(c echo.Context, err error) error {
	switch {
	case errors.Is(err, repository.ErrConversationNotFound):
		return response.NotFound(c, "Conversation")
	case errors.Is(err, repository.ErrConversationBusy):
		return response.Error(c, http.StatusConflict, "CONVERSATION_BUSY", "A reply is already in progress")
	case errors.Is(err, repository.ErrConversationConflict):
		return response.Error(c, http.StatusConflict, "CONVERSATION_CONFLICT", "This request conflicts with the current conversation")
	case errors.Is(err, repository.ErrApprovalExpired):
		return response.Error(c, http.StatusGone, "APPROVAL_EXPIRED", "This approval has expired")
	case errors.Is(err, repository.ErrConversationInvalid):
		return response.Error(c, http.StatusBadRequest, "INVALID_CONVERSATION", "Invalid conversation request")
	case errors.Is(err, service.ErrAISettingsNotConfigured):
		return response.Error(c, http.StatusBadRequest, "AI_NOT_CONFIGURED", "Configure the workspace AI provider first")
	case errors.Is(err, service.ErrAttachmentTooLarge):
		return response.Error(c, http.StatusRequestEntityTooLarge, "ATTACHMENT_TOO_LARGE", "Images can be at most 10 MB")
	case errors.Is(err, service.ErrAttachmentUnsupported):
		return response.Error(c, http.StatusUnsupportedMediaType, "ATTACHMENT_UNSUPPORTED", "Only PNG, JPEG, WebP and GIF images are supported")
	case errors.Is(err, repository.ErrAttachmentLimit):
		return response.Error(c, http.StatusBadRequest, "ATTACHMENT_LIMIT", "Send or remove the attached images first")
	case errors.Is(err, service.ErrAttachmentsDisabled):
		return response.Error(c, http.StatusBadRequest, "ATTACHMENTS_UNAVAILABLE", "Image attachments are not available")
	default:
		return response.InternalError(c)
	}
}
func conversationID(c echo.Context) (uuid.UUID, error) {
	id, err := uuid.Parse(c.Param("conversationId"))
	if err != nil {
		return uuid.Nil, repository.ErrConversationNotFound
	}
	return id, nil
}
func conversationCursor(c echo.Context, name string) (int64, error) {
	raw := c.QueryParam(name)
	if raw == "" {
		return 0, nil
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || v < 0 {
		return 0, repository.ErrConversationInvalid
	}
	return v, nil
}
func (h *ConversationHandler) Status(c echo.Context) error {
	result, err := h.svc.Status(c.Request().Context(), middleware.GetWorkspace(c).ID)
	if err != nil {
		return conversationError(c, err)
	}
	return response.Success(c, http.StatusOK, result)
}

// List returns the caller's own conversations; ?project_id=<id> narrows them
// to one visible project and ?project_id=none to chats without one.
func (h *ConversationHandler) List(c echo.Context) error {
	var filter domain.ConversationFilter
	switch raw := c.QueryParam("project_id"); raw {
	case "":
	case "none":
		filter.NoProject = true
	default:
		project, err := uuid.Parse(raw)
		if err != nil {
			return conversationError(c, repository.ErrConversationInvalid)
		}
		filter.ProjectID = &project
	}
	result, err := h.svc.Repo.ListFiltered(c.Request().Context(), middleware.GetWorkspace(c).ID, filter)
	if err != nil {
		return conversationError(c, err)
	}
	return response.Success(c, http.StatusOK, result)
}
func (h *ConversationHandler) Create(c echo.Context) error {
	var req struct {
		Title     string     `json:"title"`
		IssueID   *uuid.UUID `json:"issue_id"`
		ProjectID *uuid.UUID `json:"project_id"`
	}
	if err := bindConversation(c, &req); err != nil {
		return conversationError(c, repository.ErrConversationInvalid)
	}
	var result *domain.Conversation
	var err error
	if req.IssueID != nil {
		// An issue chat follows its issue's project; it cannot take another.
		if req.ProjectID != nil {
			return conversationError(c, repository.ErrConversationInvalid)
		}
		result, err = h.svc.Repo.OpenIssue(c.Request().Context(), middleware.GetWorkspace(c).ID, *req.IssueID)
	} else {
		result, err = h.svc.Repo.CreateWithProject(c.Request().Context(), middleware.GetWorkspace(c).ID, req.Title, req.ProjectID)
	}
	if err != nil {
		return conversationError(c, err)
	}
	return response.Success(c, http.StatusCreated, result)
}
func (h *ConversationHandler) History(c echo.Context) error {
	id, err := conversationID(c)
	if err != nil {
		return conversationError(c, err)
	}
	before, err := conversationCursor(c, "before")
	if err != nil {
		return conversationError(c, err)
	}
	result, err := h.svc.Repo.History(c.Request().Context(), middleware.GetWorkspace(c).ID, id, before)
	if err != nil {
		return conversationError(c, err)
	}
	return response.Success(c, http.StatusOK, result)
}
func (h *ConversationHandler) Events(c echo.Context) error {
	id, err := conversationID(c)
	if err != nil {
		return conversationError(c, err)
	}
	after, err := conversationCursor(c, "after")
	if err != nil {
		return conversationError(c, err)
	}
	result, err := h.svc.Repo.Events(c.Request().Context(), middleware.GetWorkspace(c).ID, id, after)
	if err != nil {
		return conversationError(c, err)
	}
	return response.Success(c, http.StatusOK, result)
}
func (h *ConversationHandler) Send(c echo.Context) error {
	id, err := conversationID(c)
	if err != nil {
		return conversationError(c, err)
	}
	var req struct {
		RequestID     uuid.UUID   `json:"request_id"`
		Content       string      `json:"content"`
		AttachmentIDs []uuid.UUID `json:"attachment_ids"`
	}
	if err = bindConversation(c, &req); err != nil {
		return conversationError(c, repository.ErrConversationInvalid)
	}
	result, err := h.svc.SendMessage(c.Request().Context(), middleware.GetWorkspace(c).ID, id, req.RequestID, req.Content, req.AttachmentIDs)
	if err != nil {
		return conversationError(c, err)
	}
	return response.Success(c, http.StatusAccepted, result)
}
func (h *ConversationHandler) Retry(c echo.Context) error {
	id, err := conversationID(c)
	if err != nil {
		return conversationError(c, err)
	}
	message, err := uuid.Parse(c.Param("messageId"))
	if err != nil {
		return conversationError(c, repository.ErrConversationNotFound)
	}
	var req struct {
		RequestID uuid.UUID `json:"request_id"`
	}
	if err = bindConversation(c, &req); err != nil {
		return conversationError(c, repository.ErrConversationInvalid)
	}
	result, err := h.svc.Retry(c.Request().Context(), middleware.GetWorkspace(c).ID, id, message, req.RequestID)
	if err != nil {
		return conversationError(c, err)
	}
	return response.Success(c, http.StatusAccepted, result)
}
func (h *ConversationHandler) Cancel(c echo.Context) error {
	id, err := conversationID(c)
	if err != nil {
		return conversationError(c, err)
	}
	if err = h.svc.Repo.Cancel(c.Request().Context(), middleware.GetWorkspace(c).ID, id); err != nil {
		return conversationError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

// Stream reuses the authenticated Echo/WebSocket stack, with conversation-level
// authorization rather than the workspace-wide broadcast hub. Persisted events
// are the source for both initial replay and live delivery on every API replica.
func (h *ConversationHandler) Stream(c echo.Context) error {
	id, err := conversationID(c)
	if err != nil {
		return conversationError(c, err)
	}
	cursor, err := conversationCursor(c, "after")
	if err != nil {
		return conversationError(c, err)
	}
	ws := middleware.GetWorkspace(c).ID
	if _, err = h.svc.Repo.Events(c.Request().Context(), ws, id, cursor); err != nil {
		return conversationError(c, err)
	}
	if h.origin == "" || !strings.EqualFold(c.Request().Header.Get("Origin"), h.origin) {
		return response.Forbidden(c)
	}
	conn, err := websocket.Accept(c.Response().Writer, c.Request(), &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return err
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	ctx := conn.CloseRead(c.Request().Context())
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()
	for {
		events, err := h.svc.Repo.Events(ctx, ws, id, cursor)
		if err != nil {
			conn.Close(websocket.StatusPolicyViolation, "Conversation access unavailable")
			return nil
		}
		for _, event := range events {
			writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			err = wsjson.Write(writeCtx, conn, event)
			cancel()
			if err != nil {
				return nil
			}
			cursor = event.Cursor
		}
		if len(events) == 200 {
			continue
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		case <-heartbeat.C:
			pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			err = conn.Ping(pingCtx)
			cancel()
			if err != nil {
				return nil
			}
		}
	}
}

func bindConversation(c echo.Context, req any) error {
	c.Request().Body = http.MaxBytesReader(c.Response(), c.Request().Body, 32<<10)
	return c.Bind(req)
}

// ResolveApproval accepts the session user's decision. The backend binds the
// approval to its actor and re-authorizes the effect; repeats are idempotent.
func (h *ConversationHandler) ResolveApproval(c echo.Context) error {
	id, err := conversationID(c)
	if err != nil {
		return conversationError(c, err)
	}
	approval, err := uuid.Parse(c.Param("approvalId"))
	if err != nil {
		return conversationError(c, repository.ErrConversationNotFound)
	}
	var input struct {
		Decision string `json:"decision"`
	}
	decoder := json.NewDecoder(io.LimitReader(c.Request().Body, 1024))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || (input.Decision != "approve" && input.Decision != "deny") {
		return conversationError(c, repository.ErrConversationInvalid)
	}
	result, err := h.svc.ResolveApproval(c.Request().Context(), middleware.GetWorkspace(c).ID, id, approval, input.Decision == "approve")
	if err != nil {
		return conversationError(c, err)
	}
	return response.Success(c, http.StatusOK, result)
}

func folderID(c echo.Context) (uuid.UUID, error) {
	id, err := uuid.Parse(c.Param("folderId"))
	if err != nil {
		return uuid.Nil, repository.ErrConversationNotFound
	}
	return id, nil
}

func (h *ConversationHandler) Folders(c echo.Context) error {
	folders, err := h.svc.Repo.Folders(c.Request().Context(), middleware.GetWorkspace(c).ID)
	if err != nil {
		return conversationError(c, err)
	}
	return response.Success(c, http.StatusOK, folders)
}

func (h *ConversationHandler) CreateFolder(c echo.Context) error {
	var req struct {
		Name string `json:"name"`
	}
	if err := bindConversation(c, &req); err != nil {
		return conversationError(c, repository.ErrConversationInvalid)
	}
	folder, err := h.svc.Repo.CreateFolder(c.Request().Context(), middleware.GetWorkspace(c).ID, req.Name)
	if err != nil {
		return conversationError(c, err)
	}
	return response.Success(c, http.StatusCreated, folder)
}

func (h *ConversationHandler) RenameFolder(c echo.Context) error {
	id, err := folderID(c)
	if err != nil {
		return conversationError(c, err)
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := bindConversation(c, &req); err != nil {
		return conversationError(c, repository.ErrConversationInvalid)
	}
	folder, err := h.svc.Repo.RenameFolder(c.Request().Context(), middleware.GetWorkspace(c).ID, id, req.Name)
	if err != nil {
		return conversationError(c, err)
	}
	return response.Success(c, http.StatusOK, folder)
}

func (h *ConversationHandler) DeleteFolder(c echo.Context) error {
	id, err := folderID(c)
	if err != nil {
		return conversationError(c, err)
	}
	if err := h.svc.Repo.DeleteFolder(c.Request().Context(), middleware.GetWorkspace(c).ID, id); err != nil {
		return conversationError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

// Update renames, files or tags one conversation per request.
func (h *ConversationHandler) Update(c echo.Context) error {
	id, err := conversationID(c)
	if err != nil {
		return conversationError(c, err)
	}
	// A request either renames ("title"), files the conversation ("folder_id",
	// null for the root) or tags it with a project ("project_id", null to clear).
	var req map[string]json.RawMessage
	if err := bindConversation(c, &req); err != nil {
		return conversationError(c, repository.ErrConversationInvalid)
	}
	ctx, ws := c.Request().Context(), middleware.GetWorkspace(c).ID
	var result *domain.Conversation
	if raw, ok := req["title"]; ok {
		var title string
		if err := json.Unmarshal(raw, &title); err != nil {
			return conversationError(c, repository.ErrConversationInvalid)
		}
		result, err = h.svc.Repo.RenameConversation(ctx, ws, id, title)
	} else if raw, ok := req["folder_id"]; ok {
		var folder *uuid.UUID
		if err := json.Unmarshal(raw, &folder); err != nil {
			return conversationError(c, repository.ErrConversationInvalid)
		}
		result, err = h.svc.Repo.MoveConversation(ctx, ws, id, folder)
	} else if raw, ok := req["project_id"]; ok {
		var project *uuid.UUID
		if err := json.Unmarshal(raw, &project); err != nil {
			return conversationError(c, repository.ErrConversationInvalid)
		}
		result, err = h.svc.Repo.SetConversationProject(ctx, ws, id, project)
	} else {
		err = repository.ErrConversationInvalid
	}
	if err != nil {
		return conversationError(c, err)
	}
	return response.Success(c, http.StatusOK, result)
}

func (h *ConversationHandler) Delete(c echo.Context) error {
	id, err := conversationID(c)
	if err != nil {
		return conversationError(c, err)
	}
	if err := h.svc.Repo.Delete(c.Request().Context(), middleware.GetWorkspace(c).ID, id); err != nil {
		return conversationError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}
