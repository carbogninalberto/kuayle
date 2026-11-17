package service

import (
	"context"
	"errors"
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
		_, hold, _, err := f.approvals.mergeApproved(context.Background(), f.pr.ws, f.pr.admin, f.pr.issue.ID, f.payload)
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
		_, hold, _, err := f.approvals.mergeApproved(context.Background(), f.pr.ws, f.pr.admin, f.pr.issue.ID, f.payload)
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

	t.Run("behind its base is updated and waits instead of merging", func(t *testing.T) {
		f := newMergeGateFixture()
		f.pr.github.status.Behind, f.pr.github.status.MergeableState = 3, "behind"
		f.updateTo("head1", "head2", "base2")
		_, hold, wait, err := f.approvals.mergeApproved(context.Background(), f.pr.ws, f.pr.admin, f.pr.issue.ID, f.payload)
		require.NoError(t, err)
		require.True(t, hold.updated, "the approved merge waits for the new head's checks")
		require.Contains(t, hold.reason, "3 commit(s) behind main")
		require.Contains(t, hold.reason, "new head head2")
		require.Contains(t, hold.reason, "merges it once the checks on the new head pass")
		require.Equal(t, "head1", wait.Approved)
		require.Equal(t, "head2", wait.Head, "the verified update is the head Kuayle merges")
		require.Equal(t, 1, wait.Updates)
		require.False(t, wait.Updating)
		require.Equal(t, gateNow.Add(mergeWaitTimeout), wait.Deadline)
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
		f.updateTo("head1", "head2", "base2")
		_, hold, wait, err := f.approvals.mergeApproved(context.Background(), f.pr.ws, f.pr.admin, f.pr.issue.ID, f.payload)
		require.NoError(t, err)
		require.True(t, hold.updated)
		require.Contains(t, hold.reason, "1 commit(s) behind main")
		require.Equal(t, "head2", wait.Head)
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
		f.updateTo("head1", "head2", "base2")
		_, hold, _, err := f.approvals.mergeApproved(context.Background(), f.pr.ws, f.pr.admin, f.pr.issue.ID, f.payload)
		require.NoError(t, err)
		require.True(t, hold.updated)
		require.Contains(t, hold.reason, "2 commit(s) behind main")
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

	// KUA-207: the chain merges the head Kuayle's update created once its
	// checks pass, and saves the wait so a resumed chain merges that head.
	t.Run("branch behind its base merges the update once green", func(t *testing.T) {
		f := newShipFixture(false)
		f.github.status.Behind = 2
		verifiedUpdate(f.github, "head1", "head2", "base2")
		update := f.github.onUpdate
		f.github.onUpdate = func() {
			update()
			f.github.checks = []PullRequestCheck{{Name: "Tests", State: "pending"}}
		}
		sleep := f.approvals.ships.sleep
		f.approvals.ships.sleep = func(ctx context.Context, d time.Duration) error {
			f.github.checks = passingChecks
			return sleep(ctx, d)
		}
		require.Empty(t, f.run(t))
		require.Equal(t, []string{"update:head1"}, f.github.calls)
		require.Equal(t, []string{"acme/app#12@head2/squash"}, f.merger.merged)
		require.NotNil(t, f.saved[1].Merge.Wait, "the wait is saved with the chain")
		require.Equal(t, "head2", f.saved[1].Merge.Wait.Head)
	})

	t.Run("branch behind its base whose update is not green in time", func(t *testing.T) {
		f := newShipFixture(false)
		f.github.status.Behind = 2
		verifiedUpdate(f.github, "head1", "head2", "base2")
		update := f.github.onUpdate
		f.github.onUpdate = func() {
			update()
			f.github.checks = []PullRequestCheck{{Name: "Tests", State: "pending"}}
		}
		message := f.run(t)
		require.Contains(t, message, "Step 1 of 4 (merge the pull request) failed")
		require.Contains(t, message, "still running on head2")
		require.Contains(t, message, "nothing was merged or released")
		require.Empty(t, f.merger.merged)
		require.Empty(t, f.releases.created)
	})
}

// verifiedUpdate makes GitHub's update-branch of from create head: a merge of
// the base's tip into from that changes nothing else. The update moves the
// pull request to head, up to date with its base.
func verifiedUpdate(g *prGitHub, from, head, tip string) {
	if g.parents == nil {
		g.parents, g.onBase, g.changes = map[string][]string{}, map[string]bool{}, map[string][]gh.CompareFile{}
	}
	g.updateHead = head
	g.parents[head] = []string{from, tip}
	g.onBase[tip] = true
	g.changes[tip+"..."+from] = []gh.CompareFile{{Filename: "app.go", Status: "modified", SHA: "blob-a", Patch: "@@ -1,3 +1,4 @@\n package app\n+func Dark() {}\n"}}
	// The base moved the change down a few lines: same change, other hunk.
	g.changes[tip+"..."+head] = []gh.CompareFile{{Filename: "app.go", Status: "modified", SHA: "blob-b", Patch: "@@ -4,3 +4,4 @@\n import \"fmt\"\n+func Dark() {}\n"}}
	g.onUpdate = func() {
		g.status.HeadSHA, g.status.Behind, g.status.MergeableState, g.checksHead = head, 0, "clean", head
		g.status.MergeBase = tip
	}
}

func (f *mergeGateFixture) updateTo(from, head, tip string) {
	verifiedUpdate(f.pr.github, from, head, tip)
}

// pass runs one pass of a merge that waits, as the poller does, without the
// database: it returns what the pass decided.
func (f *mergeGateFixture) pass(t *testing.T, payload domain.MergePayload) mergeWaitPass {
	t.Helper()
	return f.approvals.passMergeWait(context.Background(), repository.ClaimedApproval{WorkspaceID: f.pr.ws, ActorID: f.pr.admin, IssueID: f.pr.issue.ID}, payload)
}

// waiting is the fixture's approval after the first pass updated the branch.
func (f *mergeGateFixture) waiting(t *testing.T) domain.MergePayload {
	t.Helper()
	f.pr.github.status.Behind, f.pr.github.status.MergeableState = 1, "behind"
	f.updateTo("head1", "head2", "base2")
	f.pr.github.checks = []PullRequestCheck{{Name: "Tests", State: "pending"}}
	_, hold, wait, err := f.approvals.mergeApproved(context.Background(), f.pr.ws, f.pr.admin, f.pr.issue.ID, f.payload)
	require.NoError(t, err)
	require.True(t, hold.updated)
	payload := f.payload
	payload.Wait = wait
	return payload
}

// KUA-207: an approved merge whose branch Kuayle brought up to date stays
// alive and merges the updated head once its checks pass, with every gate
// applied again on each pass.
func TestWaitingMergeFinishesOnTheUpdatedHead(t *testing.T) {
	t.Run("green merges the updated head", func(t *testing.T) {
		f := newMergeGateFixture()
		payload := f.waiting(t)
		f.pr.github.checks = passingChecks
		pass := f.pass(t, payload)
		require.True(t, pass.finish)
		require.Empty(t, pass.failure)
		require.Equal(t, f.pr.task, *pass.task)
		require.Equal(t, []string{"acme/app#12@head2"}, f.merger.merged, "the merge is pinned to the verified update")
	})

	t.Run("running checks keep waiting", func(t *testing.T) {
		f := newMergeGateFixture()
		payload := f.waiting(t)
		pass := f.pass(t, payload)
		require.False(t, pass.finish)
		require.Equal(t, mergeWaitPoll, pass.after)
		require.Contains(t, pass.wait.Waiting, "still running on head2")
		require.Zero(t, f.merger.attempts)
	})

	t.Run("failing checks end it with the reason", func(t *testing.T) {
		f := newMergeGateFixture()
		payload := f.waiting(t)
		f.pr.github.checks = []PullRequestCheck{{Name: "Tests", State: "failed"}}
		pass := f.pass(t, payload)
		require.True(t, pass.finish)
		require.Contains(t, pass.failure, "1 check(s) failed on head2: Tests")
		require.Zero(t, f.merger.attempts)
	})

	t.Run("a head someone else pushed invalidates it", func(t *testing.T) {
		f := newMergeGateFixture()
		payload := f.waiting(t)
		f.pr.github.status.HeadSHA, f.pr.github.checksHead = "head9", "head9"
		f.pr.github.checks = passingChecks
		pass := f.pass(t, payload)
		require.True(t, pass.finish)
		require.Contains(t, pass.failure, "head moved to head9")
		require.Zero(t, f.merger.attempts)
	})

	t.Run("behind again is updated again, up to the limit", func(t *testing.T) {
		f := newMergeGateFixture()
		payload := f.waiting(t)
		for round := 2; round <= maxMergeUpdates; round++ {
			from, head, tip := payload.Wait.Head, fmt.Sprintf("head%d", round+1), fmt.Sprintf("base%d", round+1)
			f.pr.github.status.Behind = 1
			f.updateTo(from, head, tip)
			pass := f.pass(t, payload)
			require.False(t, pass.finish, "round %d", round)
			require.Equal(t, head, pass.wait.Head)
			require.Equal(t, round, pass.wait.Updates)
			require.True(t, pass.changed)
			payload.Wait = pass.wait
		}
		f.pr.github.status.Behind = 1
		pass := f.pass(t, payload)
		require.True(t, pass.finish)
		require.Contains(t, pass.failure, "fell behind main again after Kuayle brought it up to date 3 times")
		require.Len(t, f.pr.github.calls, maxMergeUpdates, "no update past the limit")
		require.Zero(t, f.merger.attempts)
	})

	t.Run("expiry ends it with what it waited for", func(t *testing.T) {
		f := newMergeGateFixture()
		payload := f.waiting(t)
		now := gateNow.Add(mergeWaitTimeout)
		f.approvals.ships.now = func() time.Time { return now }
		pass := f.pass(t, payload)
		require.True(t, pass.finish)
		require.Contains(t, pass.failure, "waited 2h0m0s for the checks on head2")
		require.Contains(t, pass.failure, "still running")
		require.Zero(t, f.merger.attempts)
	})

	t.Run("an unreachable GitHub backs off until the deadline", func(t *testing.T) {
		f := newMergeGateFixture()
		payload := f.waiting(t)
		f.pr.github.readErr = errors.New("connection reset")
		pass := f.pass(t, payload)
		require.False(t, pass.finish)
		require.Equal(t, mergeWaitErrorPoll, pass.after)
	})

	t.Run("a pull request merged at the head by an earlier pass is done", func(t *testing.T) {
		f := newMergeGateFixture()
		payload := f.waiting(t)
		f.pr.github.status.State = "merged"
		pass := f.pass(t, payload)
		require.True(t, pass.finish)
		require.Empty(t, pass.failure)
		require.Zero(t, f.merger.attempts)
	})

	t.Run("an approver who lost the role ends it", func(t *testing.T) {
		f := newMergeGateFixture()
		payload := f.waiting(t)
		f.pr.github.checks = passingChecks
		f.approvals.members = prMembers{f.pr.admin: domain.RoleGuest}
		pass := f.pass(t, payload)
		require.True(t, pass.finish)
		require.NotEmpty(t, pass.failure)
		require.Zero(t, f.merger.attempts)
	})

	t.Run("GitHub still creating the update waits, then verifies it", func(t *testing.T) {
		f := newMergeGateFixture()
		f.pr.github.status.Behind = 1
		verifiedUpdate(f.pr.github, "head1", "head2", "base2")
		f.pr.github.updateHead, f.pr.github.onUpdate = "head1", nil // GitHub has not moved the head yet
		_, hold, wait, err := f.approvals.mergeApproved(context.Background(), f.pr.ws, f.pr.admin, f.pr.issue.ID, f.payload)
		require.NoError(t, err)
		require.True(t, hold.updated)
		require.True(t, wait.Updating)
		require.Equal(t, "head1", wait.Head)
		payload := f.payload
		payload.Wait = wait

		pass := f.pass(t, payload)
		require.False(t, pass.finish, "the head has not moved yet")
		require.Contains(t, pass.wait.Waiting, "still bringing pull request #12 up to date")
		require.Len(t, f.pr.github.calls, 1, "no second update while GitHub makes the first")

		f.pr.github.status.HeadSHA, f.pr.github.status.Behind, f.pr.github.checksHead = "head2", 0, "head2"
		f.pr.github.checks = passingChecks
		pass = f.pass(t, payload)
		require.True(t, pass.finish)
		require.Empty(t, pass.failure)
		require.Equal(t, []string{"acme/app#12@head2"}, f.merger.merged)
	})

	t.Run("a new head that is not only the update invalidates it", func(t *testing.T) {
		f := newMergeGateFixture()
		f.pr.github.status.Behind = 1
		verifiedUpdate(f.pr.github, "head1", "head2", "base2")
		// The merge carries an extra edit besides the base's commits.
		f.pr.github.changes["base2...head2"] = append(f.pr.github.changes["base2...head2"], gh.CompareFile{Filename: "secrets.go", Status: "added", SHA: "blob-x", Patch: "@@ -0,0 +1 @@\n+var leak = 1\n"})
		_, hold, _, err := f.approvals.mergeApproved(context.Background(), f.pr.ws, f.pr.admin, f.pr.issue.ID, f.payload)
		require.NoError(t, err)
		require.False(t, hold.updated)
		require.Contains(t, hold.reason, "not only the branch update Kuayle made")
		require.Contains(t, hold.reason, "secrets.go")
		require.Zero(t, f.merger.attempts)
	})
}

// The update check accepts only a merge of the base into the approved head
// that changes nothing else (KUA-207).
func TestVerifyUpdateAcceptsOnlyTheBaseMergedIn(t *testing.T) {
	file := func(name, sha, patch string) gh.CompareFile {
		return gh.CompareFile{Filename: name, Status: "modified", SHA: sha, Patch: patch}
	}
	approved := []gh.CompareFile{file("app.go", "a1", "@@ -1,2 +1,3 @@\n x\n+y\n-z\n"), file("logo.png", "png1", "")}
	cases := []struct {
		name    string
		parents []string
		onBase  bool
		after   []gh.CompareFile
		cut     bool
		want    string
	}{
		{name: "the update", parents: []string{"head1", "tip"}, onBase: true, after: []gh.CompareFile{file("app.go", "a2", "@@ -9,2 +9,3 @@\n w\n+y\n-z\n"), file("logo.png", "png1", "")}},
		{name: "not a merge", parents: []string{"head1"}, onBase: true, after: approved, want: "not a merge of main into head1"},
		{name: "a merge of another commit", parents: []string{"head0", "tip"}, onBase: true, after: approved, want: "not a merge of main"},
		{name: "merges in a commit not on the base", parents: []string{"head1", "tip"}, onBase: false, after: approved, want: "is not on main"},
		{name: "an extra changed line", parents: []string{"head1", "tip"}, onBase: true, after: []gh.CompareFile{file("app.go", "a2", "@@ -1,2 +1,4 @@\n x\n+y\n+evil()\n-z\n"), file("logo.png", "png1", "")}, want: "changes app.go differently"},
		{name: "a binary file changed", parents: []string{"head1", "tip"}, onBase: true, after: []gh.CompareFile{file("app.go", "a1", "@@ -1,2 +1,3 @@\n x\n+y\n-z\n"), file("logo.png", "png2", "")}, want: "changes logo.png differently"},
		{name: "a base change reverted", parents: []string{"head1", "tip"}, onBase: true, after: append([]gh.CompareFile{file("base.go", "b0", "@@ -1 +1 @@\n-new\n+old\n")}, approved...), want: "changes base.go differently"},
		{name: "a comparison cut short", parents: []string{"head1", "tip"}, onBase: true, after: approved, cut: true, want: "too many files"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			github := &prGitHub{parents: map[string][]string{"head2": c.parents}, onBase: map[string]bool{"tip": c.onBase},
				changes: map[string][]gh.CompareFile{"tip...head1": approved, "tip...head2": c.after}, cutShort: map[string]bool{"tip...head2": c.cut}}
			why, err := mergeGate{pulls: github, now: time.Now}.verifyUpdate(context.Background(), uuid.New(), "acme/app", "main", "head1", "head2")
			require.NoError(t, err)
			if c.want == "" {
				require.Empty(t, why)
				return
			}
			require.Contains(t, why, c.want)
		})
	}
}
