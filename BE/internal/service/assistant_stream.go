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

const (
	assistantMaxReplyBytes = 64 << 10
	// assistantMaxToolCalls bounds the tool calls of one reply, across rounds.
	assistantMaxToolCalls = 400
	// assistantMaxRoundToolCalls bounds the parallel calls of one model round.
	assistantMaxRoundToolCalls = 32
	assistantMaxToolInputBytes = 16 << 10
)

// streamAssistant uses the existing SSRF-protected transport and never returns
// provider diagnostics (which may contain credentials). Only public reply text,
// not hidden reasoning, enters the conversation stream.
func (s *AISettingsService) streamAssistant(ctx context.Context, baseURL, model, apiKey string, messages []chatCompletionMessage, emit func(string) error) error {
	_, err := s.streamAssistantTools(ctx, baseURL, model, apiKey, messages, nil, emit)
	return err
}

// streamAssistantTools also offers tools. Requested calls are returned only when
// the provider finished the turn with finish_reason "tool_calls"; the caller
// decides, outside the model, whether and how each call may run.
func (s *AISettingsService) streamAssistantTools(ctx context.Context, baseURL, model, apiKey string, messages []chatCompletionMessage, tools []chatToolSpec, emit func(string) error) ([]chatToolCall, error) {
	body, err := json.Marshal(struct {
		Model    string                  `json:"model"`
		Messages []chatCompletionMessage `json:"messages"`
		Tools    []chatToolSpec          `json:"tools,omitempty"`
		Stream   bool                    `json:"stream"`
	}{model, messages, tools, true})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, ErrAIProviderRequestFailed
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	client := *s.httpClient
	client.Timeout = 90 * time.Second
	resp, err := client.Do(req)
	if err != nil {
		return nil, ErrAIProviderRequestFailed
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream") {
		return nil, ErrAIProviderRequestFailed
	}
	scanner := bufio.NewScanner(io.LimitReader(resp.Body, 2<<20))
	scanner.Buffer(make([]byte, 4096), 256<<10)
	pending, total, finished := "", 0, ""
	var calls []chatToolCall
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
			if err := flush(true); err != nil {
				return nil, err
			}
			if finished != "tool_calls" {
				return nil, nil
			}
			if len(calls) == 0 {
				return nil, ErrAIProviderRequestFailed
			}
			for _, call := range calls {
				if call.ID == "" || call.Function.Name == "" {
					return nil, ErrAIProviderRequestFailed
				}
			}
			return calls, nil
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content   string `json:"content"`
					ToolCalls []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
				FinishReason *string `json:"finish_reason"`
			} `json:"choices"`
			Error json.RawMessage `json:"error"`
		}
		if json.Unmarshal([]byte(event), &chunk) != nil || (len(chunk.Error) > 0 && string(chunk.Error) != "null") {
			return nil, ErrAIProviderRequestFailed
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		choice := chunk.Choices[0]
		if choice.FinishReason != nil {
			// Tool calls are accepted only when tools were offered.
			if *choice.FinishReason != "stop" && (*choice.FinishReason != "tool_calls" || len(tools) == 0) {
				return nil, ErrAIProviderRequestFailed
			}
			finished = *choice.FinishReason
		}
		for _, delta := range choice.Delta.ToolCalls {
			if len(tools) == 0 || delta.Index < 0 || delta.Index >= assistantMaxRoundToolCalls || delta.Index > len(calls) {
				return nil, ErrAIProviderRequestFailed
			}
			if delta.Index == len(calls) {
				calls = append(calls, chatToolCall{Type: "function"})
			}
			call := &calls[delta.Index]
			call.ID += delta.ID
			call.Function.Name += delta.Function.Name
			call.Function.Arguments += delta.Function.Arguments
			if len(call.ID) > 128 || len(call.Function.Name) > 64 || len(call.Function.Arguments) > assistantMaxToolInputBytes {
				return nil, ErrAIProviderRequestFailed
			}
		}
		total += len(choice.Delta.Content)
		if total > assistantMaxReplyBytes {
			return nil, ErrAIProviderRequestFailed
		}
		pending += choice.Delta.Content
		if apiKey == "" {
			if pending != "" {
				if err := emit(pending); err != nil {
					return nil, err
				}
				pending = ""
			}
		} else if err := flush(false); err != nil {
			return nil, err
		}
	}
	// EOF alone is not successful completion: keep already persisted text, mark
	// the attempt interrupted, and require an explicit user retry.
	return nil, ErrAIProviderRequestFailed
}
