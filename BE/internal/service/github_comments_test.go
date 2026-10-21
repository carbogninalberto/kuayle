package service

import (
	"context"
	"errors"
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
		identifier  string
		lookupError bool
	}{
		{"branch", []string{"agent/kua-60-bot"}, "KUA-60", false},
		{"title", []string{"feature/bot", "Fix KUA-60"}, "KUA-60", false},
		{"body URL", []string{"feature/bot", "Bot", "https://kuayle.test/acme/issue/KUA-60"}, "KUA-60", false},
		{"same identifier", []string{"kua-60", "KUA-60", "Kua-60"}, "KUA-60", false},
		{"missing", []string{"agent/bot", "Bot"}, "", false},
		{"conflict", []string{"kua-60", "Fix KUA-61"}, "", false},
		{"multiple in body", []string{"KUA-60 and KUA-61"}, "", false},
		{"embedded", []string{"_KUA-60 KUA-60abc 1KUA-60"}, "", false},
		{"database failure", []string{"KUA-60"}, "KUA-60", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ws := uuid.New()
			repo := new(mockIssueRepo)
			svc := &GitHubService{issueRepo: repo}
			var want *domain.Issue
			var lookupErr error
			if tc.identifier != "" {
				want = &domain.Issue{ID: uuid.New(), WorkspaceID: ws, Identifier: tc.identifier}
				if tc.lookupError {
					want = nil
					lookupErr = errors.New("database unavailable")
				}
				repo.On("GetByIdentifier", mock.MatchedBy(func(ctx context.Context) bool { _, ok := domain.AccessFromContext(ctx); return !ok }), ws, tc.identifier).Return(want, lookupErr).Once()
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
