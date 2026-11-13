package gitea

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

func sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func headers(event, secret, delivery string, body []byte) http.Header {
	h := make(http.Header)
	h.Set(headerForgejoEvent, event)
	h.Set(headerForgejoDelivery, delivery)
	if secret != "" {
		h.Set(headerForgejoSignature, sign(secret, body))
	}
	return h
}

func TestVerifyWebhook(t *testing.T) {
	p := New(domain.ForgeGitea, nil)
	body := []byte(`{"ref":"main"}`)
	require.NoError(t, p.VerifyWebhook(headers(eventPush, "s3cret", "d1", body), body, "s3cret"))
	require.Error(t, p.VerifyWebhook(headers(eventPush, "wrong", "d1", body), body, "s3cret"))
	require.Error(t, p.VerifyWebhook(headers(eventPush, "", "d1", body), body, "s3cret"))
	require.Error(t, p.VerifyWebhook(headers(eventPush, "s3cret", "d1", body), body, ""))

	gitea := make(http.Header)
	gitea.Set(headerGiteaSignature, sign("s3cret", body))
	require.NoError(t, p.VerifyWebhook(gitea, body, "s3cret"))
}

func TestProviderAndDeliveryID(t *testing.T) {
	require.Equal(t, domain.ForgeGitea, New(domain.ForgeGitea, nil).Provider())
	require.Equal(t, domain.ForgeForgejo, New(domain.ForgeForgejo, nil).Provider())
	h := make(http.Header)
	h.Set(headerGiteaDelivery, "gitea-1")
	require.Equal(t, "gitea-1", New(domain.ForgeGitea, nil).DeliveryID(h))
	h.Set(headerForgejoDelivery, "forgejo-1")
	require.Equal(t, "forgejo-1", New(domain.ForgeForgejo, nil).DeliveryID(h))
}

func pullRequestBody(action, state string, draft, merged bool) []byte {
	body, _ := json.Marshal(map[string]any{
		"action": action,
		"number": 3,
		"pull_request": map[string]any{
			"id": 300, "number": 3, "title": "KUA-1 fix", "body": "closes KUA-1",
			"state": state, "draft": draft, "merged": merged,
			"html_url": "https://gitea.example.com/team/app/pulls/3",
			"head":     map[string]any{"ref": "feat/kua-1-x"},
			"base":     map[string]any{"ref": "main"},
			"user":     map[string]any{"login": "alice"},
		},
		"repository": map[string]any{"id": 42, "full_name": "team/app", "html_url": "https://gitea.example.com/team/app"},
	})
	return body
}

func TestParsePullRequest(t *testing.T) {
	p := New(domain.ForgeGitea, nil)
	for _, tc := range []struct {
		name       string
		action     string
		state      string
		draft      bool
		merged     bool
		wantState  string
		wantAction string
	}{
		{"open", "opened", "open", false, false, "open", "opened"},
		{"merge", "closed", "closed", false, true, "merged", "merged"},
		{"close without merge", "closed", "closed", false, false, "closed", "closed"},
		{"draft", "opened", "open", true, false, "draft", "opened"},
		{"ready", "ready_for_review", "open", false, false, "open", "ready"},
		{"reopen", "reopened", "open", false, false, "open", "reopened"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := pullRequestBody(tc.action, tc.state, tc.draft, tc.merged)
			event, err := p.ParseWebhook(headers(eventPullRequest, "s3cret", "", body), body)
			require.NoError(t, err)
			require.Equal(t, domain.ForgeEventMergeRequest, event.Kind)
			require.Equal(t, int64(42), event.Repo.ExternalID)
			require.Equal(t, "team/app", event.Repo.FullName)
			require.Equal(t, 3, event.MergeRequest.Number)
			require.Equal(t, int64(300), event.MergeRequest.ExternalID)
			require.Equal(t, "feat/kua-1-x", event.MergeRequest.SourceBranch)
			require.Equal(t, "alice", event.MergeRequest.Author)
			require.Equal(t, tc.wantState, event.MergeRequest.State)
			require.Equal(t, tc.wantAction, event.MergeRequest.Action)
		})
	}
}

func TestParsePush(t *testing.T) {
	p := New(domain.ForgeGitea, nil)
	body := []byte(`{
		"ref":"refs/heads/main",
		"repository":{"id":42,"full_name":"team/app","html_url":"https://gitea.example.com/team/app"},
		"commits":[{"id":"abc123","message":"KUA-1 fix","url":"https://gitea.example.com/team/app/commit/abc123","timestamp":"2026-01-02T03:04:05Z","author":{"name":"Alice","username":"alice"}}]
	}`)
	event, err := p.ParseWebhook(headers(eventPush, "s3cret", "", body), body)
	require.NoError(t, err)
	require.Equal(t, domain.ForgeEventPush, event.Kind)
	require.Len(t, event.Commits, 1)
	require.Equal(t, "abc123", event.Commits[0].SHA)
	require.Equal(t, "alice", event.Commits[0].Author)
}

func TestParseCreateBranch(t *testing.T) {
	p := New(domain.ForgeForgejo, nil)
	body := []byte(`{"ref":"feat/KUA-1-x","ref_type":"branch","repository":{"id":42,"full_name":"team/app","html_url":"https://gitea.example.com/team/app"}}`)
	event, err := p.ParseWebhook(headers(eventCreate, "s3cret", "", body), body)
	require.NoError(t, err)
	require.Equal(t, domain.ForgeEventBranchCreated, event.Kind)
	require.Equal(t, "feat/KUA-1-x", event.Branch.Name)
	require.Contains(t, event.Branch.WebURL, "/src/branch/feat")

	tag, err := p.ParseWebhook(headers(eventCreate, "s3cret", "", body), []byte(`{"ref":"v1","ref_type":"tag","repository":{"id":42}}`))
	require.NoError(t, err)
	require.Equal(t, domain.ForgeEventIgnored, tag.Kind)
}

func TestParseUnknownEventIsIgnored(t *testing.T) {
	p := New(domain.ForgeGitea, nil)
	event, err := p.ParseWebhook(headers("issues", "s3cret", "", nil), []byte(`{}`))
	require.NoError(t, err)
	require.Equal(t, domain.ForgeEventIgnored, event.Kind)
}

func TestCommentOnMergeRequest(t *testing.T) {
	var gotMethod, gotPath, gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotAuth = r.Method, r.URL.Path, r.Header.Get("Authorization")
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	token := "access-token"
	conn := &domain.ForgeConnection{BaseURL: server.URL, AccessToken: &token}
	repo := &domain.ForgeRepo{FullName: "team/app"}
	require.NoError(t, New(domain.ForgeGitea, server.Client()).CommentOnMergeRequest(context.Background(), conn, repo, 3, "hello"))
	require.Equal(t, http.MethodPost, gotMethod)
	require.Equal(t, "/api/v1/repos/team/app/issues/3/comments", gotPath)
	require.Equal(t, "token "+token, gotAuth)
}
