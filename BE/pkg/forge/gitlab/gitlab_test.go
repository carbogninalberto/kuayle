package gitlab

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

func headers(event, token, delivery string) http.Header {
	h := make(http.Header)
	h.Set(HeaderEvent, event)
	if token != "" {
		h.Set(HeaderToken, token)
	}
	if delivery != "" {
		h.Set(HeaderEventID, delivery)
	}
	return h
}

func TestVerifyWebhook(t *testing.T) {
	p := New(nil)
	body := []byte(`{"object_kind":"push"}`)
	require.NoError(t, p.VerifyWebhook(headers("Push Hook", "s3cret", ""), body, "s3cret"))
	require.Error(t, p.VerifyWebhook(headers("Push Hook", "wrong", ""), body, "s3cret"))
	require.Error(t, p.VerifyWebhook(headers("Push Hook", "", ""), body, "s3cret"))
	require.Error(t, p.VerifyWebhook(headers("Push Hook", "s3cret", ""), body, ""))
}

func TestDeliveryID(t *testing.T) {
	p := New(nil)
	require.Equal(t, "uuid-1", p.DeliveryID(headers("Push Hook", "", "uuid-1")))
	fallback := make(http.Header)
	fallback.Set(HeaderIdempotency, "idem-1")
	require.Equal(t, "idem-1", p.DeliveryID(fallback))
}

func mergeRequestBody(action, state string, draft bool, changes string) []byte {
	payload := map[string]any{
		"object_kind": "merge_request",
		"user":        map[string]any{"username": "alice"},
		"project":     map[string]any{"id": 42, "path_with_namespace": "team/app", "web_url": "https://gitlab.example.com/team/app"},
		"object_attributes": map[string]any{
			"id": 900, "iid": 7, "title": "KUA-1 fix", "description": "closes KUA-1",
			"state": state, "action": action, "source_branch": "feat/kua-1-x",
			"target_branch": "main", "url": "https://gitlab.example.com/team/app/-/merge_requests/7",
			"draft": draft,
		},
	}
	if changes != "" {
		payload["object_attributes"].(map[string]any)["changes"] = json.RawMessage(changes)
	}
	body, _ := json.Marshal(payload)
	return body
}

func TestParseMergeRequest(t *testing.T) {
	p := New(nil)
	for _, tc := range []struct {
		name       string
		action     string
		state      string
		draft      bool
		changes    string
		wantState  string
		wantAction string
	}{
		{"open", "open", "opened", false, "", "open", "opened"},
		{"merge", "merge", "merged", false, "", "merged", "merged"},
		{"close without merge", "close", "closed", false, "", "closed", "closed"},
		{"draft", "open", "opened", true, "", "draft", "opened"},
		{"ready", "update", "opened", false, `{"draft":{"previous":true,"current":false}}`, "open", "ready"},
		{"reopen", "reopen", "opened", false, "", "open", "reopened"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			event, err := p.ParseWebhook(headers(mergeRequestHook, "s3cret", ""), mergeRequestBody(tc.action, tc.state, tc.draft, tc.changes))
			require.NoError(t, err)
			require.Equal(t, domain.ForgeEventMergeRequest, event.Kind)
			require.Equal(t, int64(42), event.Repo.ExternalID)
			require.Equal(t, "team/app", event.Repo.FullName)
			require.Equal(t, 7, event.MergeRequest.Number)
			require.Equal(t, int64(900), event.MergeRequest.ExternalID)
			require.Equal(t, "KUA-1 fix", event.MergeRequest.Title)
			require.Equal(t, "feat/kua-1-x", event.MergeRequest.SourceBranch)
			require.Equal(t, "alice", event.MergeRequest.Author)
			require.Equal(t, tc.wantState, event.MergeRequest.State)
			require.Equal(t, tc.wantAction, event.MergeRequest.Action)
		})
	}
}

func TestParsePushCommitsAndBranchCreation(t *testing.T) {
	p := New(nil)
	body := []byte(`{
		"object_kind":"push","before":"0000000000000000000000000000000000000000","ref":"refs/heads/feat/KUA-1-x",
		"user_username":"alice",
		"project":{"id":42,"path_with_namespace":"team/app","web_url":"https://gitlab.example.com/team/app"},
		"commits":[{"id":"abc123","message":"KUA-1 fix","url":"https://gitlab.example.com/team/app/-/commit/abc123","timestamp":"2026-01-02T03:04:05Z","author":{"name":"Alice"}}]
	}`)
	event, err := p.ParseWebhook(headers(pushHook, "s3cret", ""), body)
	require.NoError(t, err)
	require.Equal(t, domain.ForgeEventPush, event.Kind)
	require.Len(t, event.Commits, 1)
	require.Equal(t, "abc123", event.Commits[0].SHA)
	require.Equal(t, "Alice", event.Commits[0].Author)
	require.NotNil(t, event.Branch)
	require.Equal(t, "feat/KUA-1-x", event.Branch.Name)
	require.Contains(t, event.Branch.WebURL, "/-/tree/feat")

	existing, err := p.ParseWebhook(headers(pushHook, "s3cret", ""), []byte(`{
		"object_kind":"push","before":"1111111111111111111111111111111111111111","ref":"refs/heads/main",
		"project":{"id":42,"path_with_namespace":"team/app","web_url":"https://gitlab.example.com/team/app"},
		"commits":[]
	}`))
	require.NoError(t, err)
	require.Nil(t, existing.Branch)
}

func TestParseUnknownEventIsIgnored(t *testing.T) {
	p := New(nil)
	event, err := p.ParseWebhook(headers("Issue Hook", "s3cret", ""), []byte(`{"object_kind":"issue"}`))
	require.NoError(t, err)
	require.Equal(t, domain.ForgeEventIgnored, event.Kind)
}

func TestCommentOnMergeRequest(t *testing.T) {
	var gotMethod, gotPath, gotToken, gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotToken = r.Method, r.URL.Path, r.Header.Get("PRIVATE-TOKEN")
		data, _ := io.ReadAll(r.Body)
		gotBody = string(data)
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	token := "access-token"
	conn := &domain.ForgeConnection{BaseURL: server.URL, AccessToken: &token}
	repo := &domain.ForgeRepo{ExternalRepoID: 42}
	require.NoError(t, New(server.Client()).CommentOnMergeRequest(context.Background(), conn, repo, 7, "hello"))
	require.Equal(t, http.MethodPost, gotMethod)
	require.Equal(t, "/api/v4/projects/42/merge_requests/7/notes", gotPath)
	require.Equal(t, token, gotToken)
	require.Contains(t, gotBody, `"body":"hello"`)
	require.False(t, strings.Contains(gotBody, "PRIVATE-TOKEN"))
}
