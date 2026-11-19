package machine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	githubclient "github.com/kuayle/kuayle-backend/pkg/github"
	"github.com/stretchr/testify/require"
)

type publicationStoreFake struct {
	*managerStoreFake
	publication domain.AgentPublication
	started     int
	result      *domain.AgentPublicationResult
}

func (f *publicationStoreFake) GetPublication(context.Context, domain.DevMachineOperation) (*domain.AgentPublication, error) {
	copy := f.publication
	return &copy, nil
}
func (f *publicationStoreFake) StartPublication(context.Context, domain.DevMachineOperation) error {
	f.started++
	f.publication.Step.Status = "running"
	return nil
}
func (f *publicationStoreFake) CompletePublication(_ context.Context, _ domain.DevMachineOperation, result domain.AgentPublicationResult) error {
	f.result = &result
	return nil
}

type publicationRuntimeFake struct {
	*runtimeFake
	dirty   bool
	ahead   *int
	scanErr error
	request PublishRequest
	commit  string
	err     error
	calls   int
	// files are the worktree's .kuayle files by name.
	files map[string][]byte
	reads []string
}

func (f *publicationRuntimeFake) ScanWorktree(_ context.Context, _ *domain.DevMachine, _ []domain.DevMachineService, job WorktreeScanJob) (*domain.WorktreeScanResult, error) {
	if job.PublicationStatus && f.scanErr != nil {
		return nil, f.scanErr
	}
	return &domain.WorktreeScanResult{Branch: job.Branch, Commit: strings.Repeat("a", 40), Dirty: f.dirty, Ahead: f.ahead}, nil
}

func (f *publicationRuntimeFake) ReadAgentFile(_ context.Context, _ []domain.DevMachineService, _ *domain.DevMachineCheckout, name string, limit int) ([]byte, error) {
	f.reads = append(f.reads, name)
	data, ok := f.files[name]
	if !ok {
		return nil, nil
	}
	if len(data) > limit {
		data = data[:limit]
	}
	return data, nil
}

func (f *publicationRuntimeFake) PublishCheckout(_ context.Context, _ *domain.DevMachine, _ []domain.DevMachineService, _ *domain.DevMachineCheckout, request PublishRequest) (string, error) {
	f.calls++
	f.request = request
	return f.commit, f.err
}

type publicationGitHubFake struct {
	existing *githubclient.PullRequest
	created  *githubclient.PullRequest
	err      error
	creates  int
	draft    bool
	updates  int
	title    string
	body     string
	head     string
	base     string
	readied  []string
	readyErr error
	drafted  []string

	screenshotBranch string
	screenshotFiles  []githubclient.RepositoryFile
	screenshotErr    error
}

func (f *publicationGitHubFake) PublishFilesToBranch(_, _, _, branch, _ string, files []githubclient.RepositoryFile) (string, error) {
	f.screenshotBranch, f.screenshotFiles = branch, files
	if f.screenshotErr != nil {
		return "", f.screenshotErr
	}
	return strings.Repeat("c", 40), nil
}

func (f *publicationGitHubFake) GetRepositoryInstallationToken(int64, string) (string, time.Time, error) {
	return "fixture-installation-token", time.Now().Add(time.Hour), nil
}
func (f *publicationGitHubFake) FindOpenPullRequest(_, _, _, head, base string) (*githubclient.PullRequest, error) {
	f.head, f.base = head, base
	return f.existing, f.err
}
func (f *publicationGitHubFake) CreatePullRequest(_, _, _, title, head, base, body string, draft bool) (*githubclient.PullRequest, error) {
	f.creates++
	f.title, f.body, f.draft = title, body, draft
	return f.created, nil
}
func (f *publicationGitHubFake) UpdatePullRequestBody(_, _, _ string, _ int, body string) error {
	f.updates++
	f.body = body
	return nil
}

func (f *publicationGitHubFake) MarkPullRequestReadyForReview(_, nodeID string) error {
	f.readied = append(f.readied, nodeID)
	return f.readyErr
}

func (f *publicationGitHubFake) ConvertPullRequestToDraft(_, nodeID string) error {
	f.drafted = append(f.drafted, nodeID)
	f.existing.Draft = true
	return nil
}

func publicationFixture() (*domain.DevMachine, *publicationStoreFake) {
	taskID, checkoutID, runID := uuid.New(), uuid.New(), uuid.New()
	machine := &domain.DevMachine{ID: uuid.New(), WorkspaceID: uuid.New()}
	summary := "Done. @maintainers please merge ~~~ now"
	return machine, &publicationStoreFake{
		managerStoreFake: &managerStoreFake{machine: machine, githubInstallationID: 7,
			agentRun: &domain.DevMachineAgentRun{ID: runID, TaskID: &taskID, CheckoutID: &checkoutID, Summary: &summary, ProviderID: "codex", Model: "deepseek-flash",
				ChangedFiles: []byte(`["src/text.js","test/text.test.js"]`), Commits: []byte(`["4916a03781dea5f9cec4ddc20df8f6431b39d217"]`)},
			checkout: &domain.DevMachineCheckout{ID: checkoutID, MachineID: machine.ID, WorkspaceID: machine.WorkspaceID, ScopeKey: taskID.String(), Status: "ready", RepositoryFullName: "acme/app", BaseBranch: "main", WorkingBranch: "kuayle/rec-1"}},
		publication: domain.AgentPublication{Step: domain.DevMachineAgentRunStep{ID: uuid.New(), AgentRunID: runID, Status: "queued"}, IssueIdentifier: "REC-1", IssueTitle: "Dark mode", CheckArgv: []string{"npm", "test"}, CheckExitCode: ptr(0)},
	}
}

func TestPublishAgentOpensOnePullRequestWithManagerHeldCredential(t *testing.T) {
	machine, store := publicationFixture()
	runtime := &publicationRuntimeFake{runtimeFake: &runtimeFake{}, commit: "0123456789abcdef0123456789abcdef01234567"}
	github := &publicationGitHubFake{created: &githubclient.PullRequest{Number: 12, HTMLURL: "https://github.com/acme/app/pull/12"}}
	manager := &Manager{store: store, runtime: runtime, github: github, encryptionKey: make([]byte, 32)}
	manager.SetAgentGitIdentity(GitIdentity{Name: "carbninai", Email: "agent@example.invalid"})
	require.NoError(t, manager.publishAgent(context.Background(), machine, &domain.DevMachineOperation{}))
	require.Equal(t, 1, store.started)
	require.Equal(t, "fixture-installation-token", runtime.request.Token)
	require.Equal(t, "carbninai", runtime.request.AuthorName)
	require.Empty(t, runtime.request.Lease, "a fast-forward pushes without a lease")
	require.NotContains(t, runtime.request.Message, "fixture-installation-token")
	require.True(t, strings.HasPrefix(runtime.request.Message, "REC-1: Dark mode"))
	require.Equal(t, "REC-1: Dark mode", github.title, "the issue identifier links the PR for existing GitHub automation")
	require.Equal(t, "kuayle/rec-1", github.head)
	require.Equal(t, "main", github.base)
	require.NotContains(t, github.body, "maintainers", "the agent's final message never reaches the pull request")
	require.Equal(t, "## Description\nAddresses REC-1: Dark mode.\n\n## Verification\nKuayle ran `npm test` independently in a clean container: passed.\n", github.body,
		"without the agent's file the description is the issue and Kuayle's check")
	for _, dump := range []string{"src/text.js", "4916a03", "Codex", "deepseek-flash", "Kuayle agent task"} {
		require.NotContains(t, github.body, dump, "no file or commit lists, provider, model or task boilerplate")
	}
	require.False(t, github.draft, "a validated change opens ready for review")
	require.Equal(t, &domain.AgentPublicationResult{Branch: "kuayle/rec-1", Commit: runtime.commit, PullRequestNumber: 12, PullRequestURL: "https://github.com/acme/app/pull/12"}, store.result)
}

func TestPublishAgentWithoutCheckOpensADraft(t *testing.T) {
	machine, store := publicationFixture()
	store.publication.CheckArgv, store.publication.CheckExitCode = nil, nil
	runtime := &publicationRuntimeFake{runtimeFake: &runtimeFake{}, commit: "0123456789abcdef0123456789abcdef01234567"}
	github := &publicationGitHubFake{created: &githubclient.PullRequest{Number: 12, HTMLURL: "https://github.com/acme/app/pull/12"}}
	manager := &Manager{store: store, runtime: runtime, github: github, encryptionKey: make([]byte, 32)}
	require.NoError(t, manager.publishAgent(context.Background(), machine, &domain.DevMachineOperation{}))
	require.True(t, github.draft, "the draft says the change is unchecked; the description carries no warning")
	require.Equal(t, "## Description\nAddresses REC-1: Dark mode.\n", github.body)
	require.Equal(t, 12, store.result.PullRequestNumber)
}

func TestPublishAgentFromStoppedTask(t *testing.T) {
	for _, tc := range []struct {
		name                     string
		dirty, resumed, existing bool
		published                string
		ahead                    *int
		empty                    bool
	}{
		{name: "uncommitted changes", dirty: true},
		{name: "clean at base", ahead: ptr(0), empty: true},
		{name: "local commit before first publication", ahead: ptr(1)},
		{name: "local commit after publication", published: strings.Repeat("b", 40), ahead: ptr(2)},
		{name: "head already published", published: strings.Repeat("a", 40), ahead: ptr(1), empty: true},
		{name: "base comparison unavailable"},
		{name: "restart after making the commit", resumed: true},
		{name: "existing ready pull request", dirty: true, existing: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			machine, store := publicationFixture()
			store.publication.FromStoppedTask = true
			store.publication.PublishedCommit = tc.published
			if tc.resumed {
				store.publication.Step.Status = domain.DevMachineAgentRunStepStatusRunning
			}
			runtime := &publicationRuntimeFake{runtimeFake: &runtimeFake{}, dirty: tc.dirty, ahead: tc.ahead, commit: strings.Repeat("a", 40)}
			github := &publicationGitHubFake{created: &githubclient.PullRequest{Number: 12, HTMLURL: "https://github.com/acme/app/pull/12"}}
			if tc.existing {
				github.existing = &githubclient.PullRequest{Number: 12, HTMLURL: github.created.HTMLURL, NodeID: "PR_node"}
			}
			manager := &Manager{store: store, runtime: runtime, github: github, encryptionKey: make([]byte, 32)}
			require.NoError(t, manager.publishAgent(context.Background(), machine, &domain.DevMachineOperation{}))
			if tc.empty {
				require.Equal(t, &domain.AgentPublicationResult{}, store.result)
				require.Zero(t, runtime.calls)
				require.Zero(t, runtime.syncs)
				require.Zero(t, github.creates)
				return
			}
			require.Equal(t, runtime.commit, store.result.Commit)
			require.Equal(t, 1, runtime.syncs)
			require.Equal(t, tc.published, runtime.syncPublished)
			require.Equal(t, 1, runtime.calls)
			require.Contains(t, github.body, "not validated: published from a failed task")
			require.NotContains(t, github.body, "passed.")
			require.Empty(t, github.readied)
			if tc.existing {
				require.Equal(t, []string{"PR_node"}, github.drafted)
				require.Zero(t, github.creates)
			} else {
				require.Equal(t, 1, github.creates)
				require.True(t, github.draft)
			}
		})
	}
}

func TestStoppedPublicationRefusesUnreadableStatus(t *testing.T) {
	machine, store := publicationFixture()
	store.publication.FromStoppedTask = true
	runtime := &publicationRuntimeFake{runtimeFake: &runtimeFake{}, scanErr: errors.New("unreadable index")}
	manager := &Manager{store: store, runtime: runtime}
	err := manager.publishAgent(context.Background(), machine, &domain.DevMachineOperation{})
	require.ErrorContains(t, err, domain.StoppedPublicationUnavailable)
	require.Nil(t, store.result)
	require.Zero(t, runtime.syncs)
}

func TestPublishAgentRefreshesReusedPullRequestDescription(t *testing.T) {
	machine, store := publicationFixture()
	runtime := &publicationRuntimeFake{runtimeFake: &runtimeFake{}, commit: "0123456789abcdef0123456789abcdef01234567"}
	github := &publicationGitHubFake{existing: &githubclient.PullRequest{Number: 3, Draft: true, HTMLURL: "https://github.com/acme/app/pull/3"}}
	manager := &Manager{store: store, runtime: runtime, github: github, encryptionKey: make([]byte, 32)}
	require.NoError(t, manager.publishAgent(context.Background(), machine, &domain.DevMachineOperation{}))
	require.Equal(t, 1, github.updates, "a later attempt describes itself")
	require.Contains(t, github.body, "independently in a clean container: passed.")
}

func TestPublishAgentReusesOpenPullRequestOnRetry(t *testing.T) {
	machine, store := publicationFixture()
	store.publication.Step.Status = "running"
	runtime := &publicationRuntimeFake{runtimeFake: &runtimeFake{}, commit: "0123456789abcdef0123456789abcdef01234567"}
	github := &publicationGitHubFake{existing: &githubclient.PullRequest{Number: 3, HTMLURL: "https://github.com/acme/app/pull/3"}}
	manager := &Manager{store: store, runtime: runtime, github: github, encryptionKey: make([]byte, 32)}
	require.NoError(t, manager.publishAgent(context.Background(), machine, &domain.DevMachineOperation{}))
	require.Zero(t, github.creates)
	require.Equal(t, 3, store.result.PullRequestNumber)
	require.Equal(t, defaultAgentGitIdentity.Email, runtime.request.AuthorEmail)
}

func TestPublishAgentOutcomesNeverClaimUnrecordedReviewArtifacts(t *testing.T) {
	t.Run("no_changes", func(t *testing.T) {
		machine, store := publicationFixture()
		github := &publicationGitHubFake{}
		manager := &Manager{store: store, runtime: &publicationRuntimeFake{runtimeFake: &runtimeFake{}}, github: github, encryptionKey: make([]byte, 32)}
		require.NoError(t, manager.publishAgent(context.Background(), machine, &domain.DevMachineOperation{}))
		require.Equal(t, &domain.AgentPublicationResult{}, store.result)
		require.Zero(t, github.creates)
	})
	t.Run("foreign_pull_request_url", func(t *testing.T) {
		machine, store := publicationFixture()
		github := &publicationGitHubFake{created: &githubclient.PullRequest{Number: 1, HTMLURL: "https://github.com/other/repo/pull/1"}}
		manager := &Manager{store: store, runtime: &publicationRuntimeFake{runtimeFake: &runtimeFake{}, commit: "0123456789abcdef0123456789abcdef01234567"}, github: github, encryptionKey: make([]byte, 32)}
		err := manager.publishAgent(context.Background(), machine, &domain.DevMachineOperation{})
		var terminal *terminalOperationError
		require.ErrorAs(t, err, &terminal)
		require.Nil(t, store.result)
	})
	t.Run("push_error_is_redacted", func(t *testing.T) {
		machine, store := publicationFixture()
		runtime := &publicationRuntimeFake{runtimeFake: &runtimeFake{}, err: errors.New("push to https://x-access-token:fixture-installation-token@github.com failed")}
		manager := &Manager{store: store, runtime: runtime, github: &publicationGitHubFake{}, encryptionKey: make([]byte, 32)}
		err := manager.publishAgent(context.Background(), machine, &domain.DevMachineOperation{})
		require.Error(t, err)
		require.NotContains(t, err.Error(), "fixture-installation-token")
		require.Nil(t, store.result)
	})
	t.Run("finished_step_is_not_republished", func(t *testing.T) {
		machine, store := publicationFixture()
		store.publication.Step.Status = "succeeded"
		runtime := &publicationRuntimeFake{runtimeFake: &runtimeFake{}}
		manager := &Manager{store: store, runtime: runtime, github: &publicationGitHubFake{}, encryptionKey: make([]byte, 32)}
		require.NoError(t, manager.publishAgent(context.Background(), machine, &domain.DevMachineOperation{}))
		require.Zero(t, runtime.calls)
		require.Zero(t, store.started)
	})
	t.Run("foreign_checkout", func(t *testing.T) {
		machine, store := publicationFixture()
		store.checkout.ScopeKey = ""
		runtime := &publicationRuntimeFake{runtimeFake: &runtimeFake{}}
		manager := &Manager{store: store, runtime: runtime, github: &publicationGitHubFake{}, encryptionKey: make([]byte, 32)}
		var terminal *terminalOperationError
		require.ErrorAs(t, manager.publishAgent(context.Background(), machine, &domain.DevMachineOperation{}), &terminal)
		require.Zero(t, runtime.calls)
	})
}

// KUA-176: nothing new is published, but GitHub's task branch still holds the
// earlier work the agent removed. The open pull request is named so the task
// can say why it stopped; it is never created, updated or readied.
func TestPublishAgentNamesTheOpenPullRequestWhenRemovedWorkRemains(t *testing.T) {
	machine, store := publicationFixture()
	runtime := &publicationRuntimeFake{runtimeFake: &runtimeFake{}, err: ErrPublishedWorkRemains}
	github := &publicationGitHubFake{existing: &githubclient.PullRequest{Number: 12, HTMLURL: "https://github.com/acme/app/pull/12"}}
	manager := &Manager{store: store, runtime: runtime, github: github, encryptionKey: make([]byte, 32)}
	require.NoError(t, manager.publishAgent(context.Background(), machine, &domain.DevMachineOperation{}))
	require.Equal(t, &domain.AgentPublicationResult{Branch: "kuayle/rec-1", Remains: true, PullRequestURL: "https://github.com/acme/app/pull/12"}, store.result)
	require.Equal(t, "kuayle/rec-1", github.head)
	require.Equal(t, "main", github.base)
	require.Zero(t, github.creates, "the earlier work is already published")
	require.Zero(t, github.updates, "a remaining branch is not described afresh")
	require.Empty(t, github.readied)
}

// KUA-176: nothing new is published and no pull request is open, so nothing the
// task published is in review any more (for example the pull request was
// squash-merged and closed and the old branch now conflicts with the base). That
// is the same outcome as a run with nothing to publish, not a task to block.
func TestPublishAgentWithoutOpenPullRequestHasNothingToPublish(t *testing.T) {
	machine, store := publicationFixture()
	runtime := &publicationRuntimeFake{runtimeFake: &runtimeFake{}, err: ErrPublishedWorkRemains}
	github := &publicationGitHubFake{}
	manager := &Manager{store: store, runtime: runtime, github: github, encryptionKey: make([]byte, 32)}
	require.NoError(t, manager.publishAgent(context.Background(), machine, &domain.DevMachineOperation{}))
	require.Equal(t, &domain.AgentPublicationResult{}, store.result)
	require.Zero(t, github.creates, "a branch that only holds removed work opens no pull request")
	require.Zero(t, github.updates, "nothing is described afresh")
	require.Empty(t, github.readied)
}

func TestPublishAgentFailsWhenTheRemainingPullRequestCannotBeFound(t *testing.T) {
	machine, store := publicationFixture()
	runtime := &publicationRuntimeFake{runtimeFake: &runtimeFake{}, err: ErrPublishedWorkRemains}
	github := &publicationGitHubFake{err: errors.New("github unavailable for fixture-installation-token")}
	manager := &Manager{store: store, runtime: runtime, github: github, encryptionKey: make([]byte, 32)}
	err := manager.publishAgent(context.Background(), machine, &domain.DevMachineOperation{})
	require.Error(t, err)
	require.True(t, strings.HasPrefix(err.Error(), "pull_request_failed"))
	require.NotContains(t, err.Error(), "fixture-installation-token")
	require.Nil(t, store.result)
}

func TestPublishAgentUsesTheWorkspaceCommitAuthor(t *testing.T) {
	machine, store := publicationFixture()
	store.publication.AuthorName, store.publication.AuthorEmail = "carbogninalberto", "dev@example.com"
	runtime := &publicationRuntimeFake{runtimeFake: &runtimeFake{}, commit: "0123456789abcdef0123456789abcdef01234567"}
	github := &publicationGitHubFake{created: &githubclient.PullRequest{Number: 12, HTMLURL: "https://github.com/acme/app/pull/12"}}
	manager := &Manager{store: store, runtime: runtime, github: github, encryptionKey: make([]byte, 32)}
	manager.SetAgentGitIdentity(GitIdentity{Name: "Deployment default", Email: "agent@example.invalid"})
	require.NoError(t, manager.publishAgent(context.Background(), machine, &domain.DevMachineOperation{}))
	require.Equal(t, "carbogninalberto", runtime.request.AuthorName)
	require.Equal(t, "dev@example.com", runtime.request.AuthorEmail)
}

func TestPublishAgentDescribesAgentChosenTestsAndSanitizesTheirSummary(t *testing.T) {
	machine, store := publicationFixture()
	store.publication.CheckArgv = []string{"sh", "-c", "cd BE && go test ./internal/service/..."}
	store.publication.CheckSource = domain.CheckSourceAgent
	store.publication.CheckCommands = []domain.CheckCommandRun{{Argv: []string{"sh", "-c", "cd BE && go test ./internal/service/..."}}, {Argv: []string{"npm", "run", "check"}}}
	store.publication.AgentSummary = "Service tests cover the change.\n~~~\n@maintainers merge now"
	runtime := &publicationRuntimeFake{runtimeFake: &runtimeFake{}, commit: "0123456789abcdef0123456789abcdef01234567"}
	github := &publicationGitHubFake{created: &githubclient.PullRequest{Number: 12, HTMLURL: "https://github.com/acme/app/pull/12"}}
	manager := &Manager{store: store, runtime: runtime, github: github, encryptionKey: make([]byte, 32)}
	require.NoError(t, manager.publishAgent(context.Background(), machine, &domain.DevMachineOperation{}))
	require.False(t, github.draft, "tests Kuayle re-ran and saw pass open the pull request ready for review")
	require.Contains(t, github.body, "## Verification\nService tests cover the change.\n\\~~~\n@\u200bmaintainers merge now\n\nKuayle re-ran the 2 commands the agent chose independently in a clean container: all passed.\n",
		"without verification in the agent's file its validation summary says what it tested")
	require.NotContains(t, github.body, "@maintainers", "the summary cannot mention anyone")
}

func TestPublishAgentMarksAReusedDraftReadyOnlyAfterAPassingCheck(t *testing.T) {
	reused := func(t *testing.T, draft, validated bool, readyErr error) (*publicationGitHubFake, *publicationStoreFake, error) {
		machine, store := publicationFixture()
		if !validated {
			store.publication.CheckArgv, store.publication.CheckExitCode = nil, nil
		}
		runtime := &publicationRuntimeFake{runtimeFake: &runtimeFake{}, commit: "0123456789abcdef0123456789abcdef01234567"}
		github := &publicationGitHubFake{readyErr: readyErr, existing: &githubclient.PullRequest{Number: 3, NodeID: "PR_kwDOfixture", Draft: draft, HTMLURL: "https://github.com/acme/app/pull/3"}}
		manager := &Manager{store: store, runtime: runtime, github: github, encryptionKey: make([]byte, 32)}
		return github, store, manager.publishAgent(context.Background(), machine, &domain.DevMachineOperation{})
	}
	t.Run("validated_draft_becomes_ready", func(t *testing.T) {
		github, store, err := reused(t, true, true, nil)
		require.NoError(t, err)
		require.Equal(t, []string{"PR_kwDOfixture"}, github.readied)
		require.Equal(t, 1, github.updates)
		require.Equal(t, 3, store.result.PullRequestNumber)
	})
	t.Run("unvalidated_draft_stays_draft", func(t *testing.T) {
		github, store, err := reused(t, true, false, nil)
		require.NoError(t, err)
		require.Empty(t, github.readied)
		require.Equal(t, 3, store.result.PullRequestNumber)
	})
	t.Run("ready_pull_request_is_left_alone", func(t *testing.T) {
		// A later unvalidated attempt does not turn a ready pull request back
		// into a draft.
		for _, validated := range []bool{true, false} {
			github, _, err := reused(t, false, validated, nil)
			require.NoError(t, err)
			require.Empty(t, github.readied)
		}
	})
	t.Run("ready_error_fails_the_pull_request_step_redacted", func(t *testing.T) {
		_, store, err := reused(t, true, true, errors.New("GraphQL refused fixture-installation-token"))
		require.ErrorContains(t, err, "pull_request_failed: ")
		require.NotContains(t, err.Error(), "fixture-installation-token")
		require.Nil(t, store.result)
	})
}

func TestPublishAgentSyncsTheCheckoutFirstAndStopsWhenItCannotFastForward(t *testing.T) {
	machine, store := publicationFixture()
	base := &runtimeFake{syncErr: errors.New("cannot be fast-forwarded and nothing was force-pushed")}
	runtime := &publicationRuntimeFake{runtimeFake: base, commit: "0123456789abcdef0123456789abcdef01234567"}
	github := &publicationGitHubFake{created: &githubclient.PullRequest{Number: 12, HTMLURL: "https://github.com/acme/app/pull/12"}}
	manager := &Manager{store: store, runtime: runtime, github: github, encryptionKey: make([]byte, 32)}
	err := manager.publishAgent(context.Background(), machine, &domain.DevMachineOperation{})
	require.ErrorContains(t, err, "checkout_sync_failed")
	require.ErrorContains(t, err, "nothing was force-pushed")
	require.Equal(t, 1, base.syncs)
	require.Zero(t, runtime.calls, "nothing is committed or pushed over commits GitHub has")
	require.Nil(t, store.result)

	machine, store = publicationFixture()
	base = &runtimeFake{}
	runtime = &publicationRuntimeFake{runtimeFake: base, commit: "0123456789abcdef0123456789abcdef01234567"}
	manager = &Manager{store: store, runtime: runtime, github: github, encryptionKey: make([]byte, 32)}
	require.NoError(t, manager.publishAgent(context.Background(), machine, &domain.DevMachineOperation{}))
	require.Equal(t, 1, base.syncs)
	require.Equal(t, 1, runtime.calls)
	require.Empty(t, runtime.request.Lease)
}

func TestPublishSyncPreservesPendingWorkWhenTheTaskBranchMoves(t *testing.T) {
	f := newScriptFixture(t)
	base := "first\n" + strings.Repeat("context\n", 10) + "last\n"
	published := f.writeAndCommit(f.checkout, "notes.md", base, 0o644, kuayleIdentity)
	f.run(f.checkout, nil, "push", "-q", f.origin, "kuayle/rec-1")
	maintainer := filepath.Join(f.root, "maintainer")
	f.run(f.root, nil, "clone", "-q", "-b", "kuayle/rec-1", f.origin, maintainer)
	upstream := f.writeAndCommit(maintainer, "notes.md", strings.Replace(base, "first", "maintainer", 1), 0o644, fixtureIdentity)
	f.run(maintainer, nil, "push", "-q", f.origin, "kuayle/rec-1")
	require.NoError(t, os.WriteFile(filepath.Join(f.checkout, "notes.md"), []byte(strings.Replace(base, "last", "agent", 1)), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(f.checkout, "new.md"), []byte("new agent file\n"), 0o644))
	f.run(f.checkout, nil, "add", "new.md")
	require.NoError(t, os.WriteFile(filepath.Join(f.checkout, "untracked.md"), []byte("untracked agent file\n"), 0o644))

	output, err := f.sync(published)
	require.NoError(t, err, output)
	require.Empty(t, output, "publication can push normally without a force lease")
	commit, err := f.commit(published)
	require.NoError(t, err, commit)
	require.False(t, f.fails(f.checkout, "merge-base", "--is-ancestor", upstream, commit))
	output, err = f.push(commit, "")
	require.NoError(t, err, output)
	require.Equal(t, commit, f.originTip())
	require.Equal(t, "maintainer\n"+strings.Repeat("context\n", 10)+"agent", f.run(f.origin, nil, "show", commit+":notes.md"))
	require.Equal(t, "new agent file", f.run(f.origin, nil, "show", commit+":new.md"))
	require.Equal(t, "untracked agent file", f.run(f.origin, nil, "show", commit+":untracked.md"))
}

func TestPublishSyncNamesConflictsAndCanContinueAfterTheyAreResolved(t *testing.T) {
	f := newScriptFixture(t)
	f.commitFile(f.checkout, "notes.md", kuayleIdentity)
	published := f.commitFile(f.checkout, "release notes.md", kuayleIdentity)
	f.run(f.checkout, nil, "push", "-q", f.origin, "kuayle/rec-1")
	maintainer := filepath.Join(f.root, "maintainer")
	f.run(f.root, nil, "clone", "-q", "-b", "kuayle/rec-1", f.origin, maintainer)
	f.writeAndCommit(maintainer, "notes.md", "maintainer\n", 0o644, fixtureIdentity)
	upstream := f.writeAndCommit(maintainer, "release notes.md", "maintainer\n", 0o644, fixtureIdentity)
	f.run(maintainer, nil, "push", "-q", f.origin, "kuayle/rec-1")
	require.NoError(t, os.WriteFile(filepath.Join(f.checkout, "notes.md"), []byte("agent\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(f.checkout, "release notes.md"), []byte("agent\n"), 0o644))

	output, err := f.sync(published)
	require.NoError(t, err, output)
	_, conflict := parseSyncLease(output)
	require.ErrorContains(t, conflict, "notes.md, release notes.md")
	checkpoint := f.head()
	require.Equal(t, "agent", f.run(f.checkout, nil, "show", "HEAD:notes.md"))
	require.Equal(t, "notes.md\nrelease notes.md", f.run(f.checkout, nil, "diff", "--name-only", "--diff-filter=U"))
	require.Equal(t, upstream, f.originTip())

	retry, err := f.sync(published)
	require.NoError(t, err, retry)
	_, retryErr := parseSyncLease(retry)
	require.EqualError(t, retryErr, conflict.Error())
	require.Equal(t, checkpoint, f.head(), "an unresolved retry leaves the saved work in place")

	// Resolve by keeping the local version. This stages no content change against
	// the saved commit, but publication still needs to conclude the merge.
	require.NoError(t, os.WriteFile(filepath.Join(f.checkout, "notes.md"), []byte("agent\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(f.checkout, "release notes.md"), []byte("agent\n"), 0o644))
	f.run(f.checkout, nil, "add", "notes.md", "release notes.md")
	output, err = f.sync(published)
	require.NoError(t, err, output)
	lease, err := parseSyncLease(output)
	require.NoError(t, err)
	require.Empty(t, lease)
	commit, err := f.commit(published)
	require.NoError(t, err, commit)
	require.False(t, f.fails(f.checkout, "merge-base", "--is-ancestor", upstream, commit))
	require.Equal(t, "agent", f.run(f.checkout, nil, "show", commit+":notes.md"))
	output, err = f.push(commit, "")
	require.NoError(t, err, output)
	require.Equal(t, commit, f.originTip())
}

func TestPublishAgentDoesNotRetryAnUnresolvedCheckout(t *testing.T) {
	_, store := publicationFixture()
	_, err := parseSyncLease("conflicts\nnotes.md\nfixture-installation-token.md")
	base := &runtimeFake{syncErr: err}
	runtime := &publicationRuntimeFake{runtimeFake: base}
	manager := NewManager(store, runtime, nil, &publicationGitHubFake{}, make([]byte, 32), nil, "fixture")
	manager.operationSlots <- struct{}{}
	manager.operations.Add(1)
	manager.processLeasedOperation(context.Background(), domain.DevMachineOperation{Action: domain.DevMachineOpPublishAgent})
	require.NotNil(t, store.failedOperationRetryable)
	require.False(t, *store.failedOperationRetryable)
	require.Equal(t, "checkout_sync_blocked", store.failedOperationCode)
	require.Contains(t, store.failedOperationMessage, "notes.md")
	require.Contains(t, store.failedOperationMessage, "[redacted].md")
	require.NotContains(t, store.failedOperationMessage, "fixture-installation-token")
	require.Equal(t, 1, base.syncs)
	require.Zero(t, runtime.calls)
}

// The sync sees Kuayle's recorded published commit, so it can tell the agent's
// own rewrite from someone else's push, and its lease guards the push.
func TestPublishAgentLeasesTheCommitItPublishedWhenTheAgentRewroteIt(t *testing.T) {
	machine, store := publicationFixture()
	store.publication.PublishedCommit = "1111111111111111111111111111111111111111"
	lease := "2222222222222222222222222222222222222222"
	base := &runtimeFake{lease: lease}
	runtime := &publicationRuntimeFake{runtimeFake: base, commit: "0123456789abcdef0123456789abcdef01234567"}
	github := &publicationGitHubFake{created: &githubclient.PullRequest{Number: 12, HTMLURL: "https://github.com/acme/app/pull/12"}}
	manager := &Manager{store: store, runtime: runtime, github: github, encryptionKey: make([]byte, 32)}
	require.NoError(t, manager.publishAgent(context.Background(), machine, &domain.DevMachineOperation{}))
	require.Equal(t, "1111111111111111111111111111111111111111", base.syncPublished)
	require.Equal(t, lease, runtime.request.Lease)
	require.Equal(t, "1111111111111111111111111111111111111111", runtime.request.PublishedCommit)
}

// GitHub shows the description as the bot's first comment, so it starts with
// the workspace's bot name and picture like the bot's comments do.
func TestPublishAgentDescriptionStartsWithBotSignature(t *testing.T) {
	machine, store := publicationFixture()
	store.publication.CheckArgv, store.publication.CheckExitCode = nil, nil
	store.publication.Bot = &domain.GitHubBotSettings{WorkspaceID: machine.WorkspaceID, DisplayName: ptr("Acme"), AvatarURL: ptr("https://img.test/bot.png")}
	runtime := &publicationRuntimeFake{runtimeFake: &runtimeFake{}, commit: "0123456789abcdef0123456789abcdef01234567"}
	github := &publicationGitHubFake{created: &githubclient.PullRequest{Number: 12, HTMLURL: "https://github.com/acme/app/pull/12"}}
	manager := &Manager{store: store, runtime: runtime, github: github, encryptionKey: make([]byte, 32)}
	manager.SetFrontendURL("https://kuayle.test")
	require.NoError(t, manager.publishAgent(context.Background(), machine, &domain.DevMachineOperation{}))
	require.True(t, strings.HasPrefix(github.body, "<img src=\"https://img.test/bot.png\" alt=\"\" width=\"20\" height=\"20\" align=\"top\"> **Acme**\n\n## Description\n"), github.body)

	// An uploaded picture is linked from Kuayle's public address.
	machine, store = publicationFixture()
	file := uuid.NewString() + ".png"
	store.publication.Bot = &domain.GitHubBotSettings{WorkspaceID: machine.WorkspaceID, AvatarFile: &file}
	github = &publicationGitHubFake{existing: &githubclient.PullRequest{Number: 3, HTMLURL: "https://github.com/acme/app/pull/3"}}
	manager = &Manager{store: store, runtime: runtime, github: github, encryptionKey: make([]byte, 32)}
	manager.SetFrontendURL("https://kuayle.test/")
	require.NoError(t, manager.publishAgent(context.Background(), machine, &domain.DevMachineOperation{}))
	require.True(t, strings.HasPrefix(github.body, `<img src="https://kuayle.test/api/github-bot/`+machine.WorkspaceID.String()+`/avatar/`+file+`"`), github.body)
}

// KUA-210: the agent describes its change in .kuayle/pull_request.json and
// leaves screenshots in .kuayle/screenshots/. Kuayle validates the images,
// commits them to a branch of their own (never the pull request's) and links
// them from the description.
func TestPublishAgentDescribesTheChangeFromTheAgentsFileWithScreenshots(t *testing.T) {
	machine, store := publicationFixture()
	png := []byte("\x89PNG\r\n\x1a\nfixture")
	runtime := &publicationRuntimeFake{runtimeFake: &runtimeFake{}, commit: "0123456789abcdef0123456789abcdef01234567", files: map[string][]byte{
		"pull_request.json": []byte(`{"description":"The toggle ignored the system theme.","changes":["Follow the system theme"],"verification":["npm run test:unit: passed"],
			"screenshots":[{"file":"dark.png","caption":"Settings, dark"},"fake.png","missing.webp"]}`),
		"screenshots/dark.png": png,
		"screenshots/fake.png": []byte("<svg onload=alert(1)>"),
	}}
	github := &publicationGitHubFake{created: &githubclient.PullRequest{Number: 12, HTMLURL: "https://github.com/acme/app/pull/12"}}
	manager := &Manager{store: store, runtime: runtime, github: github, encryptionKey: make([]byte, 32)}
	require.NoError(t, manager.publishAgent(context.Background(), machine, &domain.DevMachineOperation{}))
	taskID := *store.agentRun.TaskID
	require.Equal(t, "kuayle/screenshots/"+taskID.String(), github.screenshotBranch, "screenshots never land on the pull request branch")
	require.Equal(t, []githubclient.RepositoryFile{{Path: "dark.png", Content: png}}, github.screenshotFiles, "only real images are published")
	require.Equal(t, "## Description\nAddresses REC-1: Dark mode.\n\nThe toggle ignored the system theme.\n\n"+
		"## Changes\n- Follow the system theme\n\n"+
		"## Verification\n- npm run test:unit: passed\n\nKuayle also ran `npm test` independently in a clean container: passed.\n\n"+
		"## Screenshots\nSettings, dark\n![Settings, dark](https://github.com/acme/app/blob/"+strings.Repeat("c", 40)+"/dark.png?raw=true)\n", github.body)
	require.Equal(t, 12, store.result.PullRequestNumber)
}

func TestPublishAgentStillPublishesWhenItsFilesFail(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\nfixture")
	t.Run("screenshots_refused", func(t *testing.T) {
		machine, store := publicationFixture()
		runtime := &publicationRuntimeFake{runtimeFake: &runtimeFake{}, commit: "0123456789abcdef0123456789abcdef01234567", files: map[string][]byte{
			"pull_request.json": []byte(`{"description":"Fix.","screenshots":["a.png"]}`), "screenshots/a.png": png,
		}}
		github := &publicationGitHubFake{screenshotErr: errors.New("403 for fixture-installation-token"), created: &githubclient.PullRequest{Number: 12, HTMLURL: "https://github.com/acme/app/pull/12"}}
		manager := &Manager{store: store, runtime: runtime, github: github, encryptionKey: make([]byte, 32)}
		require.NoError(t, manager.publishAgent(context.Background(), machine, &domain.DevMachineOperation{}))
		require.Contains(t, github.body, "Fix.")
		require.NotContains(t, github.body, "## Screenshots")
		require.Equal(t, 12, store.result.PullRequestNumber)
	})
	t.Run("rejected_file", func(t *testing.T) {
		machine, store := publicationFixture()
		runtime := &publicationRuntimeFake{runtimeFake: &runtimeFake{}, commit: "0123456789abcdef0123456789abcdef01234567", files: map[string][]byte{
			"pull_request.json": []byte(`{"description":["not","text"],"screenshots":["a.png"]}`), "screenshots/a.png": png,
		}}
		github := &publicationGitHubFake{created: &githubclient.PullRequest{Number: 12, HTMLURL: "https://github.com/acme/app/pull/12"}}
		manager := &Manager{store: store, runtime: runtime, github: github, encryptionKey: make([]byte, 32)}
		require.NoError(t, manager.publishAgent(context.Background(), machine, &domain.DevMachineOperation{}))
		require.Equal(t, "## Description\nAddresses REC-1: Dark mode.\n\n## Verification\nKuayle ran `npm test` independently in a clean container: passed.\n", github.body)
		require.Empty(t, github.screenshotFiles, "a rejected file publishes nothing")
	})
	t.Run("nothing_published_reads_nothing", func(t *testing.T) {
		machine, store := publicationFixture()
		runtime := &publicationRuntimeFake{runtimeFake: &runtimeFake{}, files: map[string][]byte{"pull_request.json": []byte(`{"screenshots":["a.png"]}`), "screenshots/a.png": png}}
		github := &publicationGitHubFake{}
		manager := &Manager{store: store, runtime: runtime, github: github, encryptionKey: make([]byte, 32)}
		require.NoError(t, manager.publishAgent(context.Background(), machine, &domain.DevMachineOperation{}))
		require.Empty(t, runtime.reads)
		require.Empty(t, github.screenshotFiles)
	})
}
