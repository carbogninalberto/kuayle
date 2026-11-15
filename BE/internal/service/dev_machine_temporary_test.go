package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

// A temporary machine is the tiny headless preset with nothing that holds a
// credential, even where the workspace defaults a repository and allows Docker
// and sudo.
func TestTemporaryMachineIsTheTinyPresetWithoutCredentials(t *testing.T) {
	workspaceID, userID, conversationID, repoID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	policy := testPolicy(workspaceID)
	policy.AllowDocker, policy.AllowSudo = true, true
	store := &devMachineStoreFake{
		policy:        policy,
		reposByID:     map[uuid.UUID]*domain.GitHubRepoModel{repoID: {ID: repoID, WorkspaceID: workspaceID, FullName: "Kuayle/API", DefaultBranch: "main", IsActive: true}},
		scopeSettings: map[string]*domain.DevMachineScopeSetting{scopeKey(nil, nil, nil): {WorkspaceID: workspaceID, GitHubRepoID: &repoID}},
	}

	machine, err := newTestDevMachineService(store).StartTemporaryMachine(context.Background(), workspaceID, userID, conversationID)
	require.NoError(t, err)
	require.Equal(t, domain.TemporaryMachineResources.CPUMillis, machine.CPUMillis)
	require.Equal(t, domain.TemporaryMachineResources.MemoryMB, machine.MemoryMB)
	require.Equal(t, domain.TemporaryMachineResources.DiskGB, machine.DiskGB)
	require.Equal(t, domain.DevMachineProfileHeadless, machine.Profile)
	require.Regexp(t, `^tmp-[0-9a-f]{8}$`, machine.Name)
	require.Empty(t, machine.RepoURL, "the workspace's default repository is not cloned")
	require.Nil(t, machine.RepositoryAffinityID)
	require.False(t, machine.SudoEnabled, "never sudo")
	require.Equal(t, &conversationID, machine.TemporaryConversationID)
	require.NotNil(t, machine.TemporaryExpiresAt)
	require.WithinDuration(t, time.Now().Add(domain.TemporaryMachineTTL), *machine.TemporaryExpiresAt, 5*time.Second)
	for _, service := range store.createdServices {
		require.NotEqual(t, dockerServiceType, service.ServiceType, "never a Docker daemon")
	}
	for _, envVar := range store.createdEnvVars {
		require.Equal(t, "KUAYLE_MACHINE_TOKEN", envVar.Name, "only the collector's own token")
		require.Equal(t, "collector", envVar.TargetService)
	}
}

func TestTemporaryMachineQuotaRefusals(t *testing.T) {
	workspaceID, userID := uuid.New(), uuid.New()
	store := &devMachineStoreFake{policy: testPolicy(workspaceID), createBundleErr: repository.ErrTemporaryMachinesOff}
	_, err := newTestDevMachineService(store).StartTemporaryMachine(context.Background(), workspaceID, userID, uuid.New())
	require.ErrorIs(t, err, ErrTemporaryMachinesOff)

	store.createBundleErr = repository.ErrMachineQuota
	_, err = newTestDevMachineService(store).StartTemporaryMachine(context.Background(), workspaceID, userID, uuid.New())
	require.ErrorIs(t, err, ErrMachineQuota)

	// Temporary machines count toward the ordinary machine quotas too.
	store = &devMachineStoreFake{policy: testPolicy(workspaceID), userActiveCount: 10}
	_, err = newTestDevMachineService(store).StartTemporaryMachine(context.Background(), workspaceID, userID, uuid.New())
	require.ErrorIs(t, err, ErrMachineQuota)
}

type policyStoreFake struct {
	*devMachineStoreFake
	saved *domain.DevMachineWorkspacePolicy
}

func (f *policyStoreFake) UpsertPolicy(_ context.Context, policy *domain.DevMachineWorkspacePolicy) error {
	f.saved = policy
	return nil
}

// The temporary machine quota is kept when a client omits it, and is 0 to 10.
func TestPolicyTemporaryMachineQuota(t *testing.T) {
	workspaceID := uuid.New()
	current := testPolicy(workspaceID)
	current.MaxTemporaryMachines = 5
	store := &policyStoreFake{devMachineStoreFake: &devMachineStoreFake{policy: current}}
	service := newTestDevMachineService(store)
	request := dto.DevMachinePolicyRequest{Enabled: true, MaxConcurrentMachines: 10, MaxMachinesPerUser: 10, MaxDiskGB: 100}

	_, err := service.UpdatePolicy(context.Background(), workspaceID, request)
	require.NoError(t, err)
	require.Equal(t, 5, store.saved.MaxTemporaryMachines, "omitted keeps the current quota")
	off := 0
	request.MaxTemporaryMachines = &off
	_, err = service.UpdatePolicy(context.Background(), workspaceID, request)
	require.NoError(t, err)
	require.Zero(t, store.saved.MaxTemporaryMachines, "0 turns temporary machines off")
	tooMany := domain.MaxTemporaryMachinesLimit + 1
	request.MaxTemporaryMachines = &tooMany
	_, err = service.UpdatePolicy(context.Background(), workspaceID, request)
	require.ErrorIs(t, err, ErrInvalidMachineInput)

	store.policy = nil
	request.MaxTemporaryMachines = nil
	_, err = service.UpdatePolicy(context.Background(), workspaceID, request)
	require.NoError(t, err)
	require.Equal(t, domain.DefaultMaxTemporaryMachines, store.saved.MaxTemporaryMachines, "a new policy gets the default")
}
