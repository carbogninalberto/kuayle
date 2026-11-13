package service

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestMergeRequestTransition(t *testing.T) {
	for _, tc := range []struct {
		name     string
		action   string
		inserted bool
		merged   bool
		want     string
	}{
		{"new open", "opened", true, false, "pr_opened"},
		{"repeated open", "opened", false, false, ""},
		{"ready", "ready", false, false, "pr_opened"},
		{"merged", "closed", false, true, "pr_merged"},
		{"closed without merge", "closed", false, false, ""},
		{"updated", "updated", true, false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, mergeRequestTransition(tc.action, tc.inserted, tc.merged))
		})
	}
}

func TestForgeBaseURLValidation(t *testing.T) {
	public := &ForgeService{}
	for raw, want := range map[string]string{
		"https://gitlab.example.com/":     "https://gitlab.example.com",
		"https://gitea.example.com:8443/": "https://gitea.example.com:8443",
	} {
		got, err := public.normalizeBaseURL(raw)
		require.NoError(t, err, raw)
		require.Equal(t, want, got, raw)
	}
	for _, raw := range []string{
		"http://gitlab.example.com",
		"ftp://gitlab.example.com",
		"https://user:pass@gitlab.example.com",
		"https://gitlab.example.com/?token=x",
		"https://gitlab.example.com/#fragment",
	} {
		_, err := public.normalizeBaseURL(raw)
		require.Error(t, err, raw)
	}

	private := &ForgeService{allowPrivate: true}
	got, err := private.normalizeBaseURL("http://gitlab.internal:8080/")
	require.NoError(t, err)
	require.Equal(t, "http://gitlab.internal:8080", got)
}

func TestForgeProviderValidation(t *testing.T) {
	for _, provider := range []string{"gitlab", "gitea", "forgejo"} {
		require.True(t, validForgeProvider(provider), provider)
	}
	for _, provider := range []string{"github", "bitbucket", "", "GitLab"} {
		require.False(t, validForgeProvider(provider), provider)
	}
}

func TestGitHubForgeImplementsWebhook(t *testing.T) {
	var webhook domain.ForgeWebhook = githubForge{}
	require.Equal(t, domain.ForgeGitHub, webhook.Provider())
}

func TestGitHubForgeParsesPullRequest(t *testing.T) {
	body := []byte(`{
		"action":"opened","number":5,
		"pull_request":{"id":55,"number":5,"title":"KUA-1 fix","body":"closes KUA-1","state":"open","draft":false,"merged":false,
			"html_url":"https://github.com/team/app/pull/5","head":{"ref":"feat/kua-1-x"},"base":{"ref":"main"},"user":{"login":"alice"}},
		"repository":{"id":42,"full_name":"team/app","html_url":"https://github.com/team/app"}
	}`)
	header := make(http.Header)
	header.Set("X-GitHub-Event", "pull_request")
	event, err := (githubForge{}).ParseWebhook(header, body)
	require.NoError(t, err)
	require.Equal(t, domain.ForgeEventMergeRequest, event.Kind)
	require.Equal(t, int64(42), event.Repo.ExternalID)
	require.Equal(t, int64(55), event.MergeRequest.ExternalID)
	require.Equal(t, 5, event.MergeRequest.Number)
	require.Equal(t, "opened", event.MergeRequest.Action)
	require.Equal(t, "open", event.MergeRequest.State)
}

func TestGitHubForgeParsesMergeAndReady(t *testing.T) {
	for _, tc := range []struct {
		action, event string
		merged, draft bool
		wantAction    string
		wantState     string
	}{
		{"closed", "pull_request", true, false, "merged", "merged"},
		{"closed", "pull_request", false, false, "closed", "closed"},
		{"ready_for_review", "pull_request", false, false, "ready", "open"},
		{"opened", "pull_request", false, true, "opened", "draft"},
	} {
		body := []byte(`{"action":"` + tc.action + `","number":5,"pull_request":{"id":55,"number":5,"title":"KUA-1","state":"open","draft":` + boolString(tc.draft) + `,"merged":` + boolString(tc.merged) + `,"head":{"ref":"b"},"base":{"ref":"main"},"user":{"login":"alice"}},"repository":{"id":42}}`)
		header := make(http.Header)
		header.Set("X-GitHub-Event", tc.event)
		event, err := (githubForge{}).ParseWebhook(header, body)
		require.NoError(t, err)
		require.Equal(t, tc.wantAction, event.MergeRequest.Action)
		require.Equal(t, tc.wantState, event.MergeRequest.State)
	}
}

func TestGitHubForgeParsesPushCreateAndIgnored(t *testing.T) {
	header := make(http.Header)
	header.Set("X-GitHub-Event", "push")
	event, err := (githubForge{}).ParseWebhook(header, []byte(`{"ref":"refs/heads/main","commits":[{"id":"abc123","message":"KUA-1","timestamp":"2026-01-02T03:04:05Z","author":{"username":"alice"}}],"repository":{"id":42,"full_name":"team/app"}}`))
	require.NoError(t, err)
	require.Equal(t, domain.ForgeEventPush, event.Kind)
	require.Len(t, event.Commits, 1)
	require.Equal(t, "abc123", event.Commits[0].SHA)

	header.Set("X-GitHub-Event", "create")
	event, err = (githubForge{}).ParseWebhook(header, []byte(`{"ref":"feat/KUA-1","ref_type":"branch","repository":{"id":42,"html_url":"https://github.com/team/app"}}`))
	require.NoError(t, err)
	require.Equal(t, domain.ForgeEventBranchCreated, event.Kind)
	require.Equal(t, "feat/KUA-1", event.Branch.Name)

	header.Set("X-GitHub-Event", "issues")
	event, err = (githubForge{}).ParseWebhook(header, []byte(`{}`))
	require.NoError(t, err)
	require.Equal(t, domain.ForgeEventIgnored, event.Kind)
}

func boolString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

// TestGitHubForgeParseWebhookParity runs the payload shapes pinned by the
// GitHub regression tests through the forge adapter. Both paths decode the
// same ghWebhook* structs, so the adapter cannot drift from the live processor.
func TestGitHubForgeParseWebhookParity(t *testing.T) {
	repository := func() map[string]any {
		return map[string]any{"id": 42, "full_name": "acme/app", "html_url": "https://github.com/acme/app"}
	}
	pullRequest := func(action string, merged, draft bool, state string) []byte {
		body, err := json.Marshal(map[string]any{
			"action": action,
			"number": 1,
			"pull_request": map[string]any{
				"id": 1001, "number": 1, "title": "KUA-1 fix", "body": "Closes KUA-1",
				"state": state, "draft": draft, "merged": merged,
				"html_url": "https://github.com/acme/app/pull/1",
				"head":     map[string]any{"ref": "feat/kua-1-x"},
				"base":     map[string]any{"ref": "main"},
				"user":     map[string]any{"login": "alice"},
			},
			"repository": repository(),
		})
		require.NoError(t, err)
		return body
	}

	header := make(http.Header)
	header.Set("X-GitHub-Event", "pull_request")
	for _, tc := range []struct {
		name          string
		action, state string
		merged, draft bool
		wantAction    string
		wantState     string
	}{
		{"opened", "opened", "open", false, false, "opened", "open"},
		{"merged", "closed", "closed", true, false, "merged", "merged"},
		{"closed", "closed", "closed", false, false, "closed", "closed"},
		{"ready", "ready_for_review", "open", false, false, "ready", "open"},
		{"draft", "opened", "open", false, true, "opened", "draft"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			event, err := (githubForge{}).ParseWebhook(header, pullRequest(tc.action, tc.merged, tc.draft, tc.state))
			require.NoError(t, err)
			require.Equal(t, domain.ForgeEventMergeRequest, event.Kind)
			require.Equal(t, int64(42), event.Repo.ExternalID)
			require.Equal(t, "acme/app", event.Repo.FullName)
			require.Equal(t, "https://github.com/acme/app", event.Repo.WebURL)
			require.Equal(t, int64(1001), event.MergeRequest.ExternalID)
			require.Equal(t, tc.wantAction, event.MergeRequest.Action)
			require.Equal(t, tc.wantState, event.MergeRequest.State)
			// The live processor resolves the issue from these three fields.
			require.Equal(t, []string{"kua-1"}, issueIdentifierRegex.FindAllString(event.MergeRequest.SourceBranch, -1))
			require.Equal(t, []string{"KUA-1"}, issueIdentifierRegex.FindAllString(event.MergeRequest.Title, -1))
			require.Equal(t, []string{"KUA-1"}, issueIdentifierRegex.FindAllString(event.MergeRequest.Description, -1))
		})
	}

	pushHeader := make(http.Header)
	pushHeader.Set("X-GitHub-Event", "push")
	push, err := json.Marshal(map[string]any{
		"ref": "refs/heads/main",
		"commits": []map[string]any{{
			"id": "abc123", "message": "KUA-1 fix the thing",
			"url": "https://github.com/acme/app/commit/abc123", "timestamp": "2026-01-02T03:04:05Z",
			"author": map[string]any{"username": "alice"},
		}},
		"repository": repository(),
	})
	require.NoError(t, err)
	event, err := (githubForge{}).ParseWebhook(pushHeader, push)
	require.NoError(t, err)
	require.Equal(t, domain.ForgeEventPush, event.Kind)
	require.Equal(t, int64(42), event.Repo.ExternalID)
	require.Equal(t, "acme/app", event.Repo.FullName)
	require.Len(t, event.Commits, 1)
	require.Equal(t, "abc123", event.Commits[0].SHA)
	require.Equal(t, []string{"KUA-1"}, issueIdentifierRegex.FindAllString(event.Commits[0].Message, -1))

	createHeader := make(http.Header)
	createHeader.Set("X-GitHub-Event", "create")
	create, err := json.Marshal(map[string]any{
		"ref_type": "branch", "ref": "feat/kua-1-x", "repository": repository(),
	})
	require.NoError(t, err)
	event, err = (githubForge{}).ParseWebhook(createHeader, create)
	require.NoError(t, err)
	require.Equal(t, domain.ForgeEventBranchCreated, event.Kind)
	require.Equal(t, int64(42), event.Repo.ExternalID)
	require.Equal(t, "acme/app", event.Repo.FullName)
	require.Equal(t, "feat/kua-1-x", event.Branch.Name)
	require.Equal(t, []string{"kua-1"}, issueIdentifierRegex.FindAllString(event.Branch.Name, -1))
}

func TestForgeWebhookURL(t *testing.T) {
	id := uuid.MustParse("11111111-2222-3333-4444-555555555555")
	fallback := &ForgeService{frontendURL: "http://localhost:5173/"}
	require.Equal(t, "http://localhost:8080/api/forges/webhook/"+id.String(), fallback.webhookURL(id))

	production := &ForgeService{frontendURL: "https://kuayle.example.com"}
	require.Equal(t, "https://kuayle.example.com/api/forges/webhook/"+id.String(), production.webhookURL(id))

	override := &ForgeService{
		frontendURL:    "http://localhost:5173",
		webhookBaseURL: "https://api.kuayle.example.com/",
	}
	require.Equal(t, "https://api.kuayle.example.com/api/forges/webhook/"+id.String(), override.webhookURL(id))
}

func TestForgeRepoWebURLValidation(t *testing.T) {
	for raw, want := range map[string]bool{
		"":                                 true,
		"https://gitlab.example/g/p":       true,
		"http://gitea.internal:3000/o/r":   true,
		"javascript:alert(1)":              false,
		"/relative":                        false,
		"ftp://x":                          false,
		"https://":                         false,
		"https://user:pass@gitlab.example": false,
	} {
		require.Equal(t, want, validForgeRepoWebURL(raw), raw)
	}
}
