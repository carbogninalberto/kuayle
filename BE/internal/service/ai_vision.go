package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/pkg/crypto"
)

// VisionCheck reports whether the configured model answered an image request
// and whether its answer names the probe image's colour.
type VisionCheck struct {
	Answered bool   `json:"answered"`
	SawImage bool   `json:"saw_image"`
	Answer   string `json:"answer,omitempty"`
	Error    string `json:"error,omitempty"`
}

// visionProbeImage is a solid red 16x16 PNG; it carries no workspace data.
var visionProbeImage = func() string {
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for x := range 16 {
		for y := range 16 {
			img.Set(x, y, color.RGBA{R: 230, A: 255})
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}()

// CheckVision probes the saved assistant model with the probe image. Only
// the workspace owner, who configures AI settings, may run it.
func (s *AISettingsService) CheckVision(ctx context.Context, workspaceID, userID uuid.UUID) (*VisionCheck, error) {
	ws, err := s.workspaceRepo.GetByID(ctx, workspaceID)
	if err != nil || ws == nil {
		return nil, ErrWorkspaceNotFound
	}
	if ws.OwnerID != userID {
		return nil, ErrNotWorkspaceOwner
	}
	settings, err := s.Get(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(settings.BaseURL) == "" || strings.TrimSpace(settings.Model) == "" || settings.APIKeyEncrypted == nil || *settings.APIKeyEncrypted == "" {
		return nil, ErrAISettingsNotConfigured
	}
	key, err := crypto.Decrypt(*settings.APIKeyEncrypted, s.encryptionKey)
	if err != nil {
		return nil, ErrAISettingsNotConfigured
	}
	result := s.probeVision(ctx, settings.BaseURL, settings.Model, key)
	return &result, nil
}

func (s *AISettingsService) probeVision(ctx context.Context, baseURL, model, key string) VisionCheck {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	prompt := "What colour is this image? Answer with one English word."
	body, err := json.Marshal(map[string]any{
		"model": model,
		"messages": []chatCompletionMessage{{Role: "user", Content: prompt, Parts: []chatContentPart{
			{Type: "text", Text: prompt},
			{Type: "image_url", ImageURL: &chatImageURL{URL: visionProbeImage}},
		}}},
		"temperature": 0,
		"max_tokens":  50,
	})
	if err != nil {
		return VisionCheck{Error: "The check could not be prepared."}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return VisionCheck{Error: "The provider address is invalid."}
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.httpClient.Do(req)
	if err != nil {
		// Transport errors can name internal hosts; never return them.
		return VisionCheck{Error: "The provider could not be reached."}
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var parsed chatCompletionResponse
	_ = json.Unmarshal(raw, &parsed)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || len(parsed.Choices) == 0 {
		message := fmt.Sprintf("The provider refused the image request (status %d).", resp.StatusCode)
		if parsed.Error != nil && parsed.Error.Message != "" {
			message += " " + boundedProviderText(parsed.Error.Message, key)
		}
		return VisionCheck{Error: message}
	}
	answer := boundedProviderText(parsed.Choices[0].Message.Content, key)
	return VisionCheck{Answered: true, SawImage: strings.Contains(strings.ToLower(answer), "red"), Answer: answer}
}

// boundedProviderText is printable, at most 200 bytes and never contains the key.
func boundedProviderText(text, key string) string {
	if key != "" {
		text = strings.ReplaceAll(text, key, "[redacted]")
	}
	text = strings.Join(strings.Fields(text), " ")
	for len(text) > 200 || !utf8.ValidString(text) {
		_, size := utf8.DecodeLastRuneInString(text)
		text = text[:len(text)-max(size, 1)]
	}
	return text
}
