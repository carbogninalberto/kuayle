package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestPullRequestIssueResolution(t *testing.T) {
	for _, tc := range []struct {
		name        string
		texts       []string
		candidates  []string
		want        string
		lookupError bool
	}{
		{"branch", []string{"agent/kua-60-bot"}, []string{"KUA-60"}, "KUA-60", false},
		{"title", []string{"feature/bot", "Fix KUA-60"}, []string{"KUA-60"}, "KUA-60", false},
		{"body URL", []string{"feature/bot", "https://kuayle.test/acme/issue/KUA-60"}, []string{"KUA-60"}, "KUA-60", false},
		{"same identifier", []string{"kua-60", "KUA-60", "Kua-60"}, []string{"KUA-60"}, "KUA-60", false},
		{"missing", []string{"agent/bot"}, nil, "", false},
		{"unknown", []string{"KUA-99"}, []string{"KUA-99"}, "", false},
		{"conflict", []string{"kua-60", "KUA-61"}, []string{"KUA-60", "KUA-61"}, "", false},
		{"embedded", []string{"KUA-60abc 1KUA-60"}, nil, "", false},
		{"underscore", []string{"KUA-60_fix"}, []string{"KUA-60"}, "KUA-60", false},
		{"ordinary tokens", []string{"kua-60-upgrade-node-18", "SHA-256 / UTF-8 / x86-64"}, []string{"KUA-60", "NODE-18", "SHA-256", "UTF-8", "X86-64"}, "KUA-60", false},
		{"database failure", []string{"KUA-60"}, []string{"KUA-60"}, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ws := uuid.New()
			repo := new(mockIssueRepo)
			svc := &GitHubService{issueRepo: repo}
			var want *domain.Issue
			for _, identifier := range tc.candidates {
				var found *domain.Issue
				var lookupErr error
				if identifier == "KUA-60" || identifier == "KUA-61" {
					found = &domain.Issue{ID: uuid.New(), WorkspaceID: ws, Identifier: identifier}
				}
				if identifier == tc.want {
					want = found
				}
				if tc.lookupError {
					found = nil
					lookupErr = errors.New("database unavailable")
				}
				repo.On("GetByIdentifier", mock.MatchedBy(func(ctx context.Context) bool { _, ok := domain.AccessFromContext(ctx); return !ok }), ws, identifier).Return(found, lookupErr).Once()
			}
			got, err := svc.resolvePullRequestIssue(domain.WithAccess(context.Background(), domain.Access{UserID: uuid.New()}), ws, tc.texts...)
			require.Equal(t, want, got)
			if tc.lookupError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			repo.AssertExpectations(t)
		})
	}
	t.Run("bounded candidates", func(t *testing.T) {
		var refs []string
		for n := 1; n <= 21; n++ {
			refs = append(refs, fmt.Sprintf("KUA-%d", n))
		}
		svc := &GitHubService{issueRepo: new(mockIssueRepo)}
		got, err := svc.resolvePullRequestIssue(context.Background(), uuid.New(), strings.Join(refs, " "))
		require.NoError(t, err)
		require.Nil(t, got)
	})
}

const issueLinkbackTestLink = "https://kuayle.test/base/acme/issue/KUA-60"

func issueLinkbackTestIssue() *domain.Issue {
	return &domain.Issue{Identifier: "KUA-60", Title: "Fix the flux capacitor", Description: strPtr("<p>Short <strong>summary</strong>.</p><p>Second</p>")}
}

func issueLinkbackDescription(t *testing.T, body string) string {
	t.Helper()
	start := strings.Index(body, "<p>") + len("<p>")
	end := strings.Index(body, "</p>")
	require.Greater(t, end, start)
	return body[start:end]
}

func TestIssueCommentBody(t *testing.T) {
	t.Run("exact body", func(t *testing.T) {
		body, err := issueCommentBody("https://kuayle.test/base/", "acme", issueLinkbackTestIssue())
		require.NoError(t, err)
		require.Equal(t, "<!-- kuayle:issue-linkback:start -->\n"+
			"<details>\n"+
			"<summary><a href=\""+issueLinkbackTestLink+"\" rel=\"nofollow noreferrer noopener\" target=\"_blank\">KUA-60 Fix the flux capacitor</a></summary>\n"+
			"<p>Short summary. Second</p>\n"+
			"</details>\n"+
			"<p><a href=\""+issueLinkbackTestLink+"\" rel=\"nofollow noreferrer noopener\" target=\"_blank\">Review in Kuayle</a></p>\n"+
			"<!-- kuayle:issue-linkback:end -->", body)
		require.NotContains(t, body, "\n\n")
	})
	t.Run("missing description", func(t *testing.T) {
		for _, description := range []*string{nil, strPtr(""), strPtr("   ")} {
			issue := issueLinkbackTestIssue()
			issue.Description = description
			body, err := issueCommentBody("https://kuayle.test/base/", "acme", issue)
			require.NoError(t, err)
			require.Equal(t, "No description provided.", issueLinkbackDescription(t, body))
		}
	})
	t.Run("description truncation", func(t *testing.T) {
		issue := issueLinkbackTestIssue()
		issue.Description = strPtr(strings.Repeat("é", 300))
		body, err := issueCommentBody("https://kuayle.test/base/", "acme", issue)
		require.NoError(t, err)
		description := issueLinkbackDescription(t, body)
		require.True(t, utf8.ValidString(description))
		runes := []rune(strings.TrimSuffix(description, "…"))
		require.Len(t, runes, 280)
		require.True(t, strings.HasSuffix(description, "…"))
	})
	t.Run("description at limit is not truncated", func(t *testing.T) {
		issue := issueLinkbackTestIssue()
		issue.Description = strPtr(strings.Repeat("é", 280))
		body, err := issueCommentBody("https://kuayle.test/base/", "acme", issue)
		require.NoError(t, err)
		require.Equal(t, strings.Repeat("é", 280), issueLinkbackDescription(t, body))
	})
	t.Run("title truncation", func(t *testing.T) {
		issue := issueLinkbackTestIssue()
		issue.Title = strings.Repeat("t", 250)
		body, err := issueCommentBody("https://kuayle.test/base/", "acme", issue)
		require.NoError(t, err)
		summary := body[strings.Index(body, "\">KUA-60")+len("\">KUA-60")+1 : strings.Index(body, "</a></summary>")]
		require.True(t, utf8.ValidString(summary))
		runes := []rune(strings.TrimSuffix(summary, "…"))
		require.Len(t, runes, 200)
		require.True(t, strings.HasSuffix(summary, "…"))
	})
	t.Run("title at limit is not truncated", func(t *testing.T) {
		issue := issueLinkbackTestIssue()
		issue.Title = strings.Repeat("t", 200)
		body, err := issueCommentBody("https://kuayle.test/base/", "acme", issue)
		require.NoError(t, err)
		require.Contains(t, body, ">KUA-60 "+strings.Repeat("t", 200)+"</a></summary>")
	})
}

func TestIssueCommentBodySanitizesUntrustedContent(t *testing.T) {
	for name, mutate := range map[string]func(*domain.Issue){
		"html injection in title": func(i *domain.Issue) { i.Title = `Fix </summary></details><img src=x onerror=alert(1)> now` },
		"html injection in description": func(i *domain.Issue) {
			i.Description = strPtr(`Hello </summary></details><img src=x onerror=alert(1)> <details><p>world`)
		},
		"quotes in title": func(i *domain.Issue) { i.Title = `He said "hi" <script>` },
		"mentions":        func(i *domain.Issue) { i.Description = strPtr("Ping @octocat please") },
		"code fences":     func(i *domain.Issue) { i.Description = strPtr("```js\ncode()\n```\n~~~\nalso fenced\n~~~") },
		"comment markers": func(i *domain.Issue) { i.Description = strPtr("a --> b <!-- c --> d") },
		"blank lines":     func(i *domain.Issue) { i.Description = strPtr("first\n\nsecond\n\n\nthird") },
		"markdown":        func(i *domain.Issue) { i.Description = strPtr("# Head\n- item\n**bold** [x](https://e)") },
	} {
		t.Run(name, func(t *testing.T) {
			issue := issueLinkbackTestIssue()
			mutate(issue)
			body, err := issueCommentBody("https://kuayle.test/base/", "acme", issue)
			require.NoError(t, err)
			require.Equal(t, 1, strings.Count(body, "<details>"))
			require.Equal(t, 1, strings.Count(body, "</details>"))
			require.Equal(t, 1, strings.Count(body, "<summary>"))
			require.Equal(t, 1, strings.Count(body, "</summary>"))
			require.Equal(t, 2, strings.Count(body, "<p>"))
			require.NotContains(t, body, "<img")
			require.NotContains(t, body, "<script")
			require.NotContains(t, body, "\n\n")
			require.NotContains(t, body, "`")
			// The only raw "-->" occurrences are the two template markers.
			require.Equal(t, 2, strings.Count(body, "-->"))
			require.Equal(t, 2, strings.Count(body, "<!-- kuayle:issue-linkback:"))
			require.True(t, utf8.ValidString(body))
		})
	}
	t.Run("mentions are broken", func(t *testing.T) {
		issue := issueLinkbackTestIssue()
		issue.Description = strPtr("Ping @octocat please")
		body, err := issueCommentBody("https://kuayle.test/base/", "acme", issue)
		require.NoError(t, err)
		require.Contains(t, body, "@\u200boctocat")
		require.NotContains(t, body, "@octocat")
	})
	t.Run("escaped output", func(t *testing.T) {
		issue := issueLinkbackTestIssue()
		issue.Title = `He said "hi" & <that>`
		body, err := issueCommentBody("https://kuayle.test/base/", "acme", issue)
		require.NoError(t, err)
		require.Contains(t, body, "He said &#34;hi&#34; &amp; &lt;that&gt;")
	})
	t.Run("markdown stays one line", func(t *testing.T) {
		issue := issueLinkbackTestIssue()
		issue.Description = strPtr("# Head\n- item\n**bold** [x](https://e)")
		body, err := issueCommentBody("https://kuayle.test/base/", "acme", issue)
		require.NoError(t, err)
		require.Equal(t, "# Head - item **bold** [x](https://e)", issueLinkbackDescription(t, body))
	})
	t.Run("code fences are neutralised", func(t *testing.T) {
		issue := issueLinkbackTestIssue()
		issue.Description = strPtr("```js\ncode()\n```")
		body, err := issueCommentBody("https://kuayle.test/base/", "acme", issue)
		require.NoError(t, err)
		require.Equal(t, "&#96;&#96;&#96;js code() &#96;&#96;&#96;", issueLinkbackDescription(t, body))
	})
}

func TestIssueCommentBodyRejectsInvalidInput(t *testing.T) {
	for _, base := range []string{"", "javascript:alert(1)", "https://user:secret@kuayle.test", "https://kuayle.test?secret=x", "https://kuayle.test/#fragment"} {
		_, err := issueCommentBody(base, "acme", issueLinkbackTestIssue())
		require.Error(t, err)
	}
	_, err := issueCommentBody("https://kuayle.test", "", issueLinkbackTestIssue())
	require.Error(t, err)
	issue := issueLinkbackTestIssue()
	issue.Identifier = "KUA-60](https://evil)"
	_, err = issueCommentBody("https://kuayle.test", "acme", issue)
	require.Error(t, err)
	_, err = issueCommentBody("https://kuayle.test", "acme", nil)
	require.Error(t, err)
}
