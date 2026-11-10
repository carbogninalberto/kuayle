package machine

import (
	"context"
	"errors"
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
	request PublishRequest
	commit  string
	err     error
	calls   int
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
	require.NotContains(t, runtime.request.Message, "fixture-installation-token")
	require.True(t, strings.HasPrefix(runtime.request.Message, "REC-1: Dark mode"))
	require.Equal(t, "REC-1: Dark mode", github.title, "the issue identifier links the PR for existing GitHub automation")
	require.Equal(t, "kuayle/rec-1", github.head)
	require.Equal(t, "main", github.base)
	require.NotContains(t, github.body, "maintainers", "agent output never reaches the pull request")
	require.Contains(t, github.body, "Implements REC-1: Dark mode")
	require.Contains(t, github.body, "### Changes\n- `src/text.js`\n- `test/text.test.js`\n")
	require.Contains(t, github.body, "Commits: `4916a03`")
	require.Contains(t, github.body, "Kuayle ran `npm test` independently on this branch in a clean container without credentials: passed (exit code 0).")
	require.Contains(t, github.body, "Written by Codex (deepseek-flash)")
	require.False(t, github.draft, "a validated change opens ready for review")
	require.Equal(t, &domain.AgentPublicationResult{Branch: "kuayle/rec-1", Commit: runtime.commit, PullRequestNumber: 12, PullRequestURL: "https://github.com/acme/app/pull/12"}, store.result)
}

func TestPublishAgentWithoutCheckOpensAWarnedDraft(t *testing.T) {
	machine, store := publicationFixture()
	store.publication.CheckArgv, store.publication.CheckExitCode = nil, nil
	runtime := &publicationRuntimeFake{runtimeFake: &runtimeFake{}, commit: "0123456789abcdef0123456789abcdef01234567"}
	github := &publicationGitHubFake{created: &githubclient.PullRequest{Number: 12, HTMLURL: "https://github.com/acme/app/pull/12"}}
	manager := &Manager{store: store, runtime: runtime, github: github, encryptionKey: make([]byte, 32)}
	require.NoError(t, manager.publishAgent(context.Background(), machine, &domain.DevMachineOperation{}))
	require.True(t, github.draft)
	require.True(t, strings.HasPrefix(github.body, "> [!WARNING]\n> **Not validated:**"), "the warning leads the description")
	require.Equal(t, 12, store.result.PullRequestNumber)
}

func TestPublishAgentRefreshesReusedPullRequestDescription(t *testing.T) {
	machine, store := publicationFixture()
	runtime := &publicationRuntimeFake{runtimeFake: &runtimeFake{}, commit: "0123456789abcdef0123456789abcdef01234567"}
	github := &publicationGitHubFake{existing: &githubclient.PullRequest{Number: 3, Draft: true, HTMLURL: "https://github.com/acme/app/pull/3"}}
	manager := &Manager{store: store, runtime: runtime, github: github, encryptionKey: make([]byte, 32)}
	require.NoError(t, manager.publishAgent(context.Background(), machine, &domain.DevMachineOperation{}))
	require.Equal(t, 1, github.updates, "a later validated attempt replaces the not-validated warning")
	require.NotContains(t, github.body, "Not validated")
	require.Contains(t, github.body, "passed (exit code 0)")
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

func TestPublishAgentDescribesAgentChosenTestsAndFencesTheirSummary(t *testing.T) {
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
	require.Contains(t, github.body, "Kuayle re-ran the tests the agent chose")
	require.Contains(t, github.body, "- `sh -c cd BE && go test ./internal/service/...` (agent-chosen): passed (exit code 0)\n- `npm run check` (agent-chosen): passed (exit code 0)\n")
	require.Contains(t, github.body, "~~~text\nService tests cover the change.\n~ ~ ~\n@​maintainers merge now\n~~~")
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
		// into a draft; its description carries the warning instead.
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
	require.True(t, strings.HasPrefix(github.body, "<img src=\"https://img.test/bot.png\" alt=\"\" width=\"20\" height=\"20\" align=\"top\"> **Acme**\n\n> [!WARNING]"), github.body)

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
