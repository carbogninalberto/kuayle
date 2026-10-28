package machine

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/agent"
	"github.com/kuayle/kuayle-backend/internal/domain"
	cryptoutil "github.com/kuayle/kuayle-backend/pkg/crypto"
	"github.com/stretchr/testify/require"
)

// Task attempts, including requested reviews, are created without push, so
// the manager never asks GitHub for a token and the harness gets none; only a
// run that pushes its branch would receive one.
func TestReviewAttemptsReceiveNoGitHubCredential(t *testing.T) {
	key := cryptoutil.DeriveKey("review-credential-test")
	encrypted, err := cryptoutil.Encrypt("sk-review-agent", key)
	require.NoError(t, err)
	for _, push := range []bool{false, true} {
		machineID, workspaceID, runID, taskID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
		run := &domain.DevMachineAgentRun{
			ID: runID, MachineID: machineID, WorkspaceID: workspaceID, TaskID: &taskID,
			ProviderID: "codex", Mode: "autonomous", Status: domain.DevMachineAgentRunStatusQueued,
			AllowedSecrets: json.RawMessage(`[]`), CommandArgv: json.RawMessage(`["codex"]`), MaxRuntimeSeconds: 30,
			PushBranch: push, ModelServerID: serverRef(),
		}
		store := &managerStoreFake{
			machine: &domain.DevMachine{
				ID: machineID, WorkspaceID: workspaceID, RoutingKey: "0123456789abcdef0123", RepoOwner: "acme", RepoName: "app",
				Status: domain.DevMachineStatusRunning, DesiredStatus: domain.DevMachineStatusRunning, Generation: 1,
				DockerNetworkName: ptr("network"), WorkspaceVolumeName: ptr("volume"),
			},
			agentRun:             run,
			provider:             &domain.DevMachineAgentProvider{MachineID: machineID, ProviderID: "codex", ImageRef: "kuayle/codex:test", Enabled: true},
			githubInstallationID: 7,
			modelServer:          deepSeekServer(workspaceID, encrypted, "deepseek-v4-pro", "max"),
		}
		runtime := &runtimeFake{agentExecution: &AgentExecution{ContainerID: "agent-container", ExitCode: 0}}
		manager := NewManager(store, runtime, agent.NewRegistry(agent.NewCodexProvider("kuayle/codex:test")), &publicationGitHubFake{}, key, nil, "test")
		require.NoError(t, manager.runAgent(context.Background(), store.machine, &domain.DevMachineOperation{MachineID: machineID, WorkspaceID: workspaceID, AgentRunID: &runID, Generation: 1}))
		if push {
			require.Equal(t, "fixture-installation-token", runtime.agentSecrets["GITHUB_TOKEN"], "the fixture would hand out a token")
			continue
		}
		require.NotContains(t, runtime.agentSecrets, "GITHUB_TOKEN")
		require.Empty(t, store.installationFullNameCalls, "no GitHub token is even requested")
	}
}
