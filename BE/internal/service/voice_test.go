package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

const voiceTestToken = "voice-test-token-0123456789-abcdefghij"

// fakeVoiceServer speaks the voice service's internal protocol.
type fakeVoiceServer struct {
	mu          sync.Mutex
	states      map[string]string
	downloads   []string
	transcribed []string
	failure     string
	text        string
	unauthed    int
}

func newFakeVoiceServer(t *testing.T) (*fakeVoiceServer, *VoiceClient) {
	t.Helper()
	fake := &fakeVoiceServer{states: map[string]string{}, text: "hello world"}
	server := httptest.NewServer(http.HandlerFunc(fake.serve))
	t.Cleanup(server.Close)
	client, err := NewVoiceClient(server.URL, voiceTestToken)
	require.NoError(t, err)
	return fake, client
}

func (f *fakeVoiceServer) status() map[string]any {
	models := []map[string]any{}
	for _, m := range domain.VoiceModels {
		state := f.states[m.ID]
		if state == "" {
			state = "absent"
		}
		models = append(models, map[string]any{"id": m.ID, "state": state, "downloaded_bytes": 10, "total_bytes": 100, "error": ""})
	}
	return map[string]any{"models": models}
}

func (f *fakeVoiceServer) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer "+voiceTestToken {
		f.unauthed++
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":{"code":"unauthorized"}}`)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/v1/status":
		_ = json.NewEncoder(w).Encode(f.status())
	case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/v1/models/"):
		id := strings.TrimPrefix(r.URL.Path, "/v1/models/")
		f.downloads = append(f.downloads, id)
		if f.states[id] == "" || f.states[id] == "absent" {
			f.states[id] = "downloading"
		}
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(f.status())
	case r.Method == http.MethodPost && r.URL.Path == "/v1/transcribe":
		body, _ := io.ReadAll(r.Body)
		f.transcribed = append(f.transcribed, r.URL.Query().Get("model")+" "+r.Header.Get("Content-Type")+" "+string(body))
		if f.failure != "" {
			status := map[string]int{"too_long": 422, "busy": 503, "model_not_ready": 409, "unsupported_audio": 415, "too_large": 413}[f.failure]
			w.WriteHeader(status)
			_, _ = io.WriteString(w, `{"error":{"code":"`+f.failure+`"}}`)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"text": " " + f.text + " ", "duration_seconds": 1.5})
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

type voiceTestWorkspaces struct {
	repository.WorkspaceRepo
	owner uuid.UUID
	roles map[uuid.UUID]string
}

func (w voiceTestWorkspaces) GetMember(_ context.Context, ws, user uuid.UUID) (*domain.WorkspaceMember, error) {
	role, ok := w.roles[user]
	if !ok {
		return nil, nil
	}
	return &domain.WorkspaceMember{WorkspaceID: ws, UserID: user, Role: role}, nil
}

func (w voiceTestWorkspaces) GetByID(_ context.Context, id uuid.UUID) (*domain.Workspace, error) {
	return &domain.Workspace{ID: id, OwnerID: w.owner}, nil
}

type voiceTestSettings struct {
	models map[uuid.UUID]string
}

func (s *voiceTestSettings) Model(_ context.Context, ws uuid.UUID) (string, error) {
	if m, ok := s.models[ws]; ok {
		return m, nil
	}
	return domain.VoiceModelOff, nil
}

func (s *voiceTestSettings) SetModel(_ context.Context, ws, _ uuid.UUID, model string) error {
	s.models[ws] = model
	return nil
}

type voiceTestConversations struct{ readers map[uuid.UUID]bool }

func (c voiceTestConversations) Authorize(ctx context.Context, ws, id uuid.UUID) error {
	a, ok := domain.AccessFromContext(ctx)
	if !ok || a.WorkspaceID != ws || !c.readers[a.UserID] {
		return repository.ErrConversationNotFound
	}
	return nil
}

// spyReader fails the test if the body is read.
type spyReader struct{ read bool }

func (r *spyReader) Read(p []byte) (int, error) {
	r.read = true
	return 0, io.EOF
}

type voiceFixture struct {
	ws, owner, member, guest, outsider uuid.UUID
	settings                           *voiceTestSettings
	fake                               *fakeVoiceServer
	svc                                *VoiceService
}

func newVoiceFixture(t *testing.T, configured bool) *voiceFixture {
	f := &voiceFixture{ws: uuid.New(), owner: uuid.New(), member: uuid.New(), guest: uuid.New(), outsider: uuid.New(), settings: &voiceTestSettings{models: map[uuid.UUID]string{}}}
	workspaces := voiceTestWorkspaces{owner: f.owner, roles: map[uuid.UUID]string{f.owner: "owner", f.member: "member", f.guest: "guest"}}
	var client *VoiceClient
	if configured {
		f.fake, client = newFakeVoiceServer(t)
	}
	f.svc = NewVoiceService(f.settings, workspaces, voiceTestConversations{readers: map[uuid.UUID]bool{f.owner: true}}, client)
	return f
}

func (f *voiceFixture) as(user uuid.UUID) context.Context {
	return domain.WithAccess(context.Background(), domain.Access{WorkspaceID: f.ws, UserID: user})
}

func TestNewVoiceClientRejectsUnsafeConfiguration(t *testing.T) {
	for _, raw := range []string{"", "voice:8000", "ftp://voice", "http://user:pass@voice:8000", "http://voice:8000?x=1"} {
		_, err := NewVoiceClient(raw, voiceTestToken)
		require.Error(t, err, raw)
	}
	_, err := NewVoiceClient("http://voice:8000", "short")
	require.Error(t, err, "the internal token must be long")
	_, err = NewVoiceClient("http://voice:8000/", voiceTestToken)
	require.NoError(t, err)
}

func TestVoiceClientSendsTokenAndMapsErrors(t *testing.T) {
	fake, client := newFakeVoiceServer(t)
	ctx := context.Background()
	text, err := client.Transcribe(ctx, "vosk-small-it-0.22", "audio/ogg", []byte("OggS"))
	require.NoError(t, err)
	require.Equal(t, "hello world", text, "the transcript is trimmed")
	require.Equal(t, []string{"vosk-small-it-0.22 audio/ogg OggS"}, fake.transcribed)
	for code, want := range map[string]error{"too_long": ErrVoiceTooLong, "busy": ErrVoiceBusy, "model_not_ready": ErrVoiceNotReady, "unsupported_audio": ErrVoiceUnsupported, "too_large": ErrVoiceTooLarge} {
		fake.failure = code
		_, err = client.Transcribe(ctx, "vosk-small-it-0.22", "audio/ogg", []byte("OggS"))
		require.ErrorIs(t, err, want, code)
	}

	wrong, err := NewVoiceClient(strings.TrimSuffix(client.base, "/"), voiceTestToken+"-wrong")
	require.NoError(t, err)
	_, err = wrong.Status(ctx)
	require.ErrorIs(t, err, ErrVoiceUnavailable)
	require.Equal(t, 1, fake.unauthed)

	// Redirects are never followed.
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, client.base+"/v1/status", http.StatusFound)
	}))
	defer redirect.Close()
	redirected, err := NewVoiceClient(redirect.URL, voiceTestToken)
	require.NoError(t, err)
	_, err = redirected.Status(ctx)
	require.ErrorIs(t, err, ErrVoiceUnavailable)

	// An unreachable service is unavailable, not an internal error.
	redirect.Close()
	_, err = redirected.Status(ctx)
	require.ErrorIs(t, err, ErrVoiceUnavailable)
}

func TestVoiceSettingsAreOwnerOnlyAndStatusIsForMembers(t *testing.T) {
	f := newVoiceFixture(t, true)
	status, err := f.svc.Status(f.as(f.owner), f.ws)
	require.NoError(t, err)
	require.Equal(t, "off", status.State)
	require.True(t, status.Owner)
	require.Len(t, status.Models, len(domain.VoiceModels))
	require.Equal(t, 300, status.MaxSeconds)

	_, err = f.svc.Select(f.as(f.member), f.ws, "faster-whisper-base")
	require.ErrorIs(t, err, ErrNotWorkspaceOwner)
	for _, user := range []uuid.UUID{f.guest, f.outsider} {
		_, err = f.svc.Status(f.as(user), f.ws)
		require.ErrorIs(t, err, ErrVoiceDenied)
		_, err = f.svc.Select(f.as(user), f.ws, "faster-whisper-base")
		require.ErrorIs(t, err, ErrVoiceDenied)
	}
	_, err = f.svc.Status(domain.WithAccess(context.Background(), domain.Access{WorkspaceID: uuid.New(), UserID: f.owner}), f.ws)
	require.ErrorIs(t, err, ErrVoiceDenied, "access for another workspace is not access here")
	_, err = f.svc.Select(f.as(f.owner), f.ws, "whisper-huge")
	require.ErrorIs(t, err, ErrVoiceInvalidChoice)
	require.Empty(t, f.fake.downloads)

	// Choosing a model stores it and starts its download.
	status, err = f.svc.Select(f.as(f.owner), f.ws, "faster-whisper-base")
	require.NoError(t, err)
	require.Equal(t, "faster-whisper-base", f.settings.models[f.ws])
	require.Equal(t, []string{"faster-whisper-base"}, f.fake.downloads)
	require.Equal(t, "downloading", status.State)
	require.False(t, status.Ready)
	require.EqualValues(t, 100, status.TotalBytes, "the owner sees the progress")

	// Members learn whether the microphone works, not the download details.
	f.fake.states["faster-whisper-base"] = "ready"
	status, err = f.svc.Status(f.as(f.member), f.ws)
	require.NoError(t, err)
	require.True(t, status.Ready)
	require.False(t, status.Owner)
	require.Empty(t, status.Models)
	require.Zero(t, status.TotalBytes)

	// A model that disappeared from the volume is downloaded again.
	f.fake.states["faster-whisper-base"] = "absent"
	status, err = f.svc.Status(f.as(f.owner), f.ws)
	require.NoError(t, err)
	require.Equal(t, "downloading", status.State)
	require.Len(t, f.fake.downloads, 2)

	status, err = f.svc.Select(f.as(f.owner), f.ws, "off")
	require.NoError(t, err)
	require.Equal(t, "off", status.State)
	require.Len(t, f.fake.downloads, 2, "turning voice off downloads nothing")
}

func TestVoiceWithoutAServiceCanOnlyBeOff(t *testing.T) {
	f := newVoiceFixture(t, false)
	status, err := f.svc.Status(f.as(f.owner), f.ws)
	require.NoError(t, err)
	require.False(t, status.Configured)
	_, err = f.svc.Select(f.as(f.owner), f.ws, "vosk-small-en-us-0.15")
	require.ErrorIs(t, err, ErrVoiceUnavailable)
	_, err = f.svc.Select(f.as(f.owner), f.ws, "off")
	require.NoError(t, err)
	_, err = f.svc.Transcribe(f.as(f.owner), f.ws, uuid.New(), "audio/webm", strings.NewReader("x"))
	require.ErrorIs(t, err, ErrVoiceUnavailable)
}

func TestVoiceTranscribeAuthorizesBeforeReadingAudio(t *testing.T) {
	f := newVoiceFixture(t, true)
	conversation := uuid.New()
	f.settings.models[f.ws] = "vosk-small-en-us-0.15"
	f.fake.states["vosk-small-en-us-0.15"] = "ready"
	for _, user := range []uuid.UUID{f.member, f.guest, f.outsider} {
		body := &spyReader{}
		_, err := f.svc.Transcribe(f.as(user), f.ws, conversation, "audio/webm", body)
		require.ErrorIs(t, err, repository.ErrConversationNotFound)
		require.False(t, body.read, "audio is not read before the conversation is authorized")
	}
	require.Empty(t, f.fake.transcribed)

	text, err := f.svc.Transcribe(f.as(f.owner), f.ws, conversation, "audio/webm;codecs=opus", strings.NewReader("\x1a\x45\xdf\xa3audio"))
	require.NoError(t, err)
	require.Equal(t, "hello world", text)
	require.Equal(t, []string{"vosk-small-en-us-0.15 audio/webm \x1a\x45\xdf\xa3audio"}, f.fake.transcribed)

	for _, contentType := range []string{"", "text/html", "image/png", "multipart/form-data; boundary=x"} {
		_, err = f.svc.Transcribe(f.as(f.owner), f.ws, conversation, contentType, strings.NewReader("x"))
		require.ErrorIs(t, err, ErrVoiceUnsupported, contentType)
	}
	_, err = f.svc.Transcribe(f.as(f.owner), f.ws, conversation, "audio/mp4", strings.NewReader(""))
	require.ErrorIs(t, err, ErrVoiceUnsupported, "an empty recording")
	_, err = f.svc.Transcribe(f.as(f.owner), f.ws, conversation, "audio/mp4", io.LimitReader(zeros{}, domain.VoiceMaxBytes+1))
	require.ErrorIs(t, err, ErrVoiceTooLarge)
	require.Len(t, f.fake.transcribed, 1, "refused audio never reaches the voice service")

	f.fake.failure = "too_long"
	_, err = f.svc.Transcribe(f.as(f.owner), f.ws, conversation, "audio/ogg", strings.NewReader("OggS"))
	require.ErrorIs(t, err, ErrVoiceTooLong)

	f.settings.models[f.ws] = "off"
	_, err = f.svc.Transcribe(f.as(f.owner), f.ws, conversation, "audio/ogg", strings.NewReader("OggS"))
	require.ErrorIs(t, err, ErrVoiceOff)
	require.Len(t, f.fake.transcribed, 2)
}

func TestTelegramTranscriptIsMarkedAndBounded(t *testing.T) {
	require.Equal(t, "🎤 hello", telegramTranscript(" hello ", ""))
	require.Equal(t, "🎤 hello\n\nsee attached", telegramTranscript("hello", " see attached "))
	long := telegramTranscript(strings.Repeat("è", 20000), "")
	require.LessOrEqual(t, len(long), 16000)
	require.True(t, strings.HasPrefix(long, TelegramVoicePrefix))
}

type zeros struct{}

func (zeros) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}
