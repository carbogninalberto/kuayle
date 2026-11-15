package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	gh "github.com/kuayle/kuayle-backend/pkg/github"
	"github.com/stretchr/testify/require"
)

// gateMerger merges on the fake GitHub; refusals are GitHub's reasons for the
// next merges, in order, and onRefuse lets a test move the base meanwhile.
type gateMerger struct {
	refusals []string
	attempts int
	merged   []string
	onRefuse func()
}

func (m *gateMerger) MergeTaskPullRequest(_ context.Context, _ uuid.UUID, repository string, number int, sha, _ string) error {
	m.attempts++
	if len(m.refusals) > 0 {
		reason := m.refusals[0]
		m.refusals = m.refusals[1:]
		if m.onRefuse != nil {
			m.onRefuse()
		}
		return &gh.ErrMergeRefused{Reason: reason}
	}
	m.merged = append(m.merged, fmt.Sprintf("%s#%d@%s", repository, number, sha))
	return nil
}

var gateNow = time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)

type mergeGateFixture struct {
	pr        *prFixture
	approvals *AssistantApprovals
	merger    *gateMerger
	payload   domain.MergePayload
}

func newMergeGateFixture() *mergeGateFixture {
	pr := newPRFixture()
	yes := true
	pr.github.status = PullRequestStatus{State: "open", HeadSHA: "head1", BaseRef: "main", MergeBase: "base1", Mergeable: &yes, MergeableState: "clean"}
	pr.github.checks = passingChecks
	f := &mergeGateFixture{pr: pr, merger: &gateMerger{}}
	target := &repository.MergeTarget{TaskID: pr.task, IssueID: pr.issue.ID, CreatorID: pr.creator, Repository: "acme/app", Number: 12, URL: "https://github.com/acme/app/pull/12", Commit: "head1"}
	f.approvals = NewAssistantApprovals(nil, nil)
	f.approvals.SetMerging(prWork{merge: target}, prMembers{pr.creator: domain.RoleMember, pr.admin: domain.RoleAdmin}, f.merger)
	f.approvals.SetPullRequests(pr.github, pr.targets)
	f.approvals.ships.now = func() time.Time { return gateNow }
	f.payload = domain.MergePayload{Identifier: "ENG-1", TaskID: pr.task, Repository: "acme/app", PullRequest: 12, Commit: "head1", MergeMethod: "squash"}
	return f
}

func (f *mergeGateFixture) merge(t *testing.T) string {
	t.Helper()
	task, message, err := f.approvals.applyMerge(context.Background(), f.pr.ws, f.pr.admin, f.pr.issue.ID, f.payload)
	require.NoError(t, err)
	if message == "" {
		require.Equal(t, f.pr.task, *task)
	}
	return message
}

// CI checks the pull request head, not a merge with its base, so a head that
// lacks the base's latest commits was never tested with them (KUA-194: two
// branches each added migration 162 and both merged green). Kuayle merges only
// a head that contains its base and whose checks passed.
func TestMergeOnlyAnUpToDateHeadWhoseChecksPassed(t *testing.T) {
	t.Run("up to date and green merges", func(t *testing.T) {
		f := newMergeGateFixture()
		require.Empty(t, f.merge(t))
		require.Equal(t, []string{"acme/app#12@head1"}, f.merger.merged)
		require.Empty(t, f.pr.github.calls, "an up-to-date branch is not updated")
	})

	t.Run("no checks on the head or the base tip merges", func(t *testing.T) {
		f := newMergeGateFixture()
		f.pr.github.checks = nil
		require.Empty(t, f.merge(t), "a repository without CI has nothing to wait for")
		require.Len(t, f.merger.merged, 1)
	})

	// Right after update-branch the new head has no checks registered yet; a
	// quick second approval must not merge it untested.
	t.Run("no checks on the head while the base tip has some is held", func(t *testing.T) {
		f := newMergeGateFixture()
		f.pr.github.checks = nil
		f.pr.github.commitChecks = map[string][]PullRequestCheck{"base1": passingChecks}
		_, hold, err := f.approvals.mergeApproved(context.Background(), f.pr.ws, f.pr.admin, f.pr.issue.ID, f.payload)
		require.NoError(t, err)
		require.True(t, hold.pending, "the ship chain keeps waiting for the checks to start")
		require.Contains(t, hold.reason, "no checks have reported on head1")
		require.Contains(t, hold.reason, "main's tip has 2")
		require.Zero(t, f.merger.attempts)
	})

	t.Run("no checks on a head committed moments ago is held", func(t *testing.T) {
		f := newMergeGateFixture()
		f.pr.github.checks = nil
		f.pr.github.committed = map[string]time.Time{"head1": gateNow.Add(-30 * time.Second)}
		_, hold, err := f.approvals.mergeApproved(context.Background(), f.pr.ws, f.pr.admin, f.pr.issue.ID, f.payload)
		require.NoError(t, err)
		require.True(t, hold.pending)
		require.Contains(t, hold.reason, "committed less than 2m0s ago")
		require.Zero(t, f.merger.attempts)

		f.pr.github.committed["head1"] = gateNow.Add(-3 * time.Minute)
		require.Empty(t, f.merge(t), "once the grace window passed, a repository without CI merges")
		require.Len(t, f.merger.merged, 1)
	})

	t.Run("no checks on the head and an unknown base tip is held", func(t *testing.T) {
		f := newMergeGateFixture()
		f.pr.github.checks = nil
		f.pr.github.status.MergeBase = ""
		require.Contains(t, f.merge(t), "no checks have reported on head1")
		require.Zero(t, f.merger.attempts)
	})

	t.Run("behind its base is updated instead of merged", func(t *testing.T) {
		f := newMergeGateFixture()
		f.pr.github.status.Behind, f.pr.github.status.MergeableState = 3, "behind"
		message := f.merge(t)
		require.Contains(t, message, "3 commit(s) behind main")
		require.Contains(t, message, "was not merged")
		require.Contains(t, message, "new head head2")
		require.Contains(t, message, "ask to merge again once they pass")
		require.Equal(t, []string{"update:head1"}, f.pr.github.calls, "the update is pinned to the approved head")
		require.Zero(t, f.merger.attempts, "nothing is merged before CI ran on the updated head")
	})

	t.Run("conflicting with its base is sent back", func(t *testing.T) {
		f := newMergeGateFixture()
		no := false
		f.pr.github.status.Behind, f.pr.github.status.Mergeable, f.pr.github.status.MergeableState = 2, &no, "dirty"
		message := f.merge(t)
		require.Contains(t, message, "branch conflicts with main; needs a rebase")
		require.Contains(t, message, "update_pull_request_branch")
		require.Empty(t, f.pr.github.calls, "GitHub's update-branch cannot resolve conflicts")
		require.Zero(t, f.merger.attempts)
	})

	t.Run("an update GitHub refuses for conflicts is sent back", func(t *testing.T) {
		f := newMergeGateFixture()
		f.pr.github.status.Behind, f.pr.github.status.Mergeable = 2, nil
		f.pr.github.writeErr = &gh.APIError{Status: 422, Message: "merge conflict between base and head"}
		message := f.merge(t)
		require.Contains(t, message, "branch conflicts with main; needs a rebase")
		require.Equal(t, []string{"update:head1"}, f.pr.github.calls)
		require.Zero(t, f.merger.attempts)
	})

	t.Run("an update that cannot be made says why", func(t *testing.T) {
		f := newMergeGateFixture()
		f.pr.github.status.Behind = 1
		f.pr.github.writeErr = &GitHubBotAbilityDisabledError{Ability: domain.GitHubBotPullRequestActions}
		message := f.merge(t)
		require.Contains(t, message, "1 commit(s) behind main")
		require.Contains(t, message, "turned off")
		require.Zero(t, f.merger.attempts)
	})

	t.Run("unknown distance from the base is not merged", func(t *testing.T) {
		f := newMergeGateFixture()
		f.pr.github.status.Behind = -1
		message := f.merge(t)
		require.Contains(t, message, "contains the latest main")
		require.Zero(t, f.merger.attempts)
	})

	for state, want := range map[string]string{"pending": "still running", "failed": "failed on head1: Tests"} {
		t.Run(state+" checks are not merged", func(t *testing.T) {
			f := newMergeGateFixture()
			f.pr.github.checks = []PullRequestCheck{{Name: "Tests", State: state}, {Name: "Security", State: "passed"}}
			message := f.merge(t)
			require.Contains(t, message, want)
			require.Contains(t, message, "was not merged")
			require.Zero(t, f.merger.attempts)
		})
	}

	t.Run("checks that could not all be read are not merged", func(t *testing.T) {
		f := newMergeGateFixture()
		f.pr.github.coverage = ChecksCoverage{StatusesUnavailable: true}
		message := f.merge(t)
		require.Contains(t, message, "not known that CI passed")
		require.Zero(t, f.merger.attempts)
	})

	t.Run("a draft is left to GitHub's refusal", func(t *testing.T) {
		f := newMergeGateFixture()
		f.pr.github.status.Draft, f.pr.github.status.Behind = true, 2
		f.merger.refusals = []string{"Pull Request is still a draft"}
		message := f.merge(t)
		require.Contains(t, message, "mark_pull_request_ready")
		require.Empty(t, f.pr.github.calls, "a draft's branch is not updated")
	})

	t.Run("a head that moved is not updated or merged", func(t *testing.T) {
		f := newMergeGateFixture()
		f.pr.github.status.HeadSHA, f.pr.github.status.Behind = "head9", 4
		message := f.merge(t)
		require.Contains(t, message, "head moved to head9")
		require.Empty(t, f.pr.github.calls)
		require.Zero(t, f.merger.attempts)
	})
}

// GitHub answers 405 "Base branch was modified" when the base moves while it
// merges: Kuayle checks again rather than failing, so a base that now has new
// commits gets the branch updated and anything else is merged.
func TestMergeChecksAgainWhenTheBaseMovesDuringTheMerge(t *testing.T) {
	const moved = "Base branch was modified. Review and try the merge again."

	t.Run("base gained commits", func(t *testing.T) {
		f := newMergeGateFixture()
		f.merger.refusals = []string{moved}
		f.merger.onRefuse = func() { f.pr.github.status.Behind = 1 }
		message := f.merge(t)
		require.Contains(t, message, "1 commit(s) behind main")
		require.Equal(t, []string{"update:head1"}, f.pr.github.calls)
		require.Equal(t, 1, f.merger.attempts)
		require.Empty(t, f.merger.merged)
	})

	t.Run("base still contained", func(t *testing.T) {
		f := newMergeGateFixture()
		f.merger.refusals = []string{moved}
		require.Empty(t, f.merge(t))
		require.Equal(t, []string{"acme/app#12@head1"}, f.merger.merged)
		require.Equal(t, 2, f.merger.attempts)
	})

	t.Run("strict branch protection reports the branch out of date", func(t *testing.T) {
		f := newMergeGateFixture()
		f.merger.refusals = []string{"Head branch is not up to date with the base branch"}
		f.merger.onRefuse = func() { f.pr.github.status.Behind = 2 }
		message := f.merge(t)
		require.Contains(t, message, "2 commit(s) behind main")
		require.Equal(t, []string{"update:head1"}, f.pr.github.calls)
	})

	t.Run("base keeps moving", func(t *testing.T) {
		f := newMergeGateFixture()
		f.merger.refusals = []string{moved, moved, moved, moved}
		message := f.merge(t)
		require.Equal(t, mergeAttempts, f.merger.attempts, "the re-checks are bounded")
		require.Contains(t, message, "base branch moved while GitHub was merging")
		require.Empty(t, f.merger.merged)
	})
}

// The ship chain runs in the background, so it waits for the head's running
// checks before merging instead of failing; anything else stops it at once.
func TestShipWaitsForTheHeadsChecksBeforeMerging(t *testing.T) {
	t.Run("checks finish", func(t *testing.T) {
		f := newShipFixture(false)
		f.github.checks = []PullRequestCheck{{Name: "Tests", State: "pending"}}
		sleep := f.approvals.ships.sleep
		f.approvals.ships.sleep = func(ctx context.Context, d time.Duration) error {
			f.github.checks = passingChecks
			return sleep(ctx, d)
		}
		require.Empty(t, f.run(t))
		require.Equal(t, []string{"acme/app#12@head1/squash"}, f.merger.merged)
	})

	t.Run("checks not registered yet", func(t *testing.T) {
		f := newShipFixture(false)
		f.github.checks = nil
		f.github.commitChecks = map[string][]PullRequestCheck{"base1": passingChecks}
		sleep := f.approvals.ships.sleep
		f.approvals.ships.sleep = func(ctx context.Context, d time.Duration) error {
			f.github.checks = passingChecks
			return sleep(ctx, d)
		}
		require.Empty(t, f.run(t))
		require.Equal(t, []string{"acme/app#12@head1/squash"}, f.merger.merged)
		require.Positive(t, f.sleeps, "the chain waited for the checks to start")
	})

	t.Run("checks never finish", func(t *testing.T) {
		f := newShipFixture(false)
		f.github.checks = []PullRequestCheck{{Name: "Tests", State: "pending"}}
		message := f.run(t)
		require.Contains(t, message, "Step 1 of 4 (merge the pull request) failed")
		require.Contains(t, message, "still running")
		require.Empty(t, f.merger.merged)
	})

	t.Run("branch behind its base", func(t *testing.T) {
		f := newShipFixture(false)
		f.github.status.Behind = 2
		message := f.run(t)
		require.Contains(t, message, "Step 1 of 4 (merge the pull request) failed")
		require.Contains(t, message, "2 commit(s) behind main")
		require.Contains(t, message, "nothing was merged or released")
		require.Equal(t, []string{"update:head1"}, f.github.calls)
		require.Empty(t, f.merger.merged)
		require.Empty(t, f.releases.created)
	})
}
