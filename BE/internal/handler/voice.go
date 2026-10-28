package handler

import (
	"errors"
	"net/http"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/kuayle/kuayle-backend/pkg/response"
	"github.com/labstack/echo/v4"
)

type VoiceHandler struct{ svc *service.VoiceService }

func NewVoiceHandler(svc *service.VoiceService) *VoiceHandler { return &VoiceHandler{svc} }

func voiceError(c echo.Context, err error) error {
	switch {
	case errors.Is(err, service.ErrVoiceDenied), errors.Is(err, service.ErrNotWorkspaceOwner):
		return response.Forbidden(c)
	case errors.Is(err, repository.ErrConversationNotFound):
		return response.NotFound(c, "Conversation")
	case errors.Is(err, service.ErrVoiceInvalidChoice):
		return response.Error(c, http.StatusBadRequest, "VOICE_INVALID_MODEL", "Unknown voice model")
	case errors.Is(err, service.ErrVoiceOff):
		return response.Error(c, http.StatusConflict, "VOICE_OFF", "Voice transcription is off in this workspace")
	case errors.Is(err, service.ErrVoiceNotReady):
		return response.Error(c, http.StatusConflict, "VOICE_NOT_READY", "The voice model is still downloading")
	case errors.Is(err, service.ErrVoiceTooLong):
		return response.Error(c, http.StatusUnprocessableEntity, "VOICE_TOO_LONG", "Recordings can be at most 5 minutes")
	case errors.Is(err, service.ErrVoiceTooLarge):
		return response.Error(c, http.StatusRequestEntityTooLarge, "VOICE_TOO_LARGE", "The recording is too large")
	case errors.Is(err, service.ErrVoiceUnsupported):
		return response.Error(c, http.StatusUnsupportedMediaType, "VOICE_UNSUPPORTED", "Unsupported audio format")
	case errors.Is(err, service.ErrVoiceBusy):
		c.Response().Header().Set("Retry-After", "10")
		return response.Error(c, http.StatusServiceUnavailable, "VOICE_BUSY", "Transcription is busy; try again shortly")
	case errors.Is(err, service.ErrVoiceUnavailable):
		return response.Error(c, http.StatusServiceUnavailable, "VOICE_UNAVAILABLE", "The voice service is not available")
	default:
		return response.InternalError(c)
	}
}

func (h *VoiceHandler) Status(c echo.Context) error {
	status, err := h.svc.Status(c.Request().Context(), middleware.GetWorkspace(c).ID)
	if err != nil {
		return voiceError(c, err)
	}
	return response.Success(c, http.StatusOK, status)
}

func (h *VoiceHandler) Select(c echo.Context) error {
	var req struct {
		Model string `json:"model"`
	}
	if err := bindConversation(c, &req); err != nil {
		return voiceError(c, service.ErrVoiceInvalidChoice)
	}
	status, err := h.svc.Select(c.Request().Context(), middleware.GetWorkspace(c).ID, req.Model)
	if err != nil {
		return voiceError(c, err)
	}
	return response.Success(c, http.StatusOK, status)
}

// Transcribe takes one raw audio recording (the request body, with its
// Content-Type) and returns its text for the composer. The conversation is
// authorized before the body is read; nothing is stored.
func (h *VoiceHandler) Transcribe(c echo.Context) error {
	c.Response().Header().Set("Cache-Control", "no-store")
	id, err := conversationID(c)
	if err != nil {
		return voiceError(c, err)
	}
	c.Request().Body = http.MaxBytesReader(c.Response(), c.Request().Body, domain.VoiceMaxBytes+1)
	text, err := h.svc.Transcribe(c.Request().Context(), middleware.GetWorkspace(c).ID, id, c.Request().Header.Get("Content-Type"), c.Request().Body)
	if err != nil {
		return voiceError(c, err)
	}
	return response.Success(c, http.StatusOK, map[string]string{"text": text})
}
