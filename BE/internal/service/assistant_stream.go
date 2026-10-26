package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

const assistantMaxReplyBytes = 64 << 10

// streamAssistant uses the existing SSRF-protected transport and never returns
// provider diagnostics (which may contain credentials). Only public reply text,
// not hidden reasoning, enters the conversation stream.
func (s *AISettingsService) streamAssistant(ctx context.Context, baseURL, model, apiKey string, messages []chatCompletionMessage, emit func(string) error) error {
	body, err := json.Marshal(struct {
		Model    string                  `json:"model"`
		Messages []chatCompletionMessage `json:"messages"`
		Stream   bool                    `json:"stream"`
	}{model, messages, true})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return ErrAIProviderRequestFailed
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	client := *s.httpClient
	client.Timeout = 90 * time.Second
	resp, err := client.Do(req)
	if err != nil {
		return ErrAIProviderRequestFailed
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream") {
		return ErrAIProviderRequestFailed
	}
	scanner := bufio.NewScanner(io.LimitReader(resp.Body, 2<<20))
	scanner.Buffer(make([]byte, 4096), 256<<10)
	pending, total := "", 0
	flush := func(final bool) error {
		if apiKey != "" {
			pending = strings.ReplaceAll(pending, apiKey, "[REDACTED]")
		}
		keep := 0
		if !final {
			// Withhold only a suffix that might become a credential on the next chunk.
			for n := 1; n < len(apiKey) && n <= len(pending); n++ {
				if strings.HasSuffix(pending, apiKey[:n]) {
					keep = n
				}
			}
		}
		text := pending[:len(pending)-keep]
		pending = pending[len(pending)-keep:]
		if text != "" {
			return emit(text)
		}
		return nil
	}
	var data []string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data:") {
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
			continue
		}
		if line != "" || len(data) == 0 {
			continue
		}
		event := strings.Join(data, "\n")
		data = nil
		if event == "[DONE]" {
			return flush(true)
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
				FinishReason *string `json:"finish_reason"`
			} `json:"choices"`
			Error json.RawMessage `json:"error"`
		}
		if json.Unmarshal([]byte(event), &chunk) != nil || (len(chunk.Error) > 0 && string(chunk.Error) != "null") {
			return ErrAIProviderRequestFailed
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		choice := chunk.Choices[0]
		if choice.FinishReason != nil && *choice.FinishReason != "stop" {
			return ErrAIProviderRequestFailed
		}
		total += len(choice.Delta.Content)
		if total > assistantMaxReplyBytes {
			return ErrAIProviderRequestFailed
		}
		pending += choice.Delta.Content
		if apiKey == "" {
			if pending != "" {
				if err := emit(pending); err != nil {
					return err
				}
				pending = ""
			}
		} else if err := flush(false); err != nil {
			return err
		}
	}
	// EOF alone is not successful completion: keep already persisted text, mark
	// the attempt interrupted, and require an explicit user retry.
	return ErrAIProviderRequestFailed
}
