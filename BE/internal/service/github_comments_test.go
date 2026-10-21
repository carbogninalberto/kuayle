package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

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

func TestIssueCommentBody(t *testing.T) {
	body, err := issueCommentBody("https://kuayle.test/base/", "acme", "KUA-60")
	require.NoError(t, err)
	require.Equal(t, "Linked Kuayle issue: [KUA-60](https://kuayle.test/base/acme/issue/KUA-60)", body)
	for _, base := range []string{"", "javascript:alert(1)", "https://user:secret@kuayle.test", "https://kuayle.test?secret=x", "https://kuayle.test/#fragment"} {
		_, err := issueCommentBody(base, "acme", "KUA-60")
		require.Error(t, err)
	}
	_, err = issueCommentBody("https://kuayle.test", "acme", "KUA-60](https://evil)")
	require.Error(t, err)
}
