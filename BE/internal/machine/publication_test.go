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
	title    string
	body     string
	head     string
	base     string
}

func (f *publicationGitHubFake) GetRepositoryInstallationToken(int64, string) (string, time.Time, error) {
	return "fixture-installation-token", time.Now().Add(time.Hour), nil
}
func (f *publicationGitHubFake) FindOpenPullRequest(_, _, _, head, base string) (*githubclient.PullRequest, error) {
	f.head, f.base = head, base
	return f.existing, f.err
}
func (f *publicationGitHubFake) CreatePullRequest(_, _, _, title, head, base, body string) (*githubclient.PullRequest, error) {
	f.creates++
	f.title, f.body = title, body
	return f.created, nil
}

func publicationFixture() (*domain.DevMachine, *publicationStoreFake) {
	taskID, checkoutID, runID := uuid.New(), uuid.New(), uuid.New()
	machine := &domain.DevMachine{ID: uuid.New(), WorkspaceID: uuid.New(), ExpiresAt: time.Now().Add(time.Hour)}
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
	require.Equal(t, &domain.AgentPublicationResult{Branch: "kuayle/rec-1", Commit: runtime.commit, PullRequestNumber: 12, PullRequestURL: "https://github.com/acme/app/pull/12"}, store.result)
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
