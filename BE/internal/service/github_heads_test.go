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
	change := &domain.PullRequestHeadChange{Kind: "merge"}
	s := &GitHubService{
		heads: store,
		readPullRequestHead: func(context.Context, uuid.UUID, string, int) (string, error) {
			return "3220dbc9", nil
		},
		compareCommits: func(_ context.Context, _ uuid.UUID, _ string, _ int, published, head string) (*domain.PullRequestHeadChange, error) {
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

func baseOnly(reachable ...string) func(string) (bool, error) {
	return func(sha string) (bool, error) {
		for _, r := range reachable {
			if r == sha {
				return true, nil
			}
		}
		return false, nil
	}
}

func TestHeadChangeLabelsABaseMergeWithItsBroughtInCommits(t *testing.T) {
	// KUA-66: main was merged into the branch. The compare list also holds the
	// main commits the merge brought in, each with a single parent.
	merge := gh.Commit{SHA: "3220dbc9", Commit: gh.CommitData{Message: "Merge branch 'main' into kuayle/kua-66\n\nbody"}, Author: &gh.GitHubUser{Login: "maintainer"}, Parents: []gh.CommitRef{{SHA: "15af2c3a"}, {SHA: "m2"}}}
	m1 := gh.Commit{SHA: "m1", Parents: []gh.CommitRef{{SHA: "m0"}}}
	m2 := gh.Commit{SHA: "m2", Parents: []gh.CommitRef{{SHA: "m1"}}}
	comparison := &gh.Comparison{Status: "ahead", TotalCommits: 3, Commits: []gh.Commit{m1, m2, merge}}
	change, err := headChange(comparison, &merge, "15af2c3a", false, baseOnly("m2"))
	require.NoError(t, err)
	require.Equal(t, "merge", change.Kind)
	require.False(t, change.TreeChanged)
	require.Equal(t, []domain.PullRequestHeadCommit{{SHA: "3220dbc9", Author: "maintainer", Subject: "Merge branch 'main' into kuayle/kua-66", Merge: true}}, change.Commits)

	// The merged-in parent is not on the base branch: it is someone's work.
	change, err = headChange(comparison, &merge, "15af2c3a", true, baseOnly())
	require.NoError(t, err)
	require.Equal(t, "pushed", change.Kind)
	require.True(t, change.TreeChanged)
}

func TestHeadChangeListsOnlyCommitsAddedSinceTheTruncatedFirstPage(t *testing.T) {
	push := gh.Commit{SHA: "f00d", Commit: gh.CommitData{Message: "Fix typo", Author: gh.CommitAuthor{Name: "Dev"}}, Parents: []gh.CommitRef{{SHA: "15af2c3a"}}}
	change, err := headChange(&gh.Comparison{Status: "ahead", TotalCommits: 1, Commits: []gh.Commit{push}}, &push, "15af2c3a", true, baseOnly())
	require.NoError(t, err)
	require.Equal(t, "pushed", change.Kind, "a commit that is not a merge is a change to the task")
	require.Equal(t, "Dev", change.Commits[0].Author)

	// Head missing from a truncated first page still anchors the chain.
	change, err = headChange(&gh.Comparison{Status: "ahead", TotalCommits: 300, Commits: []gh.Commit{{SHA: "old"}}}, &push, "15af2c3a", false, baseOnly())
	require.NoError(t, err)
	require.Equal(t, []string{"f00d"}, []string{change.Commits[0].SHA})
}

func TestHeadChangeReportsRewrittenHistory(t *testing.T) {
	head := gh.Commit{SHA: "a1", Parents: []gh.CommitRef{{SHA: "a"}, {SHA: "b"}}}
	change, err := headChange(&gh.Comparison{Status: "diverged", TotalCommits: 1, Commits: []gh.Commit{head}}, &head, "15af2c3a", false, baseOnly("b"))
	require.NoError(t, err)
	require.Equal(t, "rewritten", change.Kind, "a history without the published commit is not a push on top of it")
}

func TestHeadChangePropagatesBaseLookupFailures(t *testing.T) {
	head := gh.Commit{SHA: "a1", Parents: []gh.CommitRef{{SHA: "a"}, {SHA: "b"}}}
	_, err := headChange(&gh.Comparison{Status: "ahead"}, &head, "a", false, func(string) (bool, error) { return false, errors.New("boom") })
	require.EqualError(t, err, "boom")
}
