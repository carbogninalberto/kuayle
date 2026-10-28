package service

import (
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
)

var ErrVoiceDenied = errors.New("voice settings access denied")

type voiceSettings interface {
	Model(ctx context.Context, ws uuid.UUID) (string, error)
	SetModel(ctx context.Context, ws, user uuid.UUID, model string) error
}

type voiceConversations interface {
	Authorize(ctx context.Context, ws, id uuid.UUID) error
}

// VoiceService turns speech into text for the assistant composer and
// Telegram voice notes. The workspace owner chooses one model; transcripts
// are returned to the caller and never stored here.
type VoiceService struct {
	settings      voiceSettings
	workspaces    repository.WorkspaceRepo
	conversations voiceConversations
	// client is nil when no voice service is configured.
	client *VoiceClient
}

func NewVoiceService(settings voiceSettings, workspaces repository.WorkspaceRepo, conversations voiceConversations, client *VoiceClient) *VoiceService {
	return &VoiceService{settings: settings, workspaces: workspaces, conversations: conversations, client: client}
}

// VoiceStatus is what the settings page and the composer microphone need.
// Download details and the model list are only given to the owner.
type VoiceStatus struct {
	Model string `json:"model"`
	Owner bool   `json:"owner"`
	// Configured: the server has a voice service (VOICE_URL).
	Configured bool `json:"configured"`
	// State: off, unavailable, absent, queued, downloading, ready or failed.
	State           string              `json:"state"`
	Ready           bool                `json:"ready"`
	DownloadedBytes int64               `json:"downloaded_bytes,omitempty"`
	TotalBytes      int64               `json:"total_bytes,omitempty"`
	Error           string              `json:"error,omitempty"`
	MaxSeconds      int                 `json:"max_seconds"`
	MaxBytes        int                 `json:"max_bytes"`
	Models          []domain.VoiceModel `json:"models,omitempty"`
}

// access allows current non-guest members and reports ownership.
func (s *VoiceService) access(ctx context.Context, ws uuid.UUID) (uuid.UUID, bool, error) {
	a, ok := domain.AccessFromContext(ctx)
	if !ok || a.WorkspaceID != ws {
		return uuid.Nil, false, ErrVoiceDenied
	}
	member, err := s.workspaces.GetMember(ctx, ws, a.UserID)
	if err != nil {
		return uuid.Nil, false, err
	}
	if member == nil || member.Role == "guest" {
		return uuid.Nil, false, ErrVoiceDenied
	}
	workspace, err := s.workspaces.GetByID(ctx, ws)
	if err != nil {
		return uuid.Nil, false, err
	}
	if workspace == nil {
		return uuid.Nil, false, ErrVoiceDenied
	}
	return a.UserID, workspace.OwnerID == a.UserID, nil
}

func (s *VoiceService) Status(ctx context.Context, ws uuid.UUID) (*VoiceStatus, error) {
	_, owner, err := s.access(ctx, ws)
	if err != nil {
		return nil, err
	}
	model, err := s.settings.Model(ctx, ws)
	if err != nil {
		return nil, err
	}
	status := &VoiceStatus{Model: model, Owner: owner, Configured: s.client != nil, MaxSeconds: domain.VoiceMaxSeconds, MaxBytes: domain.VoiceMaxBytes}
	if owner {
		status.Models = domain.VoiceModels
	}
	switch {
	case model == domain.VoiceModelOff:
		status.State = "off"
	case s.client == nil:
		status.State = "unavailable"
	default:
		service, err := s.client.Status(ctx)
		var m *VoiceServiceModel
		if err == nil {
			m = service.model(model)
		}
		if m == nil {
			status.State = "unavailable"
			break
		}
		// A model missing from the volume (for example after it was wiped)
		// is fetched again for the workspace that still uses it.
		if m.State == "absent" {
			if requested, e := s.client.Download(ctx, model); e == nil && requested.model(model) != nil {
				m = requested.model(model)
			}
		}
		status.State = m.State
		status.Ready = m.State == "ready"
		if owner {
			status.DownloadedBytes, status.TotalBytes, status.Error = m.DownloadedBytes, m.TotalBytes, m.Error
		}
	}
	return status, nil
}

// Select stores the owner's model and starts its download.
func (s *VoiceService) Select(ctx context.Context, ws uuid.UUID, model string) (*VoiceStatus, error) {
	user, owner, err := s.access(ctx, ws)
	if err != nil {
		return nil, err
	}
	if !owner {
		return nil, ErrNotWorkspaceOwner
	}
	model = strings.TrimSpace(model)
	if !domain.ValidVoiceModel(model) {
		return nil, ErrVoiceInvalidChoice
	}
	if model != domain.VoiceModelOff && s.client == nil {
		return nil, ErrVoiceUnavailable
	}
	if err = s.settings.SetModel(ctx, ws, user, model); err != nil {
		return nil, err
	}
	if model != domain.VoiceModelOff {
		// Best effort: the status reports an unreachable service.
		_, _ = s.client.Download(ctx, model)
	}
	return s.Status(ctx, ws)
}

// Model returns the workspace's model when voice can be used, or "" when it
// is off or no voice service is configured.
func (s *VoiceService) Model(ctx context.Context, ws uuid.UUID) (string, error) {
	if s == nil || s.client == nil {
		return "", nil
	}
	model, err := s.settings.Model(ctx, ws)
	if err != nil || model == domain.VoiceModelOff {
		return "", err
	}
	return model, nil
}

// voiceContentType accepts what recorders and messengers send. The voice
// service still identifies the container from the content.
func voiceContentType(raw string) (string, bool) {
	media, _, err := mime.ParseMediaType(raw)
	if err != nil {
		return "", false
	}
	switch {
	case strings.HasPrefix(media, "audio/"), media == "video/webm", media == "video/mp4", media == "application/octet-stream", media == "application/ogg":
		return media, true
	}
	return "", false
}

// Transcribe authorizes the conversation before reading any audio, then
// returns the transcript for the composer. Nothing is stored.
func (s *VoiceService) Transcribe(ctx context.Context, ws, conversation uuid.UUID, contentType string, body io.Reader) (string, error) {
	if err := s.conversations.Authorize(ctx, ws, conversation); err != nil {
		return "", err
	}
	model, err := s.Model(ctx, ws)
	if err != nil {
		return "", err
	}
	if model == "" {
		if s.client == nil {
			return "", ErrVoiceUnavailable
		}
		return "", ErrVoiceOff
	}
	media, ok := voiceContentType(contentType)
	if !ok {
		return "", ErrVoiceUnsupported
	}
	audio, err := readVoice(body)
	if err != nil {
		return "", err
	}
	return s.client.Transcribe(ctx, model, media, audio)
}

// TranscribeAudio transcribes already authorized audio (Telegram).
func (s *VoiceService) TranscribeAudio(ctx context.Context, model, contentType string, audio []byte) (string, error) {
	if s == nil || s.client == nil {
		return "", ErrVoiceUnavailable
	}
	if len(audio) == 0 {
		return "", ErrVoiceUnsupported
	}
	if len(audio) > domain.VoiceMaxBytes {
		return "", ErrVoiceTooLarge
	}
	media, ok := voiceContentType(contentType)
	if !ok {
		media = "application/octet-stream"
	}
	return s.client.Transcribe(ctx, model, media, audio)
}

// readVoice reads at most the size limit plus one byte.
func readVoice(r io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, domain.VoiceMaxBytes+1))
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) || len(data) > domain.VoiceMaxBytes {
		return nil, ErrVoiceTooLarge
	}
	if err != nil {
		return nil, ErrVoiceUnsupported
	}
	if len(data) == 0 {
		return nil, ErrVoiceUnsupported
	}
	return data, nil
}
