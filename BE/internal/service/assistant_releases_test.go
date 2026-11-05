package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	gh "github.com/kuayle/kuayle-backend/pkg/github"
	"github.com/stretchr/testify/require"
)

const releaseSHA = "0123456789abcdef0123456789abcdef01234567"

// releaseFake is the GitHub side of a repository with one previous release.
type releaseFake struct {
	releases   []gh.Release
	checks     []PullRequestCheck
	coverage   ChecksCoverage
	onBranch   bool
	tags       map[string]bool
	rng        ReleaseRange
	pulls      map[int]gh.PullRequest
	runs       []gh.WorkflowRun
	run        *gh.WorkflowRun
	jobs       []gh.WorkflowJob
	readErr    error
	createErr  error
	created    []gh.NewRelease
	rangeBases []string
}

func newReleaseFake() *releaseFake {
	published := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	return &releaseFake{
		releases: []gh.Release{
			{TagName: "v1.3.0-rc.1", Prerelease: true},
			{TagName: "v1.2.0", Name: "v1.2.0", Body: "### Features\n- **Old feature (KUA-1).** Text.\n\n**Full Changelog**: https://github.com/acme/app/compare/v1.1.0...v1.2.0", HTMLURL: "https://github.com/acme/app/releases/tag/v1.2.0", PublishedAt: &published},
		},
		checks:   []PullRequestCheck{{Name: "Tests", State: "passed"}, {Name: "Security", State: "passed"}},
		onBranch: true,
		tags:     map[string]bool{"v1.2.0": true},
		rng: ReleaseRange{Status: "ahead", TotalCommits: 3, Messages: []string{"Merge pull request #12 from acme/release-tools", "fix(ui): keep focus (#13)", "chore: tidy"},
			Files: []gh.CompareFile{
				{Filename: "BE/migrations/000111_release_approvals.up.sql", Status: "added", Patch: "@@ -0,0 +1 @@\n+ALTER TABLE x;"},
				{Filename: "UI/src/app.ts", Status: "modified", Patch: "@@ -1 +1 @@\n-a\n+b"},
			}},
		pulls: map[int]gh.PullRequest{
			12: {Number: 12, Title: "feat(assistant): release tools", HTMLURL: "https://github.com/acme/app/pull/12", Body: "Adds the tools."},
			13: {Number: 13, Title: "fix(ui): keep focus", HTMLURL: "https://github.com/acme/app/pull/13"},
		},
	}
}

func (f *releaseFake) ListReleases(context.Context, uuid.UUID, string, int) ([]gh.Release, error) {
	return f.releases, f.readErr
}
func (f *releaseFake) GetRelease(_ context.Context, _ uuid.UUID, _ string, tag string) (*gh.Release, error) {
	if f.readErr != nil {
		return nil, f.readErr
	}
	for _, r := range f.releases {
		if r.TagName == tag {
			r := r
			return &r, nil
		}
	}
	return nil, &gh.APIError{Status: 404, Message: "Not Found"}
}
func (f *releaseFake) ResolveCommit(_ context.Context, _ uuid.UUID, _ string, ref string) (string, error) {
	if f.readErr != nil {
		return "", f.readErr
	}
	if ref == "main" || f.tags[ref] || strings.HasPrefix(releaseSHA, ref) {
		return releaseSHA, nil
	}
	return "", &gh.APIError{Status: 422, Message: "No commit found"}
}
func (f *releaseFake) OnBranch(context.Context, uuid.UUID, string, string, string) (bool, error) {
	return f.onBranch, nil
}
func (f *releaseFake) ReleaseRange(_ context.Context, _ uuid.UUID, _ string, base, _ string) (*ReleaseRange, error) {
	f.rangeBases = append(f.rangeBases, base)
	r := f.rng
	return &r, f.readErr
}
func (f *releaseFake) PullRequestSummary(_ context.Context, _ uuid.UUID, _ string, number int) (*gh.PullRequest, error) {
	pull, ok := f.pulls[number]
	if !ok {
		return nil, &gh.APIError{Status: 404}
	}
	return &pull, nil
}
func (f *releaseFake) CommitChecks(context.Context, uuid.UUID, string, string) ([]PullRequestCheck, ChecksCoverage, error) {
	return f.checks, f.coverage, f.readErr
}
func (f *releaseFake) TagExists(_ context.Context, _ uuid.UUID, _ string, tag string) (bool, error) {
	return f.tags[tag], nil
}
func (f *releaseFake) CreateRelease(_ context.Context, _ uuid.UUID, repository string, release gh.NewRelease) (*gh.Release, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}
	f.created = append(f.created, release)
	f.tags[release.TagName] = true
	f.releases = append([]gh.Release{{TagName: release.TagName, TargetCommitish: release.TargetCommitish, Body: release.Body, HTMLURL: "https://github.com/" + repository + "/releases/tag/" + release.TagName}}, f.releases...)
	return &f.releases[0], nil
}
func (f *releaseFake) WorkflowRuns(context.Context, uuid.UUID, string, string) ([]gh.WorkflowRun, error) {
	return f.runs, f.readErr
}
func (f *releaseFake) WorkflowRun(context.Context, uuid.UUID, string, int64) (*gh.WorkflowRun, []gh.WorkflowJob, bool, error) {
	if f.run == nil {
		return nil, nil, false, &gh.APIError{Status: 404}
	}
	return f.run, f.jobs, false, nil
}

type releaseRepos struct{}

func (releaseRepos) GetLinkedRepositoryByFullName(_ context.Context, _ uuid.UUID, name string) (*domain.GitHubRepoModel, error) {
	if !strings.EqualFold(name, "acme/app") {
		return nil, nil
	}
	return &domain.GitHubRepoModel{FullName: "acme/app", DefaultBranch: "main", IsActive: true}, nil
}

// releaseStyles is the workspace's style skill; nil skill means none yet.
type releaseStyles struct {
	skill  *domain.AssistantSkill
	seeded int
}

func (s *releaseStyles) ReleaseNotesStyle(_ context.Context, _ uuid.UUID, seed domain.AssistantSkillInput) (*domain.AssistantSkill, error) {
	if s.skill == nil {
		s.seeded++
		s.skill = &domain.AssistantSkill{Name: seed.Name, Description: seed.Description, Body: seed.Body, Status: "active", Enabled: true, CurrentVersion: 1}
	}
	copy := *s.skill
	return &copy, nil
}

type releaseFixture struct {
	tools     *AssistantTools
	approvals *AssistantApprovals
	github    *releaseFake
	styles    *releaseStyles
	ws        uuid.UUID
	admin     uuid.UUID
	member    uuid.UUID
}

func newReleaseFixture() *releaseFixture {
	f := &releaseFixture{github: newReleaseFake(), styles: &releaseStyles{}, ws: uuid.New(), admin: uuid.New(), member: uuid.New()}
	members := prMembers{f.admin: domain.RoleAdmin, f.member: domain.RoleMember}
	f.tools = NewAssistantTools(prIssues{}, nil, members, nil)
	f.tools.SetReleases(f.github, releaseRepos{}, f.styles)
	f.approvals = &AssistantApprovals{members: members}
	f.approvals.SetReleases(f.github)
	return f
}

func (f *releaseFixture) run(t *testing.T, name, arguments string) (map[string]any, error) {
	t.Helper()
	result, _, err := f.tools.runReleaseTool(context.Background(), f.ws, name, arguments)
	if err != nil {
		return nil, err
	}
	return result.(map[string]any), nil
}

func (f *releaseFixture) prepare(t *testing.T, arguments string) (domain.ReleasePayload, error) {
	t.Helper()
	return f.tools.prepareRelease(context.Background(), f.ws, f.admin, arguments)
}

const releaseNotes = "### Features\n- **Release tools (KUA-88).** Publish from chat.\n\n**Full Changelog**: https://github.com/acme/app/compare/v1.2.0...v1.3.0"

func createArgs(notes string) string { return createTagArgs("v1.3.0", notes) }

func createTagArgs(tag, notes string) string {
	data, _ := json.Marshal(map[string]any{"repository": "acme/app", "tag": tag, "target_commit": releaseSHA[:7], "notes": notes})
	return string(data)
}

func TestReleaseToolCatalogNeedsApprovalOnlyToPublish(t *testing.T) {
	approval := map[string]bool{}
	for _, tool := range assistantReleaseToolCatalog {
		approval[tool.name] = tool.approval
		require.True(t, tool.releases)
		require.False(t, (&AssistantTools{}).available(tool), "%s is offered only once wired", tool.name)
		require.True(t, newReleaseFixture().tools.available(tool))
	}
	require.Equal(t, map[string]bool{"list_releases": false, "get_release": false, "draft_release_notes": false, "create_release": true, "get_workflow_run": false}, approval)
	require.Equal(t, roleWorkspaceAdmin, assistantActionPolicies["create_release"].role)
	require.Equal(t, "write", assistantActionPolicies["create_release"].github["contents"])
	require.Equal(t, map[string]string{"actions": "read"}, assistantActionPolicies["get_workflow_run"].github)
}

func TestCreateReleaseIsAdminOnly(t *testing.T) {
	ctx, ws := context.Background(), uuid.New()
	for role, allowed := range map[string]bool{domain.RoleOwner: true, domain.RoleAdmin: true, domain.RoleMember: false, domain.RoleGuest: false, "": false} {
		refusal, err := authorizeAssistantAction(ctx, memberFake{role}, nil, ws, uuid.New(), "create_release", uuid.Nil)
		require.NoError(t, err)
		require.Equal(t, allowed, refusal == "", role)
	}
	refusal, _ := authorizeAssistantAction(ctx, memberFake{domain.RoleMember}, nil, ws, uuid.New(), "create_release", uuid.Nil)
	require.Contains(t, refusal, "only workspace owners and admins can publish a release")
	require.False(t, ownerScoped("create_release"), "the role does not depend on a target, so it is checked up front")
}

func TestListAndGetReleasesShowThePreviousNotes(t *testing.T) {
	f := newReleaseFixture()
	result, err := f.run(t, "list_releases", `{"repository":"ACME/app"}`)
	require.NoError(t, err)
	releases := result["releases"].([]map[string]any)
	require.Len(t, releases, 2)
	require.Equal(t, "v1.2.0", releases[1]["tag"])
	require.Contains(t, releases[1]["notes_excerpt"], "Old feature")
	require.Equal(t, "v1.2.0", result["latest_tag"], "a prerelease is not the latest release")

	result, err = f.run(t, "get_release", `{"repository":"acme/app","tag":"v1.2.0"}`)
	require.NoError(t, err)
	require.Contains(t, result["notes"], "**Full Changelog**")

	_, err = f.run(t, "get_release", `{"repository":"acme/app","tag":"v9.9.9"}`)
	require.ErrorContains(t, err, "no release v9.9.9")
	_, err = f.run(t, "list_releases", `{"repository":"other/repo"}`)
	require.ErrorContains(t, err, "not linked to this workspace")
	f.github.readErr = &gh.APIError{Status: 403, Message: "Resource not accessible by integration"}
	_, err = f.run(t, "list_releases", `{"repository":"acme/app"}`)
	require.ErrorContains(t, err, "GitHub refused")
}

func TestDraftReleaseNotesUseTheSeededStyleAndThePullRequestsSinceTheLastTag(t *testing.T) {
	f := newReleaseFixture()
	result, err := f.run(t, "draft_release_notes", `{"repository":"acme/app"}`)
	require.NoError(t, err)
	require.Equal(t, 1, f.styles.seeded, "the default style is stored as an editable skill on first use")
	require.Equal(t, []string{"v1.2.0"}, f.github.rangeBases, "the range starts at the latest release, not a prerelease")
	require.Equal(t, "v1.2.0", result["previous_tag"])
	require.Equal(t, "v1.2.1", result["tag"])
	require.Equal(t, releaseSHA, result["target_commit"])
	notes := result["notes"].(string)
	require.Contains(t, notes, "### Features\n- **Release tools** (#12)")
	require.Contains(t, notes, "### Fixed\n- **Keep focus** (#13)")
	require.Contains(t, notes, "000111_release_approvals.up.sql")
	require.Contains(t, notes, "All 2 checks passed on main")
	require.Contains(t, notes, "**Full Changelog**: https://github.com/acme/app/compare/v1.2.0...v1.2.1")
	style := result["style"].(map[string]any)
	require.Equal(t, releaseNotesSkillName, style["skill"])
	require.Contains(t, style["guidance"], "Sections:")
	require.Contains(t, result["previous_notes"], "Old feature", "the previous release's notes show the style in practice")
	require.Equal(t, "passing", result["checks_state"])
	pulls := result["pull_requests"].([]releasePull)
	require.Len(t, pulls, 2)
	require.Equal(t, "Adds the tools.", pulls[0].Body)

	// An admin's edit to the skill changes the draft.
	f.styles.skill.Body = "Sections: Fixed, Full Changelog"
	f.styles.skill.CurrentVersion = 2
	result, err = f.run(t, "draft_release_notes", `{"repository":"acme/app","tag":"v2.0.0"}`)
	require.NoError(t, err)
	require.Equal(t, 1, f.styles.seeded)
	require.NotContains(t, result["notes"], "### Features")
	require.True(t, strings.HasPrefix(result["notes"].(string), "### Fixed"))
	require.EqualValues(t, 2, result["style"].(map[string]any)["version"])

	// A disabled skill is not followed; the built-in default is used instead.
	f.styles.skill.Enabled = false
	result, err = f.run(t, "draft_release_notes", `{"repository":"acme/app"}`)
	require.NoError(t, err)
	require.Contains(t, result["notes"], "### Features")
	require.Equal(t, "default", result["style"].(map[string]any)["source"])
}

func TestCreateReleaseRefusesUnlessTheBranchTestsPassed(t *testing.T) {
	for _, tc := range []struct {
		name     string
		checks   []PullRequestCheck
		coverage ChecksCoverage
		want     string
	}{
		{"failing", []PullRequestCheck{{Name: "Tests", State: "failed"}, {Name: "Security", State: "passed"}}, ChecksCoverage{}, "1 check(s) failed on main at 01234567: Tests"},
		{"pending", []PullRequestCheck{{Name: "Tests", State: "pending"}}, ChecksCoverage{}, "still running"},
		{"none", nil, ChecksCoverage{}, "no checks"},
		{"incomplete", []PullRequestCheck{{Name: "Tests", State: "passed"}}, ChecksCoverage{StatusesUnavailable: true}, "not every check on main at 01234567 could be read"},
	} {
		f := newReleaseFixture()
		f.github.checks, f.github.coverage = tc.checks, tc.coverage
		_, err := f.prepare(t, createArgs(releaseNotes))
		require.Error(t, err, tc.name)
		require.Contains(t, toolErrorText(err), "Tagging refused", tc.name)
		require.Contains(t, toolErrorText(err), tc.want, tc.name)
	}
}

func TestCreateReleaseRefusesBadTargetsTagsAndSecrets(t *testing.T) {
	f := newReleaseFixture()
	f.github.onBranch = false
	_, err := f.prepare(t, createArgs(releaseNotes))
	require.ErrorContains(t, err, "not on main")

	f = newReleaseFixture()
	_, err = f.prepare(t, createTagArgs("v1.2.0", releaseNotes))
	require.ErrorContains(t, err, "tag v1.2.0 already exists")
	_, err = f.prepare(t, createTagArgs("v1..3", releaseNotes))
	require.ErrorContains(t, err, "not a valid tag")

	f = newReleaseFixture()
	f.github.rng.Files = append(f.github.rng.Files, gh.CompareFile{Filename: "deploy/config.yml", Status: "modified", Patch: "@@ -4 +4,2 @@\n ctx\n+token: " + fakeGitHubToken})
	_, err = f.prepare(t, createArgs(releaseNotes))
	require.Error(t, err)
	require.Contains(t, toolErrorText(err), "deploy/config.yml line 5 (GitHub token)")
	require.NotContains(t, toolErrorText(err), fakeGitHubToken)

	f = newReleaseFixture()
	f.github.rng.FilesTruncated = true
	_, err = f.prepare(t, createArgs(releaseNotes))
	require.ErrorContains(t, err, "more files than the scan can read")

	f = newReleaseFixture()
	f.github.rng.Status = "diverged"
	_, err = f.prepare(t, createArgs(releaseNotes))
	require.ErrorContains(t, err, "does not descend from v1.2.0")

	f = newReleaseFixture()
	_, err = f.prepare(t, createArgs(releaseNotes+"\n- Deployed on build.corp first"))
	require.ErrorContains(t, err, "private host name")
}

func TestCreateReleaseRequestPinsTheCommitNotesAndScan(t *testing.T) {
	f := newReleaseFixture()
	f.github.rng.Files = append(f.github.rng.Files, gh.CompareFile{Filename: "UI/static/logo.png", Status: "added"})
	payload, err := f.prepare(t, createArgs(releaseNotes))
	require.NoError(t, err)
	require.Equal(t, domain.ReleasePayload{
		Repository: "acme/app", Tag: "v1.3.0", Name: "v1.3.0", TargetCommit: releaseSHA, Branch: "main", PreviousTag: "v1.2.0",
		Notes: releaseNotes, ChecksPassed: 2, Commits: 3, ScannedFiles: 2, UnscannedFiles: []string{"UI/static/logo.png"},
		CompareURL: "https://github.com/acme/app/compare/v1.2.0...0123456789abcdef0123456789abcdef01234567",
		ReleaseURL: "https://github.com/acme/app/releases/tag/v1.3.0",
	}, payload)
	require.Empty(t, f.github.created, "asking publishes nothing")
}

func releaseApproval(t *testing.T, payload domain.ReleasePayload, actor uuid.UUID) *domain.ConversationApproval {
	data, err := json.Marshal(payload)
	require.NoError(t, err)
	return &domain.ConversationApproval{ID: uuid.New(), Action: "create_release", ActorID: actor, Payload: data}
}

func TestApprovingPublishesTheApprovedNotesAfterCheckingAgain(t *testing.T) {
	f := newReleaseFixture()
	payload, err := f.prepare(t, createArgs(releaseNotes))
	require.NoError(t, err)

	// The edited notes on the card are what gets published, after the same check.
	approval := releaseApproval(t, payload, f.admin)
	edited, err := editReleaseApproval(approval, "### Fixed\n- **Edited by hand.**")
	require.NoError(t, err)
	approval.Payload = edited
	_, err = editReleaseApproval(approval, "- see 10.1.2.3")
	require.ErrorContains(t, err, "private address")
	_, err = editReleaseApproval(&domain.ConversationApproval{Action: "merge_pull_request", Payload: []byte(`{}`)}, "x")
	require.Error(t, err, "only release notes can be edited")

	message, err := f.approvals.applyRelease(context.Background(), f.ws, approval)
	require.NoError(t, err)
	require.Empty(t, message)
	require.Len(t, f.github.created, 1)
	require.Equal(t, gh.NewRelease{TagName: "v1.3.0", TargetCommitish: releaseSHA, Name: "v1.3.0", Body: "### Fixed\n- **Edited by hand.**"}, f.github.created[0])

	// Approving again after a lost answer finds the release instead of tagging twice.
	message, err = f.approvals.applyRelease(context.Background(), f.ws, approval)
	require.NoError(t, err)
	require.Empty(t, message)
	require.Len(t, f.github.created, 1)
}

func TestApprovingRefusesWhenTheGateOrScanNoLongerPasses(t *testing.T) {
	f := newReleaseFixture()
	payload, err := f.prepare(t, createArgs(releaseNotes))
	require.NoError(t, err)
	approval := releaseApproval(t, payload, f.admin)

	f.github.checks = []PullRequestCheck{{Name: "Tests", State: "failed"}}
	message, err := f.approvals.applyRelease(context.Background(), f.ws, approval)
	require.NoError(t, err)
	require.Contains(t, message, "Tagging refused")
	require.Empty(t, f.github.created)

	f = newReleaseFixture()
	f.github.tags["v1.3.0"] = true
	f.github.releases = append(f.github.releases, gh.Release{TagName: "v1.3.0", TargetCommitish: "ffffffffffffffffffffffffffffffffffffffff"})
	message, err = f.approvals.applyRelease(context.Background(), f.ws, approval)
	require.NoError(t, err)
	require.Contains(t, message, "already exists")

	f = newReleaseFixture()
	f.github.createErr = &gh.APIError{Status: 422, Message: "Validation Failed"}
	message, err = f.approvals.applyRelease(context.Background(), f.ws, approval)
	require.NoError(t, err)
	require.Contains(t, message, "GitHub refused the release: Validation Failed")

	f = newReleaseFixture()
	f.github.createErr = errors.New("connection reset")
	_, err = f.approvals.applyRelease(context.Background(), f.ws, approval)
	require.Error(t, err, "a transient failure leaves the claim for approving again")
}

func TestGetWorkflowRunFollowsTheReleaseBuild(t *testing.T) {
	f := newReleaseFixture()
	f.github.runs = []gh.WorkflowRun{
		{ID: 9, Name: "Build & Push", Event: "release", Status: "in_progress", HTMLURL: "https://github.com/acme/app/actions/runs/9"},
		{ID: 8, Name: "Tests", Event: "push", Status: "completed", Conclusion: "success"},
	}
	result, err := f.run(t, "get_workflow_run", `{"repository":"acme/app","tag":"v1.2.0","workflow":"build & push"}`)
	require.NoError(t, err)
	require.Equal(t, "running", result["state"])
	runs := result["runs"].([]map[string]any)
	require.Len(t, runs, 1)
	require.EqualValues(t, 9, runs[0]["id"])
	require.Contains(t, result["next_step"], "still running")

	f.github.run = &gh.WorkflowRun{ID: 9, Name: "Build & Push", Status: "completed", Conclusion: "failure", HTMLURL: "https://github.com/acme/app/actions/runs/9"}
	f.github.jobs = []gh.WorkflowJob{{Name: "api", Status: "completed", Conclusion: "failure", HTMLURL: "https://github.com/acme/app/actions/runs/9/job/1"}, {Name: "web", Status: "completed", Conclusion: "success"}}
	result, err = f.run(t, "get_workflow_run", `{"repository":"acme/app","run_id":9}`)
	require.NoError(t, err)
	require.Equal(t, "failure", result["state"])
	failed := result["failed_jobs"].([]map[string]any)
	require.Len(t, failed, 1)
	require.Equal(t, "api", failed[0]["name"])

	f.github.run.Conclusion = "success"
	result, err = f.run(t, "get_workflow_run", `{"repository":"acme/app","run_id":9}`)
	require.NoError(t, err)
	require.Equal(t, "success", result["state"])

	f.github.runs = nil
	result, err = f.run(t, "get_workflow_run", `{"repository":"acme/app","commit":"0123456"}`)
	require.NoError(t, err)
	require.Equal(t, "none", result["state"])
	_, err = f.run(t, "get_workflow_run", `{"repository":"acme/app"}`)
	require.ErrorContains(t, err, "run_id, tag or commit")
}

func TestCreateReleaseScansFromTheLatestReleaseOnly(t *testing.T) {
	// The model cannot choose a narrower range to scan.
	f := newReleaseFixture()
	_, err := f.prepare(t, `{"repository":"acme/app","tag":"v1.3.0","target_commit":"0123456","notes":"x","previous_tag":"v1.2.9"}`)
	require.ErrorContains(t, err, "invalid arguments")

	// Without an earlier stable release there is no range to scan, so nothing is tagged.
	f = newReleaseFixture()
	f.github.releases = []gh.Release{{TagName: "v1.3.0-rc.1", Prerelease: true}}
	_, err = f.prepare(t, createArgs(releaseNotes))
	require.ErrorContains(t, err, "no earlier release")
	require.Empty(t, f.github.rangeBases)

	// A text file GitHub sent no diff for cannot be scanned, so it blocks tagging.
	f = newReleaseFixture()
	f.github.rng.Files = append(f.github.rng.Files, gh.CompareFile{Filename: "BE/fixtures/huge.json", Status: "modified"})
	_, err = f.prepare(t, createArgs(releaseNotes))
	require.ErrorContains(t, err, "BE/fixtures/huge.json")
	require.ErrorContains(t, err, "could not be scanned")
}

func TestReleaseToolResultsFitTheToolOutputLimit(t *testing.T) {
	f := newReleaseFixture()
	wide := strings.Repeat("发布说明<&>", 4000)
	f.github.releases = nil
	for i := 0; i < 20; i++ {
		f.github.releases = append(f.github.releases, gh.Release{TagName: "v1." + strings.Repeat("9", i+1) + ".0", Body: wide})
	}
	f.github.releases[0].TagName = "v1.2.0"
	fits := func(result map[string]any) {
		data, err := json.Marshal(result)
		require.NoError(t, err)
		require.LessOrEqual(t, len(data), assistantToolOutputBytes)
	}
	result, err := f.run(t, "list_releases", `{"repository":"acme/app","limit":20}`)
	require.NoError(t, err)
	fits(result)
	result, err = f.run(t, "get_release", `{"repository":"acme/app","tag":"v1.2.0"}`)
	require.NoError(t, err)
	fits(result)
	require.Equal(t, true, result["notes_truncated"])

	f.github.rng.Messages = nil
	for i := 1; i <= 40; i++ {
		f.github.rng.Messages = append(f.github.rng.Messages, "Merge pull request #"+strings.Repeat("1", 1)+string(rune('0'+i%10))+" from x")
	}
	for n := 10; n <= 19; n++ {
		f.github.pulls[n] = gh.PullRequest{Number: n, Title: "feat: " + strings.Repeat("长标题", 70), HTMLURL: "https://github.com/acme/app/pull/1", Body: wide}
	}
	result, err = f.run(t, "draft_release_notes", `{"repository":"acme/app"}`)
	require.NoError(t, err)
	fits(result)
}
