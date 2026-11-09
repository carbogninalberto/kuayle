package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
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

// Issue creation and issue details expand descriptions with one prompt, so a
// draft gets the same instructions and selection-only rewriting.
func TestDescriptionExpansionPromptIncludesDraftAndSelection(t *testing.T) {
	selected := "  fix login  "
	prompt := descriptionExpansionPrompt("Expand it.", "Login broken", "<p>fix login</p>", &selected)
	assert.Equal(t, "Expand it.\n\nIssue title:\nLogin broken\n\nCurrent description:\n<p>fix login</p>"+
		"\n\nSelected text to rewrite. Return only the replacement HTML for this selected text, not the full issue description:\nfix login", prompt)

	blank := "   "
	assert.NotContains(t, descriptionExpansionPrompt("Expand it.", "T", "", &blank), "Selected text")
	assert.NotContains(t, descriptionExpansionPrompt("Expand it.", "T", "", nil), "Selected text")
}

// A wiki rework uses wiki instructions, not the workspace's issue prompt, and
// asks only for the selection's replacement.
func TestWikiReworkPromptAsksForTheSelectionOnly(t *testing.T) {
	prompt := wikiReworkPrompt("Runbook", "<p>restart the api</p>", "  restart the api  ")
	assert.True(t, strings.HasPrefix(prompt, WikiReworkInstructions))
	assert.NotContains(t, prompt, "issue")
	assert.Contains(t, prompt, "Page title:\nRunbook\n\nCurrent page content:\n<p>restart the api</p>")
	assert.True(t, strings.HasSuffix(prompt, "not the whole page:\nrestart the api"))
}

func TestWikiReworkRequiresConfiguredAI(t *testing.T) {
	svc := NewAISettingsService(&fakeAISettingsRepo{}, nil, nil, []byte("01234567890123456789012345678901"))
	_, err := svc.ReworkWikiSelection(context.Background(), uuid.New(), "Runbook", "<p>x</p>", "x")
	require.ErrorIs(t, err, ErrAISettingsNotConfigured)
}

// fakeAISettingsRepo stores nothing: every workspace has default settings.
type fakeAISettingsRepo struct{ repository.AISettingsRepo }

func (*fakeAISettingsRepo) GetByWorkspaceID(context.Context, uuid.UUID) (*domain.AISettings, error) {
	return nil, nil
}

func TestDraftDescriptionExpansionRequiresConfiguredAI(t *testing.T) {
	key := "encrypted"
	assert.False(t, aiSettingsConfigured(&domain.AISettings{Model: "m", APIKeyEncrypted: &key}))
	assert.False(t, aiSettingsConfigured(&domain.AISettings{BaseURL: "https://ai.example", APIKeyEncrypted: &key}))
	assert.False(t, aiSettingsConfigured(&domain.AISettings{BaseURL: "https://ai.example", Model: "m"}))
	assert.True(t, aiSettingsConfigured(&domain.AISettings{BaseURL: "https://ai.example", Model: "m", APIKeyEncrypted: &key}))
}
