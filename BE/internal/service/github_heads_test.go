package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	gh "github.com/kuayle/kuayle-backend/pkg/github"
	"github.com/stretchr/testify/require"
)

type headStoreFake struct {
	recorded []string
	err      error
}

func (f *headStoreFake) RecordPullRequestHead(_ context.Context, _ uuid.UUID, _ string, _ int, sha string) error {
	f.recorded = append(f.recorded, sha)
	return f.err
}

func TestInspectPullRequestHeadRecordsTheFreshHeadAndDescribesWhatChanged(t *testing.T) {
	store := &headStoreFake{}
	var compared [2]string
	change := &domain.PullRequestHeadChange{Kind: "base_merge"}
	s := &GitHubService{
		heads: store,
		readPullRequestHead: func(context.Context, uuid.UUID, string, int) (string, error) {
			return "3220dbc9", nil
		},
		compareCommits: func(_ context.Context, _ uuid.UUID, _, published, head string) (*domain.PullRequestHeadChange, error) {
			compared = [2]string{published, head}
			return change, nil
		},
	}
	head, err := s.InspectPullRequestHead(context.Background(), uuid.New(), "acme/app", 108, "15af2c3a")
	require.NoError(t, err)
	require.Equal(t, "3220dbc9", head.SHA)
	require.Same(t, change, head.Change)
	require.Equal(t, [2]string{"15af2c3a", "3220dbc9"}, compared)
	require.Equal(t, []string{"3220dbc9"}, store.recorded)

	// An unchanged head is recorded too but needs no comparison.
	compared = [2]string{}
	head, err = s.InspectPullRequestHead(context.Background(), uuid.New(), "acme/app", 108, "3220dbc9")
	require.NoError(t, err)
	require.Nil(t, head.Change)
	require.Equal(t, [2]string{}, compared)
	require.Len(t, store.recorded, 2)
}

func TestInspectPullRequestHeadFailsWhenGitHubCannotBeRead(t *testing.T) {
	store := &headStoreFake{}
	s := &GitHubService{heads: store, readPullRequestHead: func(context.Context, uuid.UUID, string, int) (string, error) {
		return "", errors.New("GitHub not connected")
	}}
	_, err := s.InspectPullRequestHead(context.Background(), uuid.New(), "acme/app", 1, "aaa")
	require.EqualError(t, err, "GitHub not connected")
	require.Empty(t, store.recorded)
}

func TestHeadChangeLabelsABaseMergeAndReportsTreeChanges(t *testing.T) {
	merge := gh.Commit{SHA: "3220dbc9", Commit: gh.CommitData{Message: "Merge branch 'main' into kuayle/kua-66\n\nbody"}, Author: &gh.GitHubUser{Login: "maintainer"}, Parents: []gh.CommitRef{{SHA: "a"}, {SHA: "b"}}}
	change := headChange([]gh.Commit{merge}, false)
	require.Equal(t, "base_merge", change.Kind)
	require.False(t, change.TreeChanged)
	require.Equal(t, []domain.PullRequestHeadCommit{{SHA: "3220dbc9", Author: "maintainer", Subject: "Merge branch 'main' into kuayle/kua-66", Merge: true}}, change.Commits)

	push := gh.Commit{SHA: "f00d", Commit: gh.CommitData{Message: "Fix typo", Author: gh.CommitAuthor{Name: "Dev"}}, Parents: []gh.CommitRef{{SHA: "a"}}}
	change = headChange([]gh.Commit{merge, push}, true)
	require.Equal(t, "pushed", change.Kind, "a commit that is not a merge is a change to the task")
	require.True(t, change.TreeChanged)
	require.Equal(t, "Dev", change.Commits[1].Author)

	many := make([]gh.Commit, maxHeadChangeCommits+5)
	for i := range many {
		many[i] = merge
	}
	change = headChange(many, false)
	require.Len(t, change.Commits, maxHeadChangeCommits)
	require.True(t, change.Truncated)
}
