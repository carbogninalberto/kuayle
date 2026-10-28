package service

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/pkg/crypto"
	log "github.com/sirupsen/logrus"
)

// TelegramVoicePrefix marks a user message transcribed from a voice note.
const TelegramVoicePrefix = "🎤 "

const (
	telegramVoiceOff         = "Voice notes are off in this workspace. Send your message as text, or ask the workspace owner to choose a voice model in Settings → Voice."
	telegramVoiceTooLong     = "Voice notes can be at most 5 minutes. Send a shorter one or type your message."
	telegramVoiceTooLarge    = "This voice note is too large to transcribe. Send a shorter one or type your message."
	telegramVoiceUnreadable  = "This voice note could not be read. Try recording it again or type your message."
	telegramVoiceNotReady    = "The voice model is still downloading. Try again in a few minutes."
	telegramVoiceBusy        = "Transcription is busy right now. Try again in a minute."
	telegramVoiceFailed      = "This voice note could not be transcribed right now. Try again later or type your message."
	telegramVoiceEmpty       = "No speech was recognized in this voice note."
	telegramVoiceUnavailable = "This voice note could not be received."
	// Downloading, waiting in the voice queue and transcribing on CPU.
	telegramVoiceBudget = 8 * time.Minute
	// Voice notes being transcribed at once for all workspaces; more get a
	// "busy" reply instead of piling up behind the voice service's queue.
	telegramVoiceJobLimit = 4
)

type telegramAudio struct {
	FileID   string `json:"file_id"`
	Duration int    `json:"duration"`
	MimeType string `json:"mime_type"`
	FileSize int64  `json:"file_size"`
}

// telegramVoiceJobs tracks voice notes being transcribed, so a redelivered
// update is not transcribed twice and the number in flight stays bounded.
type telegramVoiceJobs struct {
	mu      sync.Mutex
	running map[string]bool
	wg      sync.WaitGroup
}

// start reports "started", "duplicate" or "full".
func (j *telegramVoiceJobs) start(key string) string {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.running == nil {
		j.running = map[string]bool{}
	}
	if j.running[key] {
		return "duplicate"
	}
	if len(j.running) >= telegramVoiceJobLimit {
		return "full"
	}
	j.running[key] = true
	j.wg.Add(1)
	return "started"
}

func (j *telegramVoiceJobs) done(key string) {
	j.mu.Lock()
	delete(j.running, key)
	j.mu.Unlock()
	j.wg.Done()
}

// SetVoice lets voice notes and audio messages be transcribed.
func (s *TelegramService) SetVoice(voice *VoiceService) {
	s.voice = voice
}

// WaitVoice waits for voice notes being transcribed (tests and shutdown).
func (s *TelegramService) WaitVoice() {
	s.voiceJobs.wg.Wait()
}

// receiveVoice answers voice notes that cannot be transcribed straight away
// and otherwise transcribes them in the background, after the same sender,
// link and duplicate checks as photos. Telegram gets its answer at once; the
// transcript is then recorded as the user's message, marked with a mic.
func (s *TelegramService) receiveVoice(ctx context.Context, ws uuid.UUID, secret string, u domain.TelegramUpdate, note telegramAudio, caption string) error {
	u.Text = ""
	model, err := s.voice.Model(ctx, ws)
	if err != nil {
		return err
	}
	switch {
	case model == "":
		u.Notice = telegramVoiceOff
	case note.Duration > domain.VoiceMaxSeconds:
		u.Notice = telegramVoiceTooLong
	case note.FileSize > domain.VoiceMaxBytes:
		u.Notice = telegramVoiceTooLarge
	case note.FileID == "" || len(note.FileID) > 256:
		u.Notice = telegramVoiceUnavailable
	}
	if u.Notice != "" {
		return s.record(ctx, ws, secret, u)
	}
	encrypted, err := s.Repo.ImageSender(ctx, ws, secret, u)
	if err != nil {
		return err
	}
	if encrypted == "" {
		// Not an allowed, linked sender or already received: record refuses
		// or ignores it exactly as it would a text message.
		u.Notice = telegramVoiceUnavailable
		return s.record(ctx, ws, secret, u)
	}
	key := ws.String() + ":" + strconv.FormatInt(u.ID, 10)
	switch s.voiceJobs.start(key) {
	case "duplicate":
		return nil
	case "full":
		u.Notice = telegramVoiceBusy
		return s.record(ctx, ws, secret, u)
	}
	background := context.WithoutCancel(ctx)
	go func() {
		defer s.voiceJobs.done(key)
		jobCtx, cancel := context.WithTimeout(background, telegramVoiceBudget)
		defer cancel()
		text, notice := s.transcribeVoice(jobCtx, encrypted, model, u.ChatID, note)
		if notice != "" {
			u.Notice = notice
		} else {
			u.Text = telegramTranscript(text, caption)
		}
		recordCtx, stop := context.WithTimeout(background, 10*time.Second)
		defer stop()
		if err := s.record(recordCtx, ws, secret, u); err != nil {
			log.WithField("component", "telegram").Warn("A transcribed voice note could not be recorded")
		}
	}()
	return nil
}

// transcribeVoice downloads one voice note from Telegram's file endpoint
// (no redirects, no private addresses, bounded size) and transcribes it.
// It returns the transcript or a notice for the sender.
func (s *TelegramService) transcribeVoice(ctx context.Context, encrypted, model string, chat int64, note telegramAudio) (string, string) {
	token, err := crypto.Decrypt(encrypted, s.key)
	if err != nil {
		return "", telegramVoiceUnavailable
	}
	_ = s.call(ctx, token, "sendChatAction", map[string]any{"chat_id": chat, "action": "typing"}, nil)
	var info struct {
		FilePath string `json:"file_path"`
		FileSize int64  `json:"file_size"`
	}
	if err = s.call(ctx, token, "getFile", map[string]string{"file_id": note.FileID}, &info); err != nil {
		return "", telegramVoiceUnavailable
	}
	if info.FileSize > domain.VoiceMaxBytes {
		return "", telegramVoiceTooLarge
	}
	if len(info.FilePath) > 256 || !telegramFilePath.MatchString(info.FilePath) || strings.Contains(info.FilePath, "..") {
		return "", telegramVoiceUnavailable
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.endpoint+"/file/bot"+token+"/"+info.FilePath, nil)
	if err != nil {
		return "", telegramVoiceUnavailable
	}
	client := *s.client
	client.Timeout = 60 * time.Second
	res, err := client.Do(req)
	if err != nil {
		return "", telegramVoiceUnavailable
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", telegramVoiceUnavailable
	}
	audio, err := readVoice(res.Body)
	if errors.Is(err, ErrVoiceTooLarge) {
		return "", telegramVoiceTooLarge
	}
	if err != nil {
		return "", telegramVoiceUnreadable
	}
	text, err := s.voice.TranscribeAudio(ctx, model, note.MimeType, audio)
	switch {
	case errors.Is(err, ErrVoiceTooLong):
		return "", telegramVoiceTooLong
	case errors.Is(err, ErrVoiceTooLarge):
		return "", telegramVoiceTooLarge
	case errors.Is(err, ErrVoiceUnsupported):
		return "", telegramVoiceUnreadable
	case errors.Is(err, ErrVoiceNotReady):
		return "", telegramVoiceNotReady
	case errors.Is(err, ErrVoiceBusy):
		return "", telegramVoiceBusy
	case err != nil:
		return "", telegramVoiceFailed
	case strings.TrimSpace(text) == "":
		return "", telegramVoiceEmpty
	}
	return text, ""
}

// telegramTranscript is the user's message: the mic marker, the transcript
// and any caption, within Telegram's message limit.
func telegramTranscript(text, caption string) string {
	message := TelegramVoicePrefix + strings.TrimSpace(text)
	if caption = strings.TrimSpace(caption); caption != "" {
		message += "\n\n" + caption
	}
	const limit = 16000
	for len(message) > limit || !utf8.ValidString(message) {
		_, size := utf8.DecodeLastRuneInString(message)
		message = message[:len(message)-max(size, 1)]
	}
	return message
}
