package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const completionBody = `{"choices":[{"message":{"role":"assistant","content":"<p>ok</p>"}}]}`

// Any user can own a workspace and choose its AI base URL, so completions must
// not reach the server's internal network or reveal why a connection failed.
func TestAICompletionRefusesPrivateNetworkBaseURL(t *testing.T) {
	var hits atomic.Int32
	internal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte(completionBody))
	}))
	defer internal.Close()
	svc := NewAISettingsService(nil, nil, nil, nil)

	_, err := svc.complete(context.Background(), internal.URL, "model", "key", "prompt")

	require.ErrorIs(t, err, ErrAIProviderRequestFailed)
	assert.Zero(t, hits.Load())
	assert.NotContains(t, err.Error(), "127.0.0.1")
}

func TestAICompletionPrivateNetworkOptInDoesNotFollowRedirects(t *testing.T) {
	var redirected atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		redirected.Add(1)
		_, _ = w.Write([]byte(completionBody))
	}))
	defer target.Close()
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ok/chat/completions" {
			_, _ = w.Write([]byte(completionBody))
			return
		}
		http.Redirect(w, r, target.URL+"/latest/meta-data", http.StatusTemporaryRedirect)
	}))
	defer provider.Close()
	svc := NewAISettingsService(nil, nil, nil, nil, WithAIPrivateNetworkAccess(true))

	content, err := svc.complete(context.Background(), provider.URL+"/ok", "model", "key", "prompt")
	require.NoError(t, err)
	assert.Equal(t, "<p>ok</p>", content)

	_, err = svc.complete(context.Background(), provider.URL+"/redirect", "model", "key", "prompt")
	require.ErrorIs(t, err, ErrAIProviderRequestFailed)
	assert.Zero(t, redirected.Load())
}
