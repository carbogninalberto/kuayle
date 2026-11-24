package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// factsGitHub is the pull request fake with reads that fail on their own:
// GitHub answering the pull request but not its checks, or not the checks
// of the base's tip.
type factsGitHub struct {
	*prGitHub
	checksErr, baseChecksErr error
}

func (f factsGitHub) PullRequestChecks(ctx context.Context, ws uuid.UUID, repository string, number int) (string, []PullRequestCheck, ChecksCoverage, error) {
	if f.checksErr != nil {
		return "", nil, ChecksCoverage{}, f.checksErr
	}
	return f.prGitHub.PullRequestChecks(ctx, ws, repository, number)
}

func (f factsGitHub) CommitChecks(ctx context.Context, ws uuid.UUID, repository, sha string) ([]PullRequestCheck, ChecksCoverage, error) {
	if f.baseChecksErr != nil {
		return nil, ChecksCoverage{}, f.baseChecksErr
	}
	return f.prGitHub.CommitChecks(ctx, ws, repository, sha)
}

// factRow is the yes/no facts of a read, in one comparable value.
type factRow struct {
	exists, draft, merged, closed, contains, conflict, collision, green Fact
	ci                                                                  string
}

func rowOf(f TaskFacts) factRow {
	return factRow{f.PRExists, f.PRDraft, f.Merged, f.Closed, f.HeadContainsBase, f.Conflict, f.MigrationCollision, f.CI.Green, f.CI.State}
}

func TestReadTaskFactsFromThePullRequest(t *testing.T) {
	yes, no := true, false
	const (
		Y = FactYes
		N = FactNo
		U = FactUnknown
	)
	cases := []struct {
		name   string
		number int
		setup  func(*factsGitHub)
		want   factRow
		check  func(*testing.T, TaskFacts)
	}{
		{name: "an up-to-date mergeable head whose checks passed", want: factRow{Y, N, N, N, Y, N, N, Y, "passing"},
			check: func(t *testing.T, f TaskFacts) {
				require.Equal(t, "head1", f.HeadSHA)
				require.Equal(t, "main", f.BaseRef)
				require.Zero(t, f.Behind)
				require.Equal(t, U, f.Approved, "approvals are not read from GitHub")
				require.Nil(t, f.Verdicts, "verdicts are not read from GitHub")
			}},
		{name: "a draft", setup: func(g *factsGitHub) { g.status.Draft = true }, want: factRow{Y, Y, N, N, Y, N, N, Y, "passing"}},
		{name: "behind its base", setup: func(g *factsGitHub) { g.status.Behind, g.status.MergeableState = 3, "behind" },
			want: factRow{Y, N, N, N, N, N, N, Y, "passing"}, check: func(t *testing.T, f TaskFacts) { require.Equal(t, 3, f.Behind) }},
		{name: "not compared with its base is unknown, not behind", setup: func(g *factsGitHub) { g.status.Behind = -1 },
			want: factRow{Y, N, N, N, U, N, N, Y, "passing"}},
		{name: "conflicting with its base", setup: func(g *factsGitHub) {
			g.status.Behind, g.status.Mergeable, g.status.MergeableState = 2, &no, "dirty"
		}, want: factRow{Y, N, N, N, N, Y, N, Y, "passing"}},
		{name: "mergeability GitHub has not computed yet is unknown", setup: func(g *factsGitHub) {
			g.status.Behind, g.status.Mergeable, g.status.MergeableState = 2, nil, "unknown"
		}, want: factRow{Y, N, N, N, N, U, N, Y, "passing"}},
		{name: "a head that contains its base cannot conflict", setup: func(g *factsGitHub) {
			g.status.Mergeable, g.status.MergeableState = nil, "unknown"
		}, want: factRow{Y, N, N, N, Y, N, N, Y, "passing"}},
		{name: "a migration that collides with the base", setup: func(g *factsGitHub) { g.status.migrationRepair = "Renumber migration 000201." },
			want:  factRow{Y, N, N, N, Y, N, Y, Y, "passing"},
			check: func(t *testing.T, f TaskFacts) { require.Equal(t, "Renumber migration 000201.", f.MigrationRepair) }},
		{name: "a migration check GitHub could not answer is unknown", setup: func(g *factsGitHub) { g.status.migrationCheckUnavailable = true },
			want: factRow{Y, N, N, N, Y, N, U, Y, "passing"}},
		{name: "merged", setup: func(g *factsGitHub) {
			g.status.State, g.status.MergeCommit, g.status.Mergeable, g.status.Behind = "merged", "merge1", nil, -1
		}, want: factRow{Y, N, Y, N, U, U, U, U, ""},
			check: func(t *testing.T, f TaskFacts) {
				require.Equal(t, "merge1", f.MergeCommit)
				require.Equal(t, -1, f.Behind)
			}},
		{name: "closed without merging", setup: func(g *factsGitHub) { g.status.State = "closed" }, want: factRow{Y, N, N, Y, U, U, U, U, ""}},
		{name: "a pull request GitHub could not read is unknown", setup: func(g *factsGitHub) { g.readErr = errors.New("github: 502") },
			want:  factRow{U, U, U, U, U, U, U, U, ""},
			check: func(t *testing.T, f TaskFacts) { require.EqualError(t, f.Err, "github: 502") }},
		{name: "no pull request", number: -1, want: factRow{N, U, U, U, U, U, U, U, ""},
			check: func(t *testing.T, f TaskFacts) { require.NoError(t, f.Err) }},
		{name: "a failed check", setup: func(g *factsGitHub) {
			g.checks = []PullRequestCheck{{Name: "Tests", State: "failed", URL: "https://ci/1", Summary: "2 tests failed"}, {Name: "Lint", State: "passed"}}
		}, want: factRow{Y, N, N, N, Y, N, N, N, "failing"},
			check: func(t *testing.T, f TaskFacts) {
				require.Equal(t, []PullRequestCheck{{Name: "Tests", State: "failed", URL: "https://ci/1", Summary: "2 tests failed"}}, f.CI.failing())
			}},
		{name: "checks still running", setup: func(g *factsGitHub) {
			g.checks = []PullRequestCheck{{Name: "Tests", State: "pending"}, {Name: "Lint", State: "passed"}}
		}, want: factRow{Y, N, N, N, Y, N, N, N, "pending"}},
		{name: "checks GitHub could not read are unknown", setup: func(g *factsGitHub) { g.checksErr = errors.New("checks: 502") },
			want:  factRow{Y, N, N, N, Y, N, N, U, ""},
			check: func(t *testing.T, f TaskFacts) { require.EqualError(t, f.CI.Err, "checks: 502") }},
		{name: "a check list cut short is unknown", setup: func(g *factsGitHub) { g.coverage.Truncated = true },
			want: factRow{Y, N, N, N, Y, N, N, U, "incomplete"}},
		{name: "checks read for another head are unknown", setup: func(g *factsGitHub) { g.checksHead = "head2" },
			want:  factRow{Y, N, N, N, Y, N, N, U, "passing"},
			check: func(t *testing.T, f TaskFacts) { require.Equal(t, "head2", f.CI.Head) }},
		{name: "no checks anywhere is a repository without CI", setup: func(g *factsGitHub) { g.checks = nil },
			want:  factRow{Y, N, N, N, Y, N, N, Y, "none"},
			check: func(t *testing.T, f TaskFacts) { require.Empty(t, f.CI.Unchecked) }},
		{name: "no checks on the head while the base tip has some", setup: func(g *factsGitHub) {
			g.checks, g.commitChecks = nil, map[string][]PullRequestCheck{"base1": passingChecks}
		}, want: factRow{Y, N, N, N, Y, N, N, N, "none"},
			check: func(t *testing.T, f TaskFacts) {
				require.Equal(t, "though main's tip has 2, so CI has not run on it", f.CI.Unchecked)
			}},
		{name: "no checks on a head committed moments ago", setup: func(g *factsGitHub) {
			g.checks, g.committed = nil, map[string]time.Time{"head1": gateNow.Add(-30 * time.Second)}
		}, want: factRow{Y, N, N, N, Y, N, N, N, "none"},
			check: func(t *testing.T, f TaskFacts) {
				require.Equal(t, "and it was committed less than 2m0s ago, so CI may not have started", f.CI.Unchecked)
			}},
		{name: "no checks and an unknown base tip", setup: func(g *factsGitHub) { g.checks, g.status.MergeBase = nil, "" },
			want: factRow{Y, N, N, N, Y, N, N, N, "none"},
			check: func(t *testing.T, f TaskFacts) {
				require.Equal(t, "and the tip of main it contains is unknown, so it is not known whether the repository has CI", f.CI.Unchecked)
			}},
		{name: "no checks and base tip checks GitHub could not read are unknown", setup: func(g *factsGitHub) {
			g.checks, g.baseChecksErr = nil, errors.New("statuses: 502")
		}, want: factRow{Y, N, N, N, Y, N, N, U, "none"},
			check: func(t *testing.T, f TaskFacts) { require.EqualError(t, f.CI.Err, "statuses: 502") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			github := &factsGitHub{prGitHub: &prGitHub{
				status: PullRequestStatus{State: "open", HeadSHA: "head1", BaseRef: "main", MergeBase: "base1", Mergeable: &yes, MergeableState: "clean"},
				checks: passingChecks,
			}}
			if tc.setup != nil {
				tc.setup(github)
			}
			number := 12
			if tc.number != 0 {
				number = tc.number
			}
			facts := readTaskFacts(context.Background(), github, func() time.Time { return gateNow }, uuid.New(), "acme/app", number)
			require.Equal(t, tc.want, rowOf(facts))
			if tc.check != nil {
				tc.check(t, facts)
			}
		})
	}

	t.Run("without a GitHub reader every fact is unknown", func(t *testing.T) {
		facts := readTaskFacts(context.Background(), nil, time.Now, uuid.New(), "acme/app", 12)
		require.Equal(t, factRow{U, U, U, U, U, U, U, U, ""}, rowOf(facts))
		require.Error(t, facts.Err)
	})
}

// readyFacts is a pull request every check holds for.
func readyFacts() TaskFacts {
	const head = "abcdef1234567890"
	return TaskFacts{
		Repository: "acme/app", PullRequest: 12, PRExists: FactYes, PRDraft: FactNo, Merged: FactNo, Closed: FactNo,
		HeadSHA: head, BaseRef: "main", HeadContainsBase: FactYes, Conflict: FactNo, MigrationCollision: FactNo,
		CI:       CIFacts{Green: FactYes, State: "passing", Head: head, Checks: passingChecks},
		Verdicts: map[string]TaskVerdict{"reviewer": {Commit: head, Verdict: "approve"}},
		Approved: FactYes,
	}
}

func TestUnmetChecksSayWhatToDo(t *testing.T) {
	every := []FactCheck{
		{ID: "pr", Fact: "pr_exists"}, {ID: "ready", Fact: "pr_draft"}, {ID: "up-to-date", Fact: "head_contains_base"},
		{ID: "clean", Fact: "conflict"}, {ID: "migrations", Fact: "migration_collision"}, {ID: "ci", Fact: "ci_green"},
		{ID: "review", Fact: "verdict", Role: "reviewer"}, {ID: "owner", Fact: "approved"}, {ID: "open", Fact: "closed"},
	}
	t.Run("a ready pull request meets every check", func(t *testing.T) {
		require.Empty(t, unmetChecks(readyFacts(), every))
	})

	cases := []struct {
		name   string
		check  FactCheck
		change func(*TaskFacts)
		state  string
		reason string
	}{
		{name: "no pull request", check: FactCheck{ID: "pr", Fact: "pr_exists"},
			change: func(f *TaskFacts) { f.PRExists = FactNo },
			state:  CheckUnmet, reason: "No pull request is published for this task yet. Next step: commit the work so Kuayle publishes it."},
		{name: "no pull request for a check that needs one", check: FactCheck{ID: "ci", Fact: "ci_green"},
			change: func(f *TaskFacts) { f.PRExists, f.CI = FactNo, CIFacts{Green: FactUnknown} },
			state:  CheckUnmet, reason: "No pull request is published for this task yet. Next step: commit the work so Kuayle publishes it."},
		{name: "a pull request GitHub could not read", check: FactCheck{ID: "up-to-date", Fact: "head_contains_base"},
			change: func(f *TaskFacts) { f.PRExists, f.HeadContainsBase = FactUnknown, FactUnknown },
			state:  CheckUnknown, reason: "Kuayle could not read pull request #12 from GitHub. It reads it again shortly."},
		{name: "a draft", check: FactCheck{ID: "ready", Fact: "pr_draft"},
			change: func(f *TaskFacts) { f.PRDraft = FactYes },
			state:  CheckUnmet, reason: "Pull request #12 is still a draft. Next step: mark it ready for review."},
		{name: "behind its base", check: FactCheck{ID: "up-to-date", Fact: "head_contains_base"},
			change: func(f *TaskFacts) { f.HeadContainsBase, f.Behind = FactNo, 3 },
			state:  CheckUnmet, reason: "Pull request #12 is 3 commit(s) behind main, so its CI has not run with main's latest commits. Next step: bring the branch up to date with main (GitHub's update-branch, when it does not conflict), then wait for CI on the new head."},
		{name: "not compared with its base (Behind -1)", check: FactCheck{ID: "up-to-date", Fact: "head_contains_base"},
			change: func(f *TaskFacts) { f.HeadContainsBase, f.Behind = FactUnknown, -1 },
			state:  CheckUnknown, reason: "GitHub did not report whether pull request #12 contains the latest main. Kuayle reads it again shortly."},
		{name: "conflicting", check: FactCheck{ID: "clean", Fact: "conflict"},
			change: func(f *TaskFacts) { f.Conflict = FactYes },
			state:  CheckUnmet, reason: "Pull request #12 conflicts with main. Next step: merge main into the branch, resolve the conflicts and push."},
		{name: "mergeability not computed", check: FactCheck{ID: "clean", Fact: "conflict"},
			change: func(f *TaskFacts) { f.Conflict = FactUnknown },
			state:  CheckUnknown, reason: "GitHub has not yet said whether pull request #12 conflicts with main. Kuayle reads it again shortly."},
		{name: "a colliding migration", check: FactCheck{ID: "migrations", Fact: "migration_collision"},
			change: func(f *TaskFacts) { f.MigrationCollision, f.MigrationRepair = FactYes, "Renumber migration 000201." },
			state:  CheckUnmet, reason: "Renumber migration 000201."},
		{name: "a migration check GitHub could not answer", check: FactCheck{ID: "migrations", Fact: "migration_collision"},
			change: func(f *TaskFacts) { f.MigrationCollision = FactUnknown },
			state:  CheckUnknown, reason: "Kuayle could not check the pull request for migration number collisions. It checks again shortly."},
		{name: "failed checks", check: FactCheck{ID: "ci", Fact: "ci_green"},
			change: func(f *TaskFacts) {
				f.CI.Green, f.CI.State = FactNo, "failing"
				f.CI.Checks = []PullRequestCheck{{Name: "Tests", State: "failed"}, {Name: "Lint", State: "passed"}, {Name: "Security", State: "failed"}}
			},
			state: CheckUnmet, reason: "2 check(s) failed on abcdef12: Tests, Security. Next step: read their output, fix the cause and push; CI runs again on the new head."},
		{name: "checks still running", check: FactCheck{ID: "ci", Fact: "ci_green"},
			change: func(f *TaskFacts) {
				f.CI.Green, f.CI.State = FactNo, "pending"
				f.CI.Checks = []PullRequestCheck{{Name: "Tests", State: "pending"}, {Name: "Lint", State: "passed"}}
			},
			state: CheckWaiting, reason: "1 of 2 check(s) are still running on abcdef12. Kuayle waits for them."},
		{name: "no checks yet", check: FactCheck{ID: "ci", Fact: "ci_green"},
			change: func(f *TaskFacts) {
				f.CI = CIFacts{Green: FactNo, State: "none", Head: f.HeadSHA, Unchecked: "though main's tip has 2, so CI has not run on it"}
			},
			state: CheckWaiting, reason: "No checks have reported on abcdef12 yet, though main's tip has 2, so CI has not run on it. Kuayle waits for them."},
		{name: "checks GitHub could not read", check: FactCheck{ID: "ci", Fact: "ci_green"},
			change: func(f *TaskFacts) { f.CI = CIFacts{Green: FactUnknown, Err: errors.New("checks: 502")} },
			state:  CheckUnknown, reason: "Kuayle could not read the checks of pull request #12 from GitHub. It reads them again shortly."},
		{name: "a check list cut short", check: FactCheck{ID: "ci", Fact: "ci_green"},
			change: func(f *TaskFacts) { f.CI.Green, f.CI.State = FactUnknown, "incomplete" },
			state:  CheckUnknown, reason: "Not every check on abcdef12 could be read, so it is not known that CI passed. Kuayle reads them again shortly."},
		{name: "checks of another head", check: FactCheck{ID: "ci", Fact: "ci_green"},
			change: func(f *TaskFacts) { f.CI.Green, f.CI.Head = FactUnknown, "0123456789abcdef" },
			state:  CheckUnknown, reason: "The head of pull request #12 moved while Kuayle read its checks. It reads them again shortly."},
		{name: "no verdict from the role", check: FactCheck{ID: "security", Fact: "verdict", Role: "security"},
			state: CheckUnmet, reason: "security has not reviewed abcdef12 yet. Next step: ask security to review it."},
		{name: "a verdict for an earlier commit", check: FactCheck{ID: "review", Fact: "verdict", Role: "reviewer"},
			change: func(f *TaskFacts) {
				f.Verdicts["reviewer"] = TaskVerdict{Commit: "0123456789abcdef", Verdict: "approve"}
			},
			state: CheckUnmet, reason: "reviewer's verdict is for 01234567, not the head abcdef12. Next step: ask reviewer to review abcdef12."},
		{name: "changes requested on the head", check: FactCheck{ID: "review", Fact: "verdict", Role: "reviewer"},
			change: func(f *TaskFacts) {
				f.Verdicts["reviewer"] = TaskVerdict{Commit: f.HeadSHA, Verdict: "request_changes"}
			},
			state: CheckUnmet, reason: "reviewer did not approve abcdef12 (request_changes). Next step: address reviewer's findings, push, and ask reviewer to review again."},
		{name: "verdicts not read", check: FactCheck{ID: "review", Fact: "verdict", Role: "reviewer"},
			change: func(f *TaskFacts) { f.Verdicts = nil },
			state:  CheckUnknown, reason: "Kuayle has not read the review verdicts. It reads them again shortly."},
		{name: "a verdict check without a role", check: FactCheck{ID: "review", Fact: "verdict"},
			state: CheckUnmet, reason: "Check review names no reviewing role. Next step: fix the workflow definition."},
		{name: "not approved", check: FactCheck{ID: "owner", Fact: "approved"},
			change: func(f *TaskFacts) { f.Approved = FactNo },
			state:  CheckUnmet, reason: "Nobody has approved merging abcdef12 yet. Next step: ask the owner to approve it."},
		{name: "approvals not read", check: FactCheck{ID: "owner", Fact: "approved"},
			change: func(f *TaskFacts) { f.Approved = FactUnknown },
			state:  CheckUnknown, reason: "Kuayle could not read the approvals. It reads them again shortly."},
		{name: "not merged yet", check: FactCheck{ID: "merged", Fact: "merged"},
			state: CheckUnmet, reason: "Pull request #12 is not merged yet."},
		{name: "closed without merging", check: FactCheck{ID: "open", Fact: "closed"},
			change: func(f *TaskFacts) { f.Closed = FactYes },
			state:  CheckUnmet, reason: "Pull request #12 was closed without merging. Next step: reopen it on GitHub, or publish the work again."},
		{name: "a check that needs an open pull request on a closed one", check: FactCheck{ID: "ci", Fact: "ci_green"},
			change: func(f *TaskFacts) { f.Closed, f.CI = FactYes, CIFacts{Green: FactUnknown} },
			state:  CheckUnmet, reason: "Pull request #12 was closed without merging. Next step: reopen it on GitHub, or publish the work again."},
		{name: "a check that needs an open pull request on a merged one", check: FactCheck{ID: "up-to-date", Fact: "head_contains_base"},
			change: func(f *TaskFacts) { f.Merged, f.HeadContainsBase = FactYes, FactUnknown },
			state:  CheckUnmet, reason: "Pull request #12 is already merged."},
		{name: "a fact Kuayle does not know", check: FactCheck{ID: "lint", Fact: "linted"},
			state: CheckUnmet, reason: "Kuayle does not know the fact \"linted\". Next step: fix the workflow definition."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			facts := readyFacts()
			if tc.change != nil {
				tc.change(&facts)
			}
			require.Equal(t, []UnmetCheck{{FactCheck: tc.check, State: tc.state, Reason: tc.reason}}, unmetChecks(facts, []FactCheck{tc.check}))
		})
	}

	t.Run("only the checks that do not hold, in order", func(t *testing.T) {
		facts := readyFacts()
		facts.HeadContainsBase, facts.Behind, facts.Approved = FactNo, 1, FactNo
		var ids []string
		for _, unmet := range unmetChecks(facts, every) {
			ids = append(ids, unmet.ID)
		}
		require.Equal(t, []string{"up-to-date", "owner"}, ids)
	})

	// Only the reader's explicit values hold; a fact left at its zero value
	// was never read.
	t.Run("a fact never read never holds", func(t *testing.T) {
		facts := TaskFacts{PullRequest: 12, PRExists: FactYes, HeadSHA: "abcdef1234567890", BaseRef: "main"}
		unmet := unmetChecks(facts, every)
		require.Len(t, unmet, len(every)-1, "only pr_exists holds")
		for _, check := range unmet {
			require.Equal(t, CheckUnknown, check.State, check.ID)
		}
	})

	t.Run("a merged pull request meets the merged check", func(t *testing.T) {
		facts := readyFacts()
		facts.Merged = FactYes
		require.Empty(t, unmetChecks(facts, []FactCheck{{ID: "merged", Fact: "merged"}}))
	})
}
