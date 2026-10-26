package service

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAssistantStreamsBeforeProviderCompletes(t *testing.T) {
	release := make(chan struct{})
	received := make(chan string, 4)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		if request["stream"] != true || r.Header.Get("Authorization") != "Bearer secret-key" {
			t.Error("invalid streaming request")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Hello from the assistant. \"}}]}\n\n")
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Done.\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer provider.Close()
	defer close(release)
	svc := NewAISettingsService(nil, nil, nil, nil, WithAIPrivateNetworkAccess(true))
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- svc.streamAssistant(ctx, provider.URL, "model", "secret-key", []chatCompletionMessage{{Role: "user", Content: "Hi"}}, func(delta string) error { received <- delta; return nil })
	}()
	var first string
	select {
	case first = <-received:
	case <-ctx.Done():
		t.Fatal("no progressive output")
	}
	require.NotEmpty(t, first)
	release <- struct{}{}
	require.NoError(t, <-done)
	close(received)
	for delta := range received {
		first += delta
	}
	require.Equal(t, "Hello from the assistant. Done.", first)
}

func TestAssistantRejectsIncompleteAndUnsafeStreams(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"truncated", "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n"},
		{"finish without marker", "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\n"},
		{"invalid", "data: not-json\n\n"},
		{"provider error", "data: {\"error\":{\"message\":\"secret-key\"}}\n\n"},
		{"oversize", "data: " + strings.Repeat("x", 1<<20) + "\n\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, tc.body)
			}))
			defer provider.Close()
			svc := NewAISettingsService(nil, nil, nil, nil, WithAIPrivateNetworkAccess(true))
			err := svc.streamAssistant(context.Background(), provider.URL, "model", "secret-key", nil, func(string) error { return nil })
			require.ErrorIs(t, err, ErrAIProviderRequestFailed)
			require.NotContains(t, err.Error(), "secret-key")
		})
	}
}

func TestAssistantRedactsCredentialAcrossChunks(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, part := range []string{"Here is sec", "ret-", "key and a safe answer."} {
			data, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]string{"content": part}}}})
			fmt.Fprintf(w, "data: %s\n\n", data)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer provider.Close()
	svc := NewAISettingsService(nil, nil, nil, nil, WithAIPrivateNetworkAccess(true))
	var got string
	require.NoError(t, svc.streamAssistant(context.Background(), provider.URL, "model", "secret-key", nil, func(delta string) error { got += delta; require.NotContains(t, got, "secret-key"); return nil }))
	require.Equal(t, "Here is [REDACTED] and a safe answer.", got)
}

func TestAssistantStreamingPreservesNetworkBoundary(t *testing.T) {
	var hits int
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer target.Close()
	svc := NewAISettingsService(nil, nil, nil, nil)
	require.ErrorIs(t, svc.streamAssistant(context.Background(), target.URL, "model", "key", nil, func(string) error { return nil }), ErrAIProviderRequestFailed)
	require.Zero(t, hits)
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	svc = NewAISettingsService(nil, nil, nil, nil, WithAIPrivateNetworkAccess(true))
	require.ErrorIs(t, svc.streamAssistant(context.Background(), redirect.URL, "model", "key", nil, func(string) error { return nil }), ErrAIProviderRequestFailed)
	require.Zero(t, hits)
}
