package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var (
	ErrVoiceUnavailable   = errors.New("the voice service is not available")
	ErrVoiceOff           = errors.New("voice transcription is off")
	ErrVoiceNotReady      = errors.New("the voice model is still downloading")
	ErrVoiceBusy          = errors.New("the voice service is busy")
	ErrVoiceTooLong       = errors.New("audio is longer than 5 minutes")
	ErrVoiceTooLarge      = errors.New("audio is too large")
	ErrVoiceUnsupported   = errors.New("unsupported audio")
	ErrVoiceInvalidChoice = errors.New("unknown voice model")
)

// Timeouts for calls to the internal voice service. Transcription covers
// waiting in its bounded queue and decoding five minutes of audio on CPU.
const (
	voiceStatusTimeout     = 5 * time.Second
	voiceTranscribeTimeout = 5 * time.Minute
)

// VoiceClient calls the internal voice service. Its URL comes from operator
// configuration (VOICE_URL), not from workspace owners, so it may be a
// private Compose address; redirects are never followed and responses are
// bounded.
type VoiceClient struct {
	base   string
	token  string
	client *http.Client
}

func NewVoiceClient(rawURL, token string) (*VoiceClient, error) {
	parsed, err := url.Parse(strings.TrimRight(rawURL, "/"))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" {
		return nil, fmt.Errorf("VOICE_URL must be an http(s) URL without credentials or query")
	}
	if len(token) < 32 {
		return nil, fmt.Errorf("VOICE_INTERNAL_TOKEN must be at least 32 characters")
	}
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	return &VoiceClient{
		base:  parsed.String(),
		token: token,
		client: &http.Client{
			Transport: &http.Transport{
				Proxy:                 nil,
				DialContext:           dialer.DialContext,
				TLSHandshakeTimeout:   5 * time.Second,
				ResponseHeaderTimeout: voiceTranscribeTimeout,
				MaxIdleConns:          4,
				IdleConnTimeout:       30 * time.Second,
			},
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}, nil
}

// VoiceServiceModel is one model's state in the voice service.
type VoiceServiceModel struct {
	ID              string `json:"id"`
	State           string `json:"state"`
	DownloadedBytes int64  `json:"downloaded_bytes"`
	TotalBytes      int64  `json:"total_bytes"`
	Error           string `json:"error"`
}

type VoiceServiceStatus struct {
	Loaded *string             `json:"loaded"`
	Models []VoiceServiceModel `json:"models"`
	Busy   bool                `json:"busy"`
	Queued int                 `json:"queued"`
}

func (s *VoiceServiceStatus) model(id string) *VoiceServiceModel {
	for i := range s.Models {
		if s.Models[i].ID == id {
			return &s.Models[i]
		}
	}
	return nil
}

type voiceError struct {
	Error struct {
		Code string `json:"code"`
	} `json:"error"`
}

func (c *VoiceClient) do(ctx context.Context, timeout time.Duration, method, path, contentType string, body []byte, out any) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, reader)
	if err != nil {
		return ErrVoiceUnavailable
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	res, err := c.client.Do(req)
	if err != nil {
		return ErrVoiceUnavailable
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return ErrVoiceUnavailable
	}
	if res.StatusCode >= 200 && res.StatusCode < 300 {
		if out != nil && json.Unmarshal(data, out) != nil {
			return ErrVoiceUnavailable
		}
		return nil
	}
	var failure voiceError
	_ = json.Unmarshal(data, &failure)
	switch failure.Error.Code {
	case "model_not_ready":
		return ErrVoiceNotReady
	case "busy":
		return ErrVoiceBusy
	case "too_long":
		return ErrVoiceTooLong
	case "too_large":
		return ErrVoiceTooLarge
	case "unsupported_audio":
		return ErrVoiceUnsupported
	case "unknown_model":
		return ErrVoiceInvalidChoice
	}
	return ErrVoiceUnavailable
}

func (c *VoiceClient) Status(ctx context.Context) (*VoiceServiceStatus, error) {
	var status VoiceServiceStatus
	if err := c.do(ctx, voiceStatusTimeout, http.MethodGet, "/v1/status", "", nil, &status); err != nil {
		return nil, err
	}
	return &status, nil
}

// Download asks the service to fetch (or retry) a model; it returns at once.
func (c *VoiceClient) Download(ctx context.Context, model string) (*VoiceServiceStatus, error) {
	var status VoiceServiceStatus
	if err := c.do(ctx, voiceStatusTimeout, http.MethodPost, "/v1/models/"+url.PathEscape(model), "", []byte{}, &status); err != nil {
		return nil, err
	}
	return &status, nil
}

// Transcribe sends audio for one transcription and returns its text.
func (c *VoiceClient) Transcribe(ctx context.Context, model, contentType string, audio []byte) (string, error) {
	var result struct {
		Text string `json:"text"`
	}
	if err := c.do(ctx, voiceTranscribeTimeout, http.MethodPost, "/v1/transcribe?model="+url.QueryEscape(model), contentType, audio, &result); err != nil {
		return "", err
	}
	return strings.TrimSpace(result.Text), nil
}
