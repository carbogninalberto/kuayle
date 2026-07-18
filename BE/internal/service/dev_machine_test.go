package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/agent"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/repository"
	cryptoutil "github.com/kuayle/kuayle-backend/pkg/crypto"
	"github.com/stretchr/testify/require"
)

func TestValidateRepositoryRequiresMatchingGitHubIdentity(t *testing.T) {
	require.NoError(t, validateRepository(dto.DevMachineRepoInput{
		Provider: "github", Owner: "kuayle", Name: ".github", URL: "https://github.com/kuayle/.github.git",
	}))
	require.Error(t, validateRepository(dto.DevMachineRepoInput{
		Provider: "github", Owner: "other", Name: "repo", URL: "https://github.com/kuayle/repo",
	}))
	require.Error(t, validateRepository(dto.DevMachineRepoInput{
		Provider: "github", Owner: "kuayle", Name: "repo", URL: "https://github.com:444/kuayle/repo",
	}))
}

func TestValidGitRefRejectsOptionAndTraversalForms(t *testing.T) {
	for _, ref := range []string{"main", "kuayle/ENG-42", "feature.with-dots"} {
		require.True(t, validGitRef(ref), ref)
	}
	for _, ref := range []string{"--orphan", "../main", "feature..main", "refs/@{upstream}", "feature.lock"} {
		require.False(t, validGitRef(ref), ref)
	}
}

func TestRedactPayloadReplacesNestedSecrets(t *testing.T) {
	payload := map[string]any{"message": "token-secret-value", "nested": []any{"secret-value"}, "secret-value": "key"}
	redacted := redactPayload(payload, []string{"secret-value"}).(map[string]any)
	require.Equal(t, "token-[REDACTED]", redacted["message"])
	require.Equal(t, []any{"[REDACTED]"}, redacted["nested"])
	require.Equal(t, "key", redacted["[REDACTED]"])
}

func TestValidMachineName(t *testing.T) {
	for _, name := range []string{"quiet-orchid-7f3a", "builder-01", "abc"} {
		require.True(t, validMachineName(name), name)
	}
	for _, name := range []string{"ABCD", "two words", "-leading", "trailing-", "a", "with_underscore"} {
		require.False(t, validMachineName(name), name)
	}
}

func TestNameAvailabilityUsesCaseInsensitiveStore(t *testing.T) {
	workspaceID := uuid.New()
	store := &devMachineStoreFake{nameExists: map[string]bool{"builder-01": true}}
	svc := newTestDevMachineService(store)

	available, err := svc.NameAvailable(context.Background(), workspaceID, "builder-02")
	require.NoError(t, err)
	require.True(t, available)

	available, err = svc.NameAvailable(context.Background(), workspaceID, "builder-01")
	require.NoError(t, err)
	require.False(t, available)
}

func TestGenerateNameRetriesCollisions(t *testing.T) {
	store := &devMachineStoreFake{alwaysNameExists: true}
	svc := newTestDevMachineService(store)

	_, err := svc.GenerateName(context.Background(), uuid.New())
	require.ErrorContains(t, err, "unable to allocate")
	require.Equal(t, 20, store.nameChecks)
}

func TestCreateGenericMachineDoesNotRequireRepositoryOrTTL(t *testing.T) {
	workspaceID, userID := uuid.New(), uuid.New()
	store := &devMachineStoreFake{policy: testPolicy(workspaceID)}
	svc := newTestDevMachineService(store)

	machine, operation, err := svc.Create(context.Background(), workspaceID, userID, dto.CreateDevMachineRequest{
		Size:        "small",
		KeepRunning: true,
	})
	require.NoError(t, err)
	require.NotNil(t, operation)
	require.Equal(t, domain.DevMachineOpSpawn, operation.Action)
	require.True(t, validMachineName(machine.Name))
	require.Empty(t, machine.RepoURL)
	require.Empty(t, machine.RepoOwner)
	require.Empty(t, machine.RepoName)
	require.Nil(t, machine.RepositoryAffinityID)
	require.WithinDuration(t, time.Now().UTC().Add(480*time.Minute), machine.ExpiresAt, 5*time.Second)
	require.True(t, store.createdMachine.KeepRunning)
	require.NotContains(t, string(machine.ServicesConfig), "app_preview")
	for _, service := range store.createdServices {
		require.NotEqual(t, "app_preview", service.ServiceType)
	}
}

func TestCreateResolvesScopedRepositoryAndEnvironment(t *testing.T) {
	workspaceID, userID := uuid.New(), uuid.New()
	teamID, projectID, issueID := uuid.New(), uuid.New(), uuid.New()
	repoID, envID := uuid.New(), uuid.New()
	immutableEnvRef := "sha256:scoped-environment"
	store := &devMachineStoreFake{
		policy:       testPolicy(workspaceID),
		issues:       map[uuid.UUID]*domain.Issue{issueID: {ID: issueID, WorkspaceID: workspaceID, TeamID: teamID, ProjectID: &projectID, Identifier: "ENG-42"}},
		projects:     map[uuid.UUID]*domain.Project{projectID: {ID: projectID, WorkspaceID: workspaceID, TeamID: &teamID}},
		reposByID:    map[uuid.UUID]*domain.GitHubRepoModel{repoID: {ID: repoID, WorkspaceID: workspaceID, FullName: "Kuayle/API", DefaultBranch: "main", IsActive: true}},
		environments: map[uuid.UUID]*domain.DevMachineEnvironment{envID: {ID: envID, WorkspaceID: workspaceID, Name: "base", ImageRef: immutableEnvRef, ImageDigest: &immutableEnvRef, Status: "ready"}},
		scopeSettings: map[string]*domain.DevMachineScopeSetting{
			scopeKey(nil, nil, &issueID):   {WorkspaceID: workspaceID, GitHubRepoID: &repoID, BaseBranch: dmStrPtr("issue-base")},
			scopeKey(nil, &projectID, nil): {WorkspaceID: workspaceID, EnvironmentID: &envID},
			scopeKey(nil, nil, nil):        {WorkspaceID: workspaceID, BaseBranch: dmStrPtr("workspace-base")},
			scopeKey(&teamID, nil, nil):    {WorkspaceID: workspaceID, BaseBranch: dmStrPtr("team-base")},
		},
	}
	svc := newTestDevMachineService(store)

	machine, _, err := svc.Create(context.Background(), workspaceID, userID, dto.CreateDevMachineRequest{Size: "small", IssueID: dmStrPtr(issueID.String())})
	require.NoError(t, err)
	require.Equal(t, &projectID, machine.ProjectID)
	require.Equal(t, &issueID, machine.IssueID)
	require.Equal(t, &repoID, machine.RepositoryAffinityID)
	require.Equal(t, &envID, machine.EnvironmentID)
	require.Equal(t, "Kuayle/API", machine.RepoOwner+"/"+machine.RepoName)
	require.Equal(t, "issue-base", machine.BaseBranch)
	require.Equal(t, "kuayle/eng-42", machine.WorkingBranch)
}

func TestCreateUsesImmutableEnvironmentDigestForDeveloperServices(t *testing.T) {
	workspaceID, userID, envID := uuid.New(), uuid.New(), uuid.New()
	immutableID := "sha256:environment-image"
	store := &devMachineStoreFake{
		policy: testPolicy(workspaceID),
		environments: map[uuid.UUID]*domain.DevMachineEnvironment{
			envID: {ID: envID, WorkspaceID: workspaceID, Name: "base", ImageRef: "kuayle/dev-environment-test:snapshot", ImageDigest: &immutableID, Status: "ready"},
		},
	}
	svc := newTestDevMachineService(store)

	_, _, err := svc.Create(context.Background(), workspaceID, userID, dto.CreateDevMachineRequest{Size: "small", EnvironmentID: dmStrPtr(envID.String())})
	require.NoError(t, err)

	developerImages := map[string]string{}
	for _, service := range store.createdServices {
		if service.ServiceType == "ide" || service.ServiceType == "terminal" {
			developerImages[service.ServiceType] = service.ImageRef
		}
	}
	require.Equal(t, map[string]string{"ide": immutableID, "terminal": immutableID}, developerImages)
}

func TestCheckoutIssueEnforcesRepositoryAffinityAndIsIdempotent(t *testing.T) {
	workspaceID, userID := uuid.New(), uuid.New()
	machineID, issueID, repoID, otherRepoID, teamID := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	readyMachine := &domain.DevMachine{ID: machineID, WorkspaceID: workspaceID, Status: domain.DevMachineStatusRunning, DesiredStatus: domain.DevMachineStatusRunning, RepositoryAffinityID: &repoID, ExpiresAt: time.Now().Add(time.Hour)}
	issue := &domain.Issue{ID: issueID, WorkspaceID: workspaceID, TeamID: teamID, Identifier: "ENG-7"}
	existing := domain.DevMachineCheckout{ID: uuid.New(), WorkspaceID: workspaceID, MachineID: machineID, IssueID: issueID, GitHubRepoID: repoID, Status: "queued"}
	store := &devMachineStoreFake{
		policy: testPolicy(workspaceID), machine: readyMachine,
		issues:        map[uuid.UUID]*domain.Issue{issueID: issue},
		reposByID:     map[uuid.UUID]*domain.GitHubRepoModel{repoID: {ID: repoID, WorkspaceID: workspaceID, FullName: "kuayle/api", DefaultBranch: "main", IsActive: true}, otherRepoID: {ID: otherRepoID, WorkspaceID: workspaceID, FullName: "kuayle/other", DefaultBranch: "main", IsActive: true}},
		scopeSettings: map[string]*domain.DevMachineScopeSetting{scopeKey(nil, nil, &issueID): {WorkspaceID: workspaceID, GitHubRepoID: &repoID}},
		checkouts:     []domain.DevMachineCheckout{existing},
	}
	svc := newTestDevMachineService(store)

	checkout, err := svc.CheckoutIssue(context.Background(), workspaceID, machineID, userID, dto.CheckoutIssueRequest{IssueID: issueID.String()})
	require.NoError(t, err)
	require.Equal(t, existing.ID, checkout.ID)
	require.False(t, store.createCheckoutCalled)

	store.checkouts = nil
	store.scopeSettings[scopeKey(nil, nil, &issueID)] = &domain.DevMachineScopeSetting{WorkspaceID: workspaceID, GitHubRepoID: &otherRepoID}
	_, err = svc.CheckoutIssue(context.Background(), workspaceID, machineID, userID, dto.CheckoutIssueRequest{IssueID: issueID.String()})
	require.ErrorContains(t, err, "another repository")
	require.ErrorIs(t, err, ErrCheckoutNotEligible)
}

func TestCheckoutIssueRetriesFailedCheckout(t *testing.T) {
	workspaceID, userID := uuid.New(), uuid.New()
	machineID, issueID, repoID, teamID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	message := "temporary checkout failure"
	store := &devMachineStoreFake{
		policy:  testPolicy(workspaceID),
		machine: &domain.DevMachine{ID: machineID, WorkspaceID: workspaceID, Status: domain.DevMachineStatusRunning, DesiredStatus: domain.DevMachineStatusRunning, Generation: 3, RepositoryAffinityID: &repoID, ExpiresAt: time.Now().Add(time.Hour)},
		issues:  map[uuid.UUID]*domain.Issue{issueID: {ID: issueID, WorkspaceID: workspaceID, TeamID: teamID, Identifier: "ENG-7"}},
		reposByID: map[uuid.UUID]*domain.GitHubRepoModel{
			repoID: {ID: repoID, WorkspaceID: workspaceID, FullName: "kuayle/api", DefaultBranch: "main", IsActive: true},
		},
		scopeSettings: map[string]*domain.DevMachineScopeSetting{scopeKey(nil, nil, &issueID): {WorkspaceID: workspaceID, GitHubRepoID: &repoID}},
		checkouts:     []domain.DevMachineCheckout{{ID: uuid.New(), WorkspaceID: workspaceID, MachineID: machineID, IssueID: issueID, GitHubRepoID: repoID, Status: "failed", LastError: &message}},
	}
	svc := newTestDevMachineService(store)

	checkout, err := svc.CheckoutIssue(context.Background(), workspaceID, machineID, userID, dto.CheckoutIssueRequest{IssueID: issueID.String()})

	require.NoError(t, err)
	require.Equal(t, "queued", checkout.Status)
	require.Nil(t, checkout.LastError)
	require.True(t, store.createCheckoutCalled)
	require.NotNil(t, store.checkoutOperation)
	require.Equal(t, int64(3), store.checkoutOperation.Generation)
	require.Contains(t, store.checkoutOperation.IdempotencyKey, "checkout-issue-retry:")
}

func TestCheckoutIssueRequiresDevelopmentRepository(t *testing.T) {
	workspaceID, userID, machineID, issueID, teamID := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	store := &devMachineStoreFake{
		policy:  testPolicy(workspaceID),
		machine: &domain.DevMachine{ID: machineID, WorkspaceID: workspaceID, Status: domain.DevMachineStatusRunning, DesiredStatus: domain.DevMachineStatusRunning, ExpiresAt: time.Now().Add(time.Hour)},
		issues:  map[uuid.UUID]*domain.Issue{issueID: {ID: issueID, WorkspaceID: workspaceID, TeamID: teamID, Identifier: "ENG-7"}},
	}
	svc := newTestDevMachineService(store)

	_, err := svc.CheckoutIssue(context.Background(), workspaceID, machineID, userID, dto.CheckoutIssueRequest{IssueID: issueID.String()})

	require.ErrorIs(t, err, ErrCheckoutNotEligible)
	require.ErrorContains(t, err, "no development repository")
	require.False(t, store.createCheckoutCalled)
}

func TestSnapshotEnvironmentRequiresPausedBuilderAndCreatesPending(t *testing.T) {
	workspaceID, userID, machineID := uuid.New(), uuid.New(), uuid.New()
	store := &devMachineStoreFake{machine: &domain.DevMachine{ID: machineID, WorkspaceID: workspaceID, Status: domain.DevMachineStatusRunning, DesiredStatus: domain.DevMachineStatusRunning}}
	svc := newTestDevMachineService(store)

	_, err := svc.SnapshotEnvironment(context.Background(), workspaceID, userID, dto.CreateDevMachineEnvironmentRequest{Name: "base", SourceMachineID: machineID.String()})
	require.ErrorContains(t, err, "paused or stopped")

	store.machine.Status = domain.DevMachineStatusPaused
	store.machine.EnvironmentBuilder = true
	environment, err := svc.SnapshotEnvironment(context.Background(), workspaceID, userID, dto.CreateDevMachineEnvironmentRequest{Name: "base", SourceMachineID: machineID.String()})
	require.NoError(t, err)
	require.Equal(t, "pending", environment.Status)
	require.NotNil(t, store.createdEnvironmentOperation)
	require.Equal(t, domain.DevMachineOpSnapshotEnvironment, store.createdEnvironmentOperation.Action)
}

func TestLaunchServicePausedMachineQueuesResumeAndReturnsPendingContract(t *testing.T) {
	workspaceID, userID, machineID := uuid.New(), uuid.New(), uuid.New()
	store := &devMachineStoreFake{
		policy:  testPolicy(workspaceID),
		machine: &domain.DevMachine{ID: machineID, WorkspaceID: workspaceID, RoutingKey: "0123456789abcdef0123", Status: domain.DevMachineStatusPaused, DesiredStatus: domain.DevMachineStatusPaused, Generation: 7, ExpiresAt: time.Now().Add(time.Hour)},
	}
	svc := newTestDevMachineService(store)

	launch, err := svc.LaunchService(context.Background(), workspaceID, machineID, userID, "ide", nil)

	require.NoError(t, err)
	require.Equal(t, "resuming", launch.Status)
	require.Empty(t, launch.LaunchURL)
	require.NotNil(t, launch.Operation)
	require.Equal(t, string(domain.DevMachineOpStart), launch.Operation.Action)
	require.Equal(t, domain.DevMachineStatusRunning, store.queuedDesired)
}

func TestLaunchServiceDoesNotAutoResumeStoppedMachine(t *testing.T) {
	workspaceID, userID, machineID := uuid.New(), uuid.New(), uuid.New()
	store := &devMachineStoreFake{
		policy:  testPolicy(workspaceID),
		machine: &domain.DevMachine{ID: machineID, WorkspaceID: workspaceID, RoutingKey: "0123456789abcdef0123", Status: domain.DevMachineStatusStopped, DesiredStatus: domain.DevMachineStatusStopped, Generation: 7, ExpiresAt: time.Now().Add(time.Hour)},
	}
	svc := newTestDevMachineService(store)

	_, err := svc.LaunchService(context.Background(), workspaceID, machineID, userID, "ide", nil)

	require.Error(t, err)
	require.True(t, errors.Is(err, ErrInvalidOperation))
	require.Nil(t, store.queuedOperation)
}

func TestLaunchBrowserOpensResponsiveKasmClient(t *testing.T) {
	workspaceID, userID, machineID := uuid.New(), uuid.New(), uuid.New()
	store := &devMachineStoreFake{
		policy:  testPolicy(workspaceID),
		machine: &domain.DevMachine{ID: machineID, WorkspaceID: workspaceID, RoutingKey: "0123456789abcdef0123", Status: domain.DevMachineStatusRunning, DesiredStatus: domain.DevMachineStatusRunning, ExpiresAt: time.Now().Add(time.Hour)},
		service: &domain.DevMachineService{ID: uuid.New(), MachineID: machineID, ServiceKey: "browser", ServiceType: "browser", Status: "running"},
	}
	svc := newTestDevMachineService(store)

	launch, err := svc.LaunchService(context.Background(), workspaceID, machineID, userID, "browser", nil)

	require.NoError(t, err)
	parsed, err := url.Parse(launch.LaunchURL)
	require.NoError(t, err)
	require.Equal(t, "/", parsed.Path)
	require.Equal(t, "true", parsed.Query().Get("autoconnect"))
	require.Equal(t, "remote", parsed.Query().Get("resize"))
	require.Equal(t, "false", parsed.Query().Get("enable_webrtc"))
	require.NotEmpty(t, parsed.Query().Get("ticket"))
}

func TestPermanentDeleteRunningMachineRequestsPurgeAndQueuesTeardown(t *testing.T) {
	workspaceID, userID, machineID := uuid.New(), uuid.New(), uuid.New()
	store := &devMachineStoreFake{
		machine: &domain.DevMachine{ID: machineID, WorkspaceID: workspaceID, Status: domain.DevMachineStatusRunning, DesiredStatus: domain.DevMachineStatusRunning, Generation: 3, ExpiresAt: time.Now().Add(time.Hour)},
	}
	svc := newTestDevMachineService(store)

	require.NoError(t, svc.PermanentDelete(context.Background(), workspaceID, machineID, userID))
	require.Equal(t, 1, store.permanentDeleteRequests)
	require.NotNil(t, store.machine.DeleteRequestedAt)
	require.NotNil(t, store.queuedOperation)
	require.Equal(t, domain.DevMachineOpTeardown, store.queuedOperation.Action)
	require.Equal(t, domain.DevMachineStatusDestroyed, store.queuedDesired)
	require.NotNil(t, store.queuedOperation.RequestedByUserID)
	require.Equal(t, userID, *store.queuedOperation.RequestedByUserID)
	require.False(t, store.deleteMachineCalled)
}

func TestDeleteRequestsPermanentPurgeAndQueuesTeardown(t *testing.T) {
	workspaceID, userID, machineID := uuid.New(), uuid.New(), uuid.New()
	store := &devMachineStoreFake{
		machine: &domain.DevMachine{ID: machineID, WorkspaceID: workspaceID, Status: domain.DevMachineStatusRunning, DesiredStatus: domain.DevMachineStatusRunning, Generation: 3, ExpiresAt: time.Now().Add(time.Hour)},
	}
	svc := newTestDevMachineService(store)

	operation, err := svc.Delete(context.Background(), workspaceID, machineID, userID)

	require.NoError(t, err)
	require.NotNil(t, operation)
	require.Equal(t, domain.DevMachineOpTeardown, operation.Action)
	require.Equal(t, domain.DevMachineStatusDestroyed, store.queuedDesired)
	require.False(t, store.deleteMachineCalled)
	require.NotNil(t, store.machine.DeleteRequestedAt)
	require.Equal(t, 1, store.permanentDeleteRequests)
}

func TestPermanentDeleteRepeatedRequestIsSafe(t *testing.T) {
	workspaceID, userID, machineID := uuid.New(), uuid.New(), uuid.New()
	store := &devMachineStoreFake{
		machine: &domain.DevMachine{ID: machineID, WorkspaceID: workspaceID, Status: domain.DevMachineStatusRunning, DesiredStatus: domain.DevMachineStatusRunning, Generation: 3, ExpiresAt: time.Now().Add(time.Hour)},
	}
	svc := newTestDevMachineService(store)

	require.NoError(t, svc.PermanentDelete(context.Background(), workspaceID, machineID, userID))
	firstOperation := store.queuedOperation
	require.NoError(t, svc.PermanentDelete(context.Background(), workspaceID, machineID, userID))
	require.Equal(t, 2, store.permanentDeleteRequests)
	require.Equal(t, 1, store.permanentDeleteQueued)
	require.Same(t, firstOperation, store.queuedOperation)
	require.NotNil(t, store.machine.DeleteRequestedAt)
	require.False(t, store.deleteMachineCalled)
}

func TestBulkDeleteOldOnlyWithoutSelectionIsNoop(t *testing.T) {
	store := &devMachineStoreFake{}
	svc := newTestDevMachineService(store)

	count, err := svc.BulkDelete(context.Background(), uuid.New(), uuid.New(), dto.BulkDeleteDevMachinesRequest{OldOnly: true})

	require.NoError(t, err)
	require.Zero(t, count)
	require.False(t, store.deleteMachineCalled)
	require.Nil(t, store.queuedOperation)
}

func TestDeleteScopeSettingIsIdempotent(t *testing.T) {
	store := &devMachineStoreFake{deleteScopeSettingErr: sql.ErrNoRows}
	svc := newTestDevMachineService(store)

	err := svc.DeleteScopeSetting(context.Background(), uuid.New(), "workspace", nil)

	require.NoError(t, err)
}

type devMachineStoreFake struct {
	repository.DevMachineStore
	policy                      *domain.DevMachineWorkspacePolicy
	nameExists                  map[string]bool
	alwaysNameExists            bool
	nameChecks                  int
	createdMachine              *domain.DevMachine
	createdServices             []domain.DevMachineService
	createdOperation            *domain.DevMachineOperation
	createdEnvironment          *domain.DevMachineEnvironment
	createdEnvironmentOperation *domain.DevMachineOperation
	scopeSettings               map[string]*domain.DevMachineScopeSetting
	reposByID                   map[uuid.UUID]*domain.GitHubRepoModel
	reposByFullName             map[string]*domain.GitHubRepoModel
	issues                      map[uuid.UUID]*domain.Issue
	projects                    map[uuid.UUID]*domain.Project
	environments                map[uuid.UUID]*domain.DevMachineEnvironment
	machine                     *domain.DevMachine
	service                     *domain.DevMachineService
	createdTicket               *domain.DevMachineAccessTicket
	checkouts                   []domain.DevMachineCheckout
	createCheckoutCalled        bool
	checkoutOperation           *domain.DevMachineOperation
	deleteMachineCalled         bool
	permanentDeleteRequests     int
	permanentDeleteQueued       int
	deleteScopeSettingErr       error
	queuedOperation             *domain.DevMachineOperation
	queuedDesired               domain.DevMachineStatus
}

func newTestDevMachineService(store *devMachineStoreFake) *DevMachineService {
	return NewDevMachineService(store, agent.NewRegistry(), true, "machines.example.test", cryptoutil.DeriveKey("test"), time.Minute, DevMachineImages{})
}

func testPolicy(workspaceID uuid.UUID) *domain.DevMachineWorkspacePolicy {
	return &domain.DevMachineWorkspacePolicy{
		WorkspaceID: workspaceID, Enabled: true, MaxConcurrentMachines: 10, MaxMachinesPerUser: 10,
		MaxDailyAgentRuns: 25, MaxRuntimeMinutes: 480, MaxDiskGB: 100, IdlePauseMinutes: 240,
		AllowedProviders:    json.RawMessage(`[]`),
		AllowedRepositories: json.RawMessage(`[]`),
	}
}

func (f *devMachineStoreFake) CreateBundle(_ context.Context, machine *domain.DevMachine, _ []domain.DevMachineAgentProvider, services []domain.DevMachineService, _ []domain.DevMachineVolume, _ []domain.DevMachineEnvVar, _ []domain.DevMachineToken, operation *domain.DevMachineOperation) error {
	f.createdMachine = machine
	f.createdServices = append([]domain.DevMachineService(nil), services...)
	f.createdOperation = operation
	machine.CreatedAt = time.Now().UTC()
	machine.UpdatedAt = machine.CreatedAt
	operation.CreatedAt = machine.CreatedAt
	return nil
}

func (f *devMachineStoreFake) GetMachine(_ context.Context, workspaceID, machineID uuid.UUID) (*domain.DevMachine, error) {
	if f.machine == nil || f.machine.ID != machineID || f.machine.WorkspaceID != workspaceID {
		return nil, nil
	}
	return f.machine, nil
}

func (f *devMachineStoreFake) GetService(_ context.Context, _ uuid.UUID, machineID uuid.UUID, serviceKey string) (*domain.DevMachineService, error) {
	if f.service == nil || f.service.MachineID != machineID || f.service.ServiceKey != serviceKey {
		return nil, nil
	}
	return f.service, nil
}

func (f *devMachineStoreFake) CreateAccessTicket(_ context.Context, ticket *domain.DevMachineAccessTicket) error {
	f.createdTicket = ticket
	return nil
}

func (f *devMachineStoreFake) TouchMachineActivity(context.Context, uuid.UUID, time.Time) error {
	return nil
}

func (f *devMachineStoreFake) CountActiveMachines(context.Context, uuid.UUID, *uuid.UUID) (int, error) {
	return 0, nil
}

func (f *devMachineStoreFake) GetOperationByIdempotency(context.Context, uuid.UUID, uuid.UUID, string) (*domain.DevMachineOperation, error) {
	if f.queuedOperation != nil {
		return f.queuedOperation, nil
	}
	return nil, nil
}

func (f *devMachineStoreFake) SetDesiredAndEnqueue(_ context.Context, _ uuid.UUID, _ uuid.UUID, desired domain.DevMachineStatus, operation *domain.DevMachineOperation) error {
	f.queuedDesired = desired
	f.queuedOperation = operation
	if f.machine != nil {
		f.machine.DesiredStatus = desired
		f.machine.Generation = operation.Generation
	}
	return nil
}

func (f *devMachineStoreFake) GetPolicy(context.Context, uuid.UUID) (*domain.DevMachineWorkspacePolicy, error) {
	return f.policy, nil
}

func (f *devMachineStoreFake) MachineNameExists(_ context.Context, _ uuid.UUID, name string) (bool, error) {
	f.nameChecks++
	if f.alwaysNameExists {
		return true, nil
	}
	return f.nameExists[strings.ToLower(name)], nil
}

func (f *devMachineStoreFake) CreateEvent(context.Context, *domain.DevMachineEvent) error { return nil }

func (f *devMachineStoreFake) GetScopeSetting(_ context.Context, _ uuid.UUID, teamID, projectID, issueID *uuid.UUID) (*domain.DevMachineScopeSetting, error) {
	if f.scopeSettings == nil {
		return nil, nil
	}
	return f.scopeSettings[scopeKey(teamID, projectID, issueID)], nil
}

func (f *devMachineStoreFake) GetLinkedRepository(_ context.Context, _ uuid.UUID, repositoryID uuid.UUID) (*domain.GitHubRepoModel, error) {
	if f.reposByID == nil {
		return nil, nil
	}
	return f.reposByID[repositoryID], nil
}

func (f *devMachineStoreFake) GetLinkedRepositoryByFullName(_ context.Context, _ uuid.UUID, fullName string) (*domain.GitHubRepoModel, error) {
	if f.reposByFullName != nil {
		if repo := f.reposByFullName[strings.ToLower(fullName)]; repo != nil {
			return repo, nil
		}
	}
	for _, repo := range f.reposByID {
		if strings.EqualFold(repo.FullName, fullName) {
			return repo, nil
		}
	}
	return nil, nil
}

func (f *devMachineStoreFake) GetIssueDevelopmentContext(_ context.Context, _ uuid.UUID, issueID uuid.UUID) (*domain.Issue, error) {
	if f.issues == nil {
		return nil, nil
	}
	return f.issues[issueID], nil
}

func (f *devMachineStoreFake) GetProjectDevelopmentContext(_ context.Context, _ uuid.UUID, projectID uuid.UUID) (*domain.Project, error) {
	if f.projects == nil {
		return nil, nil
	}
	return f.projects[projectID], nil
}

func (f *devMachineStoreFake) GetEnvironment(_ context.Context, _ uuid.UUID, environmentID uuid.UUID) (*domain.DevMachineEnvironment, error) {
	if f.environments == nil {
		return nil, nil
	}
	return f.environments[environmentID], nil
}

func (f *devMachineStoreFake) ListCheckouts(context.Context, uuid.UUID, uuid.UUID) ([]domain.DevMachineCheckout, error) {
	return f.checkouts, nil
}

func (f *devMachineStoreFake) CreateCheckout(_ context.Context, checkout *domain.DevMachineCheckout, operation *domain.DevMachineOperation) error {
	f.createCheckoutCalled = true
	f.checkoutOperation = operation
	f.checkouts = append(f.checkouts, *checkout)
	return nil
}

func (f *devMachineStoreFake) CreateEnvironment(_ context.Context, environment *domain.DevMachineEnvironment, operation *domain.DevMachineOperation) error {
	f.createdEnvironment = environment
	f.createdEnvironmentOperation = operation
	return nil
}

func (f *devMachineStoreFake) RequestPermanentDelete(_ context.Context, workspaceID, machineID uuid.UUID, requestedByUserID *uuid.UUID) (*domain.DevMachineOperation, error) {
	f.permanentDeleteRequests++
	if f.machine == nil || f.machine.ID != machineID || f.machine.WorkspaceID != workspaceID {
		return nil, sql.ErrNoRows
	}
	if f.machine.DeleteRequestedAt == nil {
		now := time.Now().UTC()
		f.machine.DeleteRequestedAt = &now
	}
	if domain.DevMachineSafelyPurgeable(f.machine) {
		return nil, nil
	}
	if f.queuedOperation != nil && f.queuedOperation.Action == domain.DevMachineOpTeardown && (f.queuedOperation.Status == domain.DevMachineOpStatusPending || f.queuedOperation.Status == domain.DevMachineOpStatusLeased) {
		return f.queuedOperation, nil
	}
	generation := f.machine.Generation + 1
	operation := &domain.DevMachineOperation{
		ID: uuid.New(), MachineID: machineID, WorkspaceID: workspaceID,
		Action: domain.DevMachineOpTeardown, Status: domain.DevMachineOpStatusPending,
		Generation: generation, IdempotencyKey: fmt.Sprintf("permanent-delete:%d", generation),
		RequestedByUserID: requestedByUserID, MaxAttempts: 10,
	}
	f.permanentDeleteQueued++
	f.queuedDesired = domain.DevMachineStatusDestroyed
	f.queuedOperation = operation
	f.machine.DesiredStatus = domain.DevMachineStatusDestroyed
	f.machine.Generation = generation
	return operation, nil
}

func (f *devMachineStoreFake) DeleteScopeSetting(context.Context, uuid.UUID, *uuid.UUID, *uuid.UUID, *uuid.UUID) error {
	return f.deleteScopeSettingErr
}

func scopeKey(teamID, projectID, issueID *uuid.UUID) string {
	parts := []string{"", "", ""}
	if teamID != nil {
		parts[0] = teamID.String()
	}
	if projectID != nil {
		parts[1] = projectID.String()
	}
	if issueID != nil {
		parts[2] = issueID.String()
	}
	return strings.Join(parts, ":")
}

func dmStrPtr(value string) *string { return &value }
