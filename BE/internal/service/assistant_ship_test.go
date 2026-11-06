package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	gh "github.com/kuayle/kuayle-backend/pkg/github"
	"github.com/stretchr/testify/require"
)

// shipReleases is the GitHub side of a release: the merge commit's checks and
// the release's workflow runs change from one read to the next.
type shipReleases struct {
	*releaseFake
	checkReads [][]PullRequestCheck
	runReads   [][]gh.WorkflowRun
	checksErr  error
}

func (f *shipReleases) CommitChecks(ctx context.Context, ws uuid.UUID, repository, sha string) ([]PullRequestCheck, ChecksCoverage, error) {
	if f.checksErr != nil {
		return nil, ChecksCoverage{}, f.checksErr
	}
	if len(f.checkReads) > 0 {
		f.checks, f.checkReads = f.checkReads[0], f.checkReads[1:]
	}
	return f.releaseFake.CommitChecks(ctx, ws, repository, sha)
}

func (f *shipReleases) WorkflowRuns(ctx context.Context, ws uuid.UUID, repository, sha string) ([]gh.WorkflowRun, error) {
	if len(f.runReads) > 0 {
		f.runs, f.runReads = f.runReads[0], f.runReads[1:]
	}
	return f.releaseFake.WorkflowRuns(ctx, ws, repository, sha)
}

// shipMerger merges on the fake GitHub: the pull request becomes merged with
// the merge commit the release is then tagged on.
type shipMerger struct {
	github  *prGitHub
	merged  []string
	err     error
	onMerge func()
}

func (m *shipMerger) MergeTaskPullRequest(_ context.Context, _ uuid.UUID, repository string, number int, sha, method string) error {
	m.merged = append(m.merged, fmt.Sprintf("%s#%d@%s/%s", repository, number, sha, method))
	if m.err != nil {
		return m.err
	}
	m.github.status.State, m.github.status.MergeCommit = "merged", releaseSHA
	if m.onMerge != nil {
		m.onMerge()
	}
	return nil
}

type shipHeads struct{ head string }

func (h shipHeads) InspectPullRequestHead(context.Context, uuid.UUID, string, int, string) (*domain.PullRequestHead, error) {
	return &domain.PullRequestHead{SHA: h.head}, nil
}
func (h shipHeads) ReadPullRequestHead(context.Context, uuid.UUID, string, int) (string, error) {
	return h.head, nil
}

type shipSwitches map[string]bool

func (s shipSwitches) Disabled(context.Context, uuid.UUID) (map[string]bool, error) { return s, nil }

type shipFixture struct {
	approvals *AssistantApprovals
	github    *prGitHub
	releases  *shipReleases
	merger    *shipMerger
	updater   *updaterFake
	members   prMembers
	listed    []instanceRelease
	instance  map[uuid.UUID]bool
	ws, admin uuid.UUID
	approval  *domain.ConversationApproval
	payload   domain.ShipPayload
	saved     []domain.ShipPayload
	clock     time.Time
	sleeps    int
}

var passingChecks = []PullRequestCheck{{Name: "Tests", State: "passed"}, {Name: "Security", State: "passed"}}

func newShipFixture(upgrade bool) *shipFixture {
	pr := newPRFixture()
	f := &shipFixture{github: pr.github, ws: pr.ws, admin: pr.admin, clock: time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)}
	f.members = prMembers{pr.creator: domain.RoleMember, pr.admin: domain.RoleAdmin, pr.other: domain.RoleMember}
	f.releases = &shipReleases{releaseFake: newReleaseFake()}
	f.releases.runs = []gh.WorkflowRun{{ID: 9, Name: "Build & Push", Event: "release", Status: "completed", Conclusion: "success", HTMLURL: "https://github.com/acme/app/actions/runs/9"}}
	f.merger = &shipMerger{github: f.github}
	f.updater = &updaterFake{configured: true}
	f.instance = map[uuid.UUID]bool{pr.admin: true}
	system := NewAssistantSystem(f.updater, func(id uuid.UUID) bool { return f.instance[id] }, "v1.2.0")
	f.listed = []instanceRelease{{Tag: "v1.2.0"}, {Tag: "v1.3.0"}}
	system.releases = func(context.Context) ([]instanceRelease, error) { return f.listed, nil }

	f.approvals = NewAssistantApprovals(nil, nil)
	target := &repository.MergeTarget{TaskID: pr.task, IssueID: pr.issue.ID, CreatorID: pr.creator, Repository: "acme/app", Number: 12, URL: "https://github.com/acme/app/pull/12", Commit: "head1"}
	f.approvals.SetMerging(prWork{merge: target}, f.members, f.merger)
	f.approvals.SetPullRequests(f.github, pr.targets)
	f.approvals.SetReleases(f.releases)
	f.approvals.SetSystem(system)
	f.approvals.ships.now = func() time.Time { return f.clock }
	f.approvals.ships.sleep = func(_ context.Context, d time.Duration) error {
		f.sleeps++
		f.clock = f.clock.Add(d)
		return nil
	}

	f.payload = domain.ShipPayload{
		Identifier: "ENG-1", Title: "Dark mode", Repository: "acme/app", PullRequest: 12,
		Merge:   domain.MergePayload{Identifier: "ENG-1", TaskID: pr.task, Repository: "acme/app", PullRequest: 12, Commit: "head1", MergeMethod: "squash"},
		Release: domain.ReleasePayload{Repository: "acme/app", Tag: "v1.3.0", Name: "v1.3.0", Branch: "main", PreviousTag: "v1.2.0", Notes: "### Features\n- **Dark mode (ENG-1).** [#12](https://github.com/acme/app/pull/12)", ReleaseURL: "https://github.com/acme/app/releases/tag/v1.3.0"},
		Steps:   shipSteps(upgrade),
	}
	if upgrade {
		f.payload.Upgrade = &domain.UpgradePayload{CurrentVersion: "1.2.0", LatestVersion: "1.3.0"}
	}
	f.approval = &domain.ConversationApproval{ID: uuid.New(), IssueID: pr.issue.ID, ActorID: pr.admin, Action: shipAction}
	return f
}

func (f *shipFixture) run(t *testing.T) string {
	t.Helper()
	data, err := json.Marshal(f.payload)
	require.NoError(t, err)
	f.approval.Payload = data
	chain := &shipChain{ws: f.ws, approval: f.approval, payload: f.payload, save: func(progress domain.ShipPayload) error {
		progress.Steps = append([]domain.ShipStep(nil), progress.Steps...)
		f.saved = append(f.saved, progress)
		return nil
	}}
	message, err := f.approvals.runShip(context.Background(), chain)
	require.NoError(t, err)
	f.payload = chain.payload
	return message
}

func (f *shipFixture) statuses() []string {
	statuses := make([]string, len(f.payload.Steps))
	for i, step := range f.payload.Steps {
		statuses[i] = step.Step + ":" + step.Status
	}
	return statuses
}

// One approval merges, waits for the tests, publishes the release with the
// approved notes at the merge commit, waits for its build and upgrades, and
// every step is recorded with its own tool's audit event.
func TestShipMergesWaitsForTestsReleasesAndUpgradesInOrder(t *testing.T) {
	f := newShipFixture(true)
	f.releases.checkReads = [][]PullRequestCheck{nil, {{Name: "Tests", State: "pending"}}, passingChecks}
	running := gh.WorkflowRun{ID: 9, Name: "Build & Push", Event: "release", Status: "in_progress", HTMLURL: "https://github.com/acme/app/actions/runs/9"}
	tests := gh.WorkflowRun{ID: 8, Name: "Tests", Event: "push", Status: "completed", Conclusion: "failure"}
	f.releases.runReads = [][]gh.WorkflowRun{{tests}, {tests, running}, {tests, f.releases.runs[0]}}

	var message string
	entries := auditEvents(t, func() { message = f.run(t) })
	require.Empty(t, message, "every step succeeded")
	require.Equal(t, []string{"merge:done", "tests:done", "release:done", "build:done", "upgrade:done"}, f.statuses())
	require.Equal(t, []string{"acme/app#12@head1/squash"}, f.merger.merged, "merged once, at the approved commit, with the repository's method")
	require.Equal(t, releaseSHA, f.payload.MergeCommit)
	require.Len(t, f.releases.created, 1)
	require.Equal(t, releaseSHA, f.releases.created[0].TargetCommitish, "the release is tagged at the merge commit")
	require.Equal(t, f.payload.Release.Notes, f.releases.created[0].Body, "the approved notes are published")
	require.Equal(t, 1, f.updater.started)
	require.GreaterOrEqual(t, f.sleeps, 4, "the chain waited for the checks and the build")
	require.Equal(t, "https://github.com/acme/app/releases/tag/v1.3.0", f.payload.Steps[2].URL)
	require.Equal(t, "https://github.com/acme/app/actions/runs/9", f.payload.Steps[3].URL)
	require.Contains(t, f.payload.Steps[1].Detail, "2 check(s) passed on main")

	// Progress is saved as each step starts and ends, so the card follows it.
	require.Equal(t, "running", f.saved[0].Steps[0].Status)
	require.Equal(t, "pending", f.saved[0].Steps[1].Status)

	executed := []string{}
	for _, entry := range entries {
		if entry.Data["action"] == "assistant.action_executed" {
			require.Equal(t, shipAction, entry.Data["chain"])
			require.Equal(t, f.approval.ID.String(), entry.Data["approval_id"])
			executed = append(executed, entry.Data["tool"].(string))
		}
	}
	require.Equal(t, []string{"merge_pull_request", "get_pull_request_checks", "create_release", "get_workflow_run", "upgrade_instance"}, executed, "each step is audited as its own tool")
}

// A failing step stops the chain: it says which step failed, why and the next
// step, and nothing after it runs.
func TestShipStopsAtTheFirstFailingStepAndRunsNothingAfterIt(t *testing.T) {
	t.Run("failing tests", func(t *testing.T) {
		f := newShipFixture(true)
		f.releases.checks = []PullRequestCheck{{Name: "Tests", State: "failed"}, {Name: "Security", State: "passed"}}
		var message string
		entries := auditEvents(t, func() { message = f.run(t) })
		require.Contains(t, message, "Step 2 of 5 (wait for the tests) failed")
		require.Contains(t, message, "1 check(s) failed on main at 01234567: Tests")
		require.Contains(t, message, "Next step: pull request #12 is merged as 01234567 but nothing was released")
		require.Contains(t, message, "Not run: publish the release, wait for the release build, upgrade the instance.")
		require.Equal(t, []string{"merge:done", "tests:failed", "release:skipped", "build:skipped", "upgrade:skipped"}, f.statuses())
		require.Empty(t, f.releases.created, "nothing is released")
		require.Zero(t, f.updater.started, "nothing is upgraded")
		failed := 0
		for _, entry := range entries {
			if entry.Data["action"] == "assistant.action_failed" {
				failed++
				require.Equal(t, "get_pull_request_checks", entry.Data["tool"])
			}
		}
		require.Equal(t, 1, failed)
	})

	t.Run("merge refused by GitHub", func(t *testing.T) {
		f := newShipFixture(false)
		f.merger.err = &gh.ErrMergeRefused{Reason: "Required status check \"Tests\" is failing"}
		message := f.run(t)
		require.Contains(t, message, "Step 1 of 4 (merge the pull request) failed")
		require.Contains(t, message, "nothing was merged or released")
		require.Equal(t, []string{"merge:failed", "tests:skipped", "release:skipped", "build:skipped"}, f.statuses())
		require.Empty(t, f.releases.created)
	})

	t.Run("head moved after approval", func(t *testing.T) {
		f := newShipFixture(false)
		f.approvals.SetPullRequestHeads(shipHeads{head: "head2"})
		message := f.run(t)
		require.Contains(t, message, "head moved to head2")
		require.Empty(t, f.merger.merged, "a commit the user did not see is never merged")
	})

	t.Run("checks never start", func(t *testing.T) {
		f := newShipFixture(false)
		f.releases.checks = nil
		message := f.run(t)
		require.Contains(t, message, "GitHub reported no checks on main at 01234567 after 1h0m0s")
		require.Empty(t, f.releases.created)
	})

	t.Run("release gate refuses", func(t *testing.T) {
		f := newShipFixture(false)
		f.releases.rng.Files = append(f.releases.rng.Files, gh.CompareFile{Filename: "deploy.env", Status: "added", Patch: "@@ -0,0 +1 @@\n+AWS_SECRET_ACCESS_KEY=" + strings.Repeat("a", 40)})
		message := f.run(t)
		require.Contains(t, message, "Step 3 of 4 (publish the release) failed: Tagging refused")
		require.Contains(t, message, "publish with create_release at target_commit "+releaseSHA)
		require.Empty(t, f.releases.created)
		require.Equal(t, []string{"merge:done", "tests:done", "release:failed", "build:skipped"}, f.statuses())
	})

	t.Run("release build fails", func(t *testing.T) {
		f := newShipFixture(true)
		f.releases.runs = []gh.WorkflowRun{{ID: 9, Name: "Build & Push", Event: "release", Status: "completed", Conclusion: "failure", HTMLURL: "https://github.com/acme/app/actions/runs/9"}}
		message := f.run(t)
		require.Contains(t, message, "Step 4 of 5 (wait for the release build) failed: The release build \"Build & Push\" failed (failure)")
		require.Contains(t, message, "upgrade with upgrade_instance once it succeeds")
		require.Equal(t, "https://github.com/acme/app/actions/runs/9", f.payload.Steps[3].URL, "the card links the failing run")
		require.Zero(t, f.updater.started)
	})

	t.Run("updater refuses", func(t *testing.T) {
		f := newShipFixture(true)
		f.updater.startCode = 409
		message := f.run(t)
		require.Contains(t, message, "Step 5 of 5 (upgrade the instance) failed: An upgrade is already running")
		require.NotContains(t, message, "Not run:")
		require.Len(t, f.releases.created, 1)
	})

	t.Run("release never listed for the instance", func(t *testing.T) {
		f := newShipFixture(true)
		f.listed = f.listed[:1]
		message := f.run(t)
		require.Contains(t, message, "did not show 1.3.0 within 20m0s")
		require.Zero(t, f.updater.started)
	})

	t.Run("GitHub unreachable", func(t *testing.T) {
		f := newShipFixture(false)
		f.releases.checksErr = errors.New("dial tcp: i/o timeout")
		message := f.run(t)
		require.Contains(t, message, "Step 2 of 4 (wait for the tests) failed: GitHub or the database could not be reached")
		require.NotContains(t, message, "dial tcp", "internal errors stay out of the conversation")
		require.Equal(t, 2, f.sleeps, "the step was tried three times")
	})
}

// Each step re-checks its own tool's role, GitHub permissions and switch when
// it runs, so a chain approved earlier cannot act with rights since lost.
func TestShipStepsReuseEachToolsAuthorizationWhenTheyRun(t *testing.T) {
	t.Run("demoted before the merge", func(t *testing.T) {
		f := newShipFixture(false)
		f.members[f.admin] = domain.RoleMember
		message := f.run(t)
		require.Contains(t, message, "Step 1 of 4 (merge the pull request) failed: Only workspace admins or the task's creator can merge")
		require.Empty(t, f.merger.merged)
	})

	t.Run("demoted after the merge", func(t *testing.T) {
		f := newShipFixture(false)
		f.merger.onMerge = func() { f.members[f.admin] = domain.RoleMember }
		message := f.run(t)
		require.Contains(t, message, "Step 3 of 4 (publish the release) failed: Only workspace owners and admins can publish a release")
		require.Len(t, f.merger.merged, 1)
		require.Empty(t, f.releases.created)
	})

	t.Run("no longer an instance administrator", func(t *testing.T) {
		f := newShipFixture(true)
		delete(f.instance, f.admin)
		message := f.run(t)
		require.Contains(t, message, "Step 5 of 5 (upgrade the instance) failed: Only an instance administrator can upgrade")
		require.Zero(t, f.updater.started)
		require.Len(t, f.releases.created, 1, "the steps before it ran")
	})

	t.Run("tool switched off", func(t *testing.T) {
		f := newShipFixture(false)
		f.approvals.SetToolSettings(shipSwitches{"create_release": true})
		message := f.run(t)
		require.Contains(t, message, "Step 3 of 4 (publish the release) failed: The tool create_release is disabled in this workspace")
		require.Empty(t, f.releases.created)
	})

	t.Run("GitHub permission missing", func(t *testing.T) {
		f := newShipFixture(false)
		f.approvals.SetGitHubAccess(&accessFake{gap: newGitHubAccessGap([]domain.GitHubPermissionNeed{{Name: "pull_requests", Level: "write"}}, "")})
		message := f.run(t)
		require.Contains(t, message, "Step 1 of 4 (merge the pull request) failed: The GitHub App lacks permissions this needs (pull_requests: write)")
		require.Empty(t, f.merger.merged)
	})

	t.Run("GitHub refuses for a permission the preflight missed", func(t *testing.T) {
		f := newShipFixture(false)
		f.releases.createErr = &gh.APIError{Status: 403, Message: "Resource not accessible by integration"}
		message := f.run(t)
		require.Contains(t, message, "Step 3 of 4 (publish the release) failed: The GitHub App lacks permissions this needs (checks: read, contents: write, statuses: read)")
	})
}

// A resumed chain continues from its last recorded step: a merge already made,
// recorded or not, is never repeated.
func TestShipResumesWithoutRepeatingFinishedSteps(t *testing.T) {
	f := newShipFixture(false)
	f.payload.Steps[0] = domain.ShipStep{Step: shipMerge, Status: "done"}
	f.payload.MergeCommit = releaseSHA
	f.github.status.State = "merged"
	require.Empty(t, f.run(t))
	require.Empty(t, f.merger.merged, "a recorded merge is not repeated")
	require.Len(t, f.releases.created, 1)

	// Stopped after merging but before recording it: GitHub shows the merge.
	f = newShipFixture(false)
	f.github.status.State, f.github.status.MergeCommit = "merged", releaseSHA
	require.Empty(t, f.run(t))
	require.Empty(t, f.merger.merged)
	require.Equal(t, releaseSHA, f.payload.MergeCommit)

	// Stopped after publishing: the release at the same commit counts as done.
	f = newShipFixture(false)
	f.payload.Steps[0], f.payload.Steps[1] = domain.ShipStep{Step: shipMerge, Status: "done"}, domain.ShipStep{Step: shipTests, Status: "done"}
	f.payload.MergeCommit = releaseSHA
	f.releases.releases = append([]gh.Release{{TagName: "v1.3.0", TargetCommitish: releaseSHA}}, f.releases.releases...)
	require.Empty(t, f.run(t))
	require.Empty(t, f.releases.created, "the release is not published twice")

	// A merge someone else made at another commit is not what was approved.
	f = newShipFixture(false)
	f.github.status.State, f.github.status.HeadSHA, f.github.status.MergeCommit = "merged", "other", releaseSHA
	require.Contains(t, f.run(t), "was merged at other, not at the approved head1")
	require.Empty(t, f.releases.created)
}

// Without a release workflow there is nothing to wait for, unless the instance
// is to be upgraded with what the build publishes.
func TestShipBuildStepWithoutAReleaseWorkflow(t *testing.T) {
	f := newShipFixture(false)
	f.releases.runs = nil
	require.Empty(t, f.run(t))
	require.Contains(t, f.payload.Steps[3].Detail, "No workflow run started for v1.3.0")

	f = newShipFixture(true)
	f.releases.runs = nil
	require.Contains(t, f.run(t), "No release build started for v1.3.0 within 5m0s, and the upgrade installs what it builds")
	require.Zero(t, f.updater.started)
}

// The approver can edit the chain's release notes like create_release's; a
// bad edit leaves the approval pending and the access gap stays on the card.
func TestShipNotesAreEditableOnTheCard(t *testing.T) {
	f := newShipFixture(false)
	raw, err := withGitHubAccess(f.payload, &domain.GitHubAccessGap{Message: "missing"})
	require.NoError(t, err)
	data, err := json.Marshal(raw)
	require.NoError(t, err)
	approval := &domain.ConversationApproval{Action: shipAction, Payload: data}

	_, err = editReleaseApproval(approval, "- staged on 192.168.0.4 first")
	var invalid *ApprovalEditError
	require.True(t, errors.As(err, &invalid))
	require.Contains(t, invalid.Message, "private address")

	edited, err := editReleaseApproval(approval, "  ### Fixed\n- **Edited.**  ")
	require.NoError(t, err)
	var payload domain.ShipPayload
	require.NoError(t, json.Unmarshal(edited, &payload))
	require.Equal(t, "### Fixed\n- **Edited.**", payload.Release.Notes)
	require.True(t, payload.Release.NotesEdited)
	require.Len(t, payload.Steps, 4)
	require.Contains(t, string(edited), `"github_access"`)
}

// shipTools is a workspace whose PR ENG-1 awaits review and whose repository
// has one release, as the chain's request checks see it.
func shipTools(t *testing.T) (*prFixture, *AssistantTools, *shipReleases) {
	t.Helper()
	pr := newPRFixture()
	target := &repository.MergeTarget{TaskID: pr.task, IssueID: pr.issue.ID, CreatorID: pr.creator, Repository: "acme/app", Number: 12, URL: "https://github.com/acme/app/pull/12", Commit: "head1"}
	pr.tools.SetAgentWork(prWork{merge: target})
	releases := &shipReleases{releaseFake: newReleaseFake()}
	pr.tools.SetReleases(releases, releaseRepos{}, &releaseStyles{})
	return pr, pr.tools, releases
}

func shipArguments(upgrade bool) string {
	data, _ := json.Marshal(map[string]any{"identifier": "eng-1", "tag": "v1.3.0", "notes": "### Features\n- **Dark mode.**", "upgrade": upgrade})
	return string(data)
}

// The request runs every step's own request checks before the one approval
// is offered, so a chain is never offered with a step already refused.
func TestShipRequestRunsEveryStepsRequestChecks(t *testing.T) {
	ctx := context.Background()
	pr, tools, releases := shipTools(t)
	issue, payload, err := tools.prepareShip(ctx, pr.ws, pr.admin, shipArguments(false))
	require.NoError(t, err)
	require.Equal(t, pr.issue.ID, issue)
	require.Equal(t, []domain.ShipStep{{Step: "merge", Status: "pending"}, {Step: "tests", Status: "pending"}, {Step: "release", Status: "pending"}, {Step: "build", Status: "pending"}}, payload.Steps)
	require.Equal(t, "head1", payload.Merge.Commit)
	require.Equal(t, "squash", payload.Merge.MergeMethod)
	require.Equal(t, "v1.2.0", payload.Release.PreviousTag, "the scanned range starts at the latest stable release")
	require.Equal(t, "main", payload.Release.Branch)
	require.Empty(t, payload.Release.TargetCommit, "the merge commit is not known yet")
	require.Nil(t, payload.Upgrade)

	for name, tc := range map[string]struct {
		user    func() uuid.UUID
		args    string
		prepare func()
		want    string
	}{
		"member":           {func() uuid.UUID { return pr.other }, shipArguments(false), nil, "only workspace owners and admins can publish a release"},
		"task creator":     {func() uuid.UUID { return pr.creator }, shipArguments(false), nil, "only workspace owners and admins can publish a release"},
		"guest":            {func() uuid.UUID { return pr.guest }, shipArguments(false), nil, "no longer have access"},
		"draft":            {func() uuid.UUID { return pr.admin }, shipArguments(false), func() { pr.github.status.Draft = true }, "mark_pull_request_ready"},
		"other base":       {func() uuid.UUID { return pr.admin }, shipArguments(false), func() { pr.github.status.BaseRef = "develop" }, "merges into develop, not main"},
		"tag exists":       {func() uuid.UUID { return pr.admin }, strings.Replace(shipArguments(false), "v1.3.0", "v1.2.0", 1), nil, "the tag v1.2.0 already exists"},
		"invalid tag":      {func() uuid.UUID { return pr.admin }, strings.Replace(shipArguments(false), "v1.3.0", "-x", 1), nil, "is not a valid tag"},
		"secret in notes":  {func() uuid.UUID { return pr.admin }, strings.Replace(shipArguments(false), "Dark mode.", "ghp_"+strings.Repeat("a", 36), 1), nil, "notes line 2 (GitHub token)"},
		"no release yet":   {func() uuid.UUID { return pr.admin }, shipArguments(false), func() { releases.releases = nil }, "no earlier release"},
		"switched off":     {func() uuid.UUID { return pr.admin }, shipArguments(false), func() { tools.SetSettings(shipToolSettings{"get_workflow_run": true}) }, "get_workflow_run is disabled in this workspace, and the step \"wait for the release build\" needs it"},
		"upgrade unwired":  {func() uuid.UUID { return pr.admin }, shipArguments(true), nil, "cannot be upgraded from chat"},
		"no pull request":  {func() uuid.UUID { return pr.admin }, shipArguments(false), func() { tools.SetAgentWork(prWork{}) }, "no pull request of this issue is awaiting review"},
		"unknown argument": {func() uuid.UUID { return pr.admin }, `{"identifier":"ENG-1","tag":"v1.3.0","notes":"x","force":true}`, nil, "invalid arguments"},
	} {
		t.Run(name, func(t *testing.T) {
			pr, tools, releases = shipTools(t)
			if tc.prepare != nil {
				tc.prepare()
			}
			_, _, err := tools.prepareShip(ctx, pr.ws, tc.user(), tc.args)
			require.Error(t, err)
			require.Contains(t, toolErrorText(err), tc.want)
		})
	}
}

type shipToolSettings map[string]bool

func (s shipToolSettings) Disabled(context.Context, uuid.UUID) (map[string]bool, error) {
	return s, nil
}
func (shipToolSettings) Set(context.Context, uuid.UUID, uuid.UUID, string, bool) error { return nil }

// Upgrading in the chain needs an instance administrator and a release of the
// repository this instance installs that is newer than the installed one.
func TestShipRequestWithUpgradeChecksTheInstance(t *testing.T) {
	ctx := context.Background()
	setup := func(version string) (*prFixture, *AssistantTools, *updaterFake) {
		pr, tools, _ := shipTools(t)
		tools.SetReleases(tools.releases, kuayleRepos{}, &releaseStyles{})
		tools.SetAgentWork(prWork{merge: &repository.MergeTarget{TaskID: pr.task, IssueID: pr.issue.ID, CreatorID: pr.creator, Repository: instanceSourceRepository, Number: 12, URL: "https://github.com/" + instanceSourceRepository + "/pull/12", Commit: "head1"}})
		updater := &updaterFake{configured: true}
		system := NewAssistantSystem(updater, func(id uuid.UUID) bool { return id == pr.admin }, version)
		system.releases = func(context.Context) ([]instanceRelease, error) { return []instanceRelease{{Tag: "v1.2.0"}}, nil }
		tools.SetSystem(system)
		return pr, tools, updater
	}
	pr, tools, updater := setup("v1.2.0")
	_, payload, err := tools.prepareShip(ctx, pr.ws, pr.admin, shipArguments(true))
	require.NoError(t, err)
	require.Equal(t, &domain.UpgradePayload{CurrentVersion: "1.2.0", LatestVersion: "1.3.0"}, payload.Upgrade)
	require.Len(t, payload.Steps, 5)
	require.Equal(t, "upgrade", payload.Steps[4].Step)
	require.Zero(t, updater.started, "asking upgrades nothing")

	pr, tools, _ = setup("v1.2.0")
	pr.tools.members.(prMembers)[pr.other] = domain.RoleAdmin
	_, _, err = tools.prepareShip(ctx, pr.ws, pr.other, shipArguments(true))
	require.Contains(t, toolErrorText(err), "only an instance administrator can upgrade")

	pr, tools, _ = setup("v1.3.0")
	_, _, err = tools.prepareShip(ctx, pr.ws, pr.admin, shipArguments(true))
	require.Contains(t, toolErrorText(err), "v1.3.0 is not newer than the installed 1.3.0")

	pr, tools, updater = setup("v1.2.0")
	updater.status.Running = true
	_, _, err = tools.prepareShip(ctx, pr.ws, pr.admin, shipArguments(true))
	require.Contains(t, toolErrorText(err), "an upgrade is already running")

	// A release of another repository cannot upgrade this instance.
	pr, tools, _ = shipTools(t)
	system := NewAssistantSystem(&updaterFake{configured: true}, func(uuid.UUID) bool { return true }, "v1.2.0")
	tools.SetSystem(system)
	_, _, err = tools.prepareShip(ctx, pr.ws, pr.admin, shipArguments(true))
	require.Contains(t, toolErrorText(err), "this instance installs releases of "+instanceSourceRepository)
}

type kuayleRepos struct{}

func (kuayleRepos) GetLinkedRepositoryByFullName(_ context.Context, _ uuid.UUID, name string) (*domain.GitHubRepoModel, error) {
	if !strings.EqualFold(name, instanceSourceRepository) {
		return nil, nil
	}
	return &domain.GitHubRepoModel{FullName: instanceSourceRepository, DefaultBranch: "main", IsActive: true}, nil
}

// The chain is one tool, offered only when its steps' tools are wired, needs
// approval, and declares what its steps call on GitHub.
func TestShipToolNeedsApprovalAndDeclaresItsStepsPermissions(t *testing.T) {
	tool, ok := lookupAssistantTool(shipAction)
	require.True(t, ok)
	require.True(t, tool.approval)
	require.False(t, (&AssistantTools{}).available(tool))
	_, tools, _ := shipTools(t)
	require.True(t, tools.available(tool))

	policy := assistantActionPolicies[shipAction]
	require.Equal(t, roleWorkspaceAdmin, policy.role)
	for step, action := range shipStepActions {
		for permission, level := range assistantActionPolicies[action].github {
			got := policy.github[permission]
			require.NotEmpty(t, got, "the chain must ask for %s:%s that step %s needs", permission, level, step)
			if level == "write" {
				require.Equal(t, "write", got, "step %s writes %s", step, permission)
			}
		}
	}
}
