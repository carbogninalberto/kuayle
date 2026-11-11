package machine

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/agent"
	"github.com/kuayle/kuayle-backend/internal/domain"
	cryptoutil "github.com/kuayle/kuayle-backend/pkg/crypto"
	"github.com/moby/moby/api/types/mount"
	"github.com/stretchr/testify/require"
)

type notebookStoreFake struct {
	*managerStoreFake
	updates   int
	problem   string
	stateErr  error
	snapshots []string
	listErr   error
	recorded  []domain.AgentOutboxFile
}

func (f *notebookStoreFake) NotebookUpdateState(context.Context, uuid.UUID) (int, string, error) {
	return f.updates, f.problem, f.stateErr
}

func (f *notebookStoreFake) ListAgentRunSecretSnapshots(context.Context, uuid.UUID, uuid.UUID) ([]string, error) {
	return f.snapshots, f.listErr
}

func (f *notebookStoreFake) RecordAgentOutbox(_ context.Context, _ uuid.UUID, files []domain.AgentOutboxFile) (int, error) {
	f.recorded = append(f.recorded, files...)
	return 0, nil
}

// notebookRun runs one agent attempt that concluded normally and returns
// how the manager completed it.
func notebookRun(t *testing.T, required bool, updates int, problem string) *domain.DevMachineAgentRun {
	t.Helper()
	machineID, workspaceID, runID := uuid.New(), uuid.New(), uuid.New()
	run := &domain.DevMachineAgentRun{
		ID: runID, MachineID: machineID, WorkspaceID: workspaceID, ProviderID: "opencode", Mode: "autonomous",
		Status: domain.DevMachineAgentRunStatusQueued, MaxRuntimeSeconds: 30, NotebookRequired: required,
	}
	store := &notebookStoreFake{updates: updates, problem: problem, managerStoreFake: &managerStoreFake{
		machine: &domain.DevMachine{ID: machineID, WorkspaceID: workspaceID, RoutingKey: "0123456789abcdef0123",
			Status: domain.DevMachineStatusRunning, DesiredStatus: domain.DevMachineStatusRunning, Generation: 1},
		agentRun: run,
		provider: &domain.DevMachineAgentProvider{MachineID: machineID, ProviderID: "opencode", ImageRef: "kuayle/opencode:test", Enabled: true},
	}}
	runtime := &runtimeFake{agentExecution: &AgentExecution{ContainerID: "agent-container", ExitCode: 0,
		Stdout: `{"type":"text","part":{"type":"text","text":"Implemented the toggle and ran the tests."}}`}}
	manager := NewManager(store, runtime, agent.NewRegistry(agent.NewOpenCodeProvider("kuayle/opencode:test")), nil, make([]byte, 32), make([]byte, 32), "test")
	require.NoError(t, manager.runAgent(context.Background(), store.machine, &domain.DevMachineOperation{
		MachineID: machineID, WorkspaceID: workspaceID, AgentRunID: &runID, Action: domain.DevMachineOpRunAgent, Generation: 1}))
	require.NotNil(t, store.completedRun)
	return store.completedRun
}

// The harness, not the prompt, makes the notebook update mandatory: an
// attempt that concluded normally but left no valid update fails with that
// reason, on every harness alike (the check reads Kuayle's own record).
func TestAttemptCannotFinishWithoutAValidNotebookUpdate(t *testing.T) {
	missing := notebookRun(t, true, 0, "")
	require.Equal(t, domain.DevMachineAgentRunStatusFailed, missing.Status)
	require.True(t, domain.IsNotebookUpdateReason(*missing.ErrorMessage))
	require.Contains(t, *missing.ErrorMessage, "it wrote no .kuayle/outbox/notebook.json")
	require.Equal(t, "Implemented the toggle and ran the tests.", *missing.Summary, "its summary is kept")

	invalid := notebookRun(t, true, 0, `unknown field "status"`)
	require.Equal(t, domain.DevMachineAgentRunStatusFailed, invalid.Status)
	require.Contains(t, *invalid.ErrorMessage, `unknown field "status"`)

	// A valid update followed by a refused one does not count either.
	require.Equal(t, domain.DevMachineAgentRunStatusFailed, notebookRun(t, true, 1, "done[0] is longer than 600 bytes").Status)

	valid := notebookRun(t, true, 1, "")
	require.Equal(t, domain.DevMachineAgentRunStatusSucceeded, valid.Status)
	require.Nil(t, valid.ErrorMessage)

	// Runs that need no update (a review, a manual run) are unaffected.
	require.Equal(t, domain.DevMachineAgentRunStatusSucceeded, notebookRun(t, false, 0, "").Status)
}

// Notebook updates are redacted like run output before they are recorded:
// the notebook is given to later agents and shown to people.
func TestNotebookUpdatesAreRedactedBeforeTheyAreRecorded(t *testing.T) {
	key := make([]byte, 32)
	payload, err := json.Marshal(domain.AgentRunSecretSnapshot{Version: 1, Environment: map[string]string{"GITHUB_TOKEN": "ghp_s3cr3t\"quoted"}, Redactions: map[string]string{"GITHUB_TOKEN": "ghp_s3cr3t\"quoted"}})
	require.NoError(t, err)
	sealed, err := cryptoutil.Encrypt(string(payload), key)
	require.NoError(t, err)
	store := &notebookStoreFake{managerStoreFake: &managerStoreFake{}, snapshots: []string{sealed}}
	manager := &Manager{store: store, encryptionKey: key}
	machine := &domain.DevMachine{ID: uuid.New(), WorkspaceID: uuid.New()}
	update, err := json.Marshal(map[string]any{"done": []string{"Pushed with ghp_s3cr3t\"quoted to origin"}})
	require.NoError(t, err)
	files := manager.redactNotebookFiles(context.Background(), machine, []domain.AgentOutboxFile{
		{Name: "notebook.json", Data: update},
		{Name: "1.json", Data: []byte(`{"to":"ENG-2","body":"ghp_s3cr3t"}`)},
		{Name: "notebook-2.json", Data: []byte("not json ghp_s3cr3t\"quoted")},
	})
	require.NotContains(t, string(files[0].Data), "ghp_s3cr3t")
	parsed, err := domain.ParseNotebookUpdate(files[0].Data, false)
	require.NoError(t, err, "a redacted update is still valid JSON")
	require.Contains(t, parsed.Done[0], "Pushed with")
	require.Contains(t, string(files[1].Data), "ghp_s3cr3t", "messages keep their own handling")
	require.NotContains(t, string(files[2].Data), "ghp_s3cr3t")

	// An update that cannot be checked for secrets is not recorded.
	store.listErr = errors.New("database is down")
	files = manager.redactNotebookFiles(context.Background(), machine, []domain.AgentOutboxFile{{Name: "notebook.json", Data: update}})
	require.Nil(t, files[0].Data)
	require.NotEmpty(t, files[0].Unreadable)
}

// A planning step's agent container mounts its worktree read-only and only
// the worktree's .kuayle directory (the outbox) writable; a step that
// changes code gets no such mount. The spec is the same for every harness:
// which harnesses ask for it is domain.ReadOnlyWorktreeSupported.
func TestPlanningStepMountsItsWorktreeReadOnlyWithAWritableOutbox(t *testing.T) {
	checkout := &domain.DevMachineCheckout{WorkspacePath: "/workspace/tasks/eng-9-0f0e"}
	for _, harness := range []string{"claude-code", "codex", "opencode"} {
		planning := readOnlyWorktreeMounts("workspace-volume", &domain.DevMachineAgentRun{ProviderID: harness, Mode: "autonomous", WorktreeReadOnly: true}, checkout)
		require.Len(t, planning, 2, harness)
		worktree, outbox := planning[0], planning[1]
		require.Equal(t, mount.TypeVolume, worktree.Type)
		require.Equal(t, "workspace-volume", worktree.Source)
		require.Equal(t, "/workspace/tasks/eng-9-0f0e", worktree.Target)
		require.True(t, worktree.ReadOnly)
		require.Equal(t, "tasks/eng-9-0f0e", worktree.VolumeOptions.Subpath)
		require.True(t, worktree.VolumeOptions.NoCopy)
		require.Equal(t, "/workspace/tasks/eng-9-0f0e/.kuayle", outbox.Target)
		require.False(t, outbox.ReadOnly, "the outbox stays writable")
		require.Equal(t, "tasks/eng-9-0f0e/.kuayle", outbox.VolumeOptions.Subpath)

		require.Empty(t, readOnlyWorktreeMounts("workspace-volume", &domain.DevMachineAgentRun{ProviderID: harness, Mode: "autonomous"}, checkout), "a step that changes code")
	}
	readOnly := &domain.DevMachineAgentRun{Mode: "autonomous", WorktreeReadOnly: true}
	require.Empty(t, readOnlyWorktreeMounts("workspace-volume", readOnly, nil))
	for _, path := range []string{"/workspace", "/workspace/tasks", "/workspace/tasks/", "/workspace/tasks/../repos", "/workspace/tasks/a/b", "/workspace/tasks/.hidden", "/elsewhere/tasks/a"} {
		require.Empty(t, readOnlyWorktreeMounts("workspace-volume", readOnly, &domain.DevMachineCheckout{WorkspacePath: path}), path)
	}
	require.Empty(t, readOnlyWorktreeMounts("workspace-volume", &domain.DevMachineAgentRun{Mode: "interactive", WorktreeReadOnly: true}, checkout))

	// The workspace volume itself stays writable for the agent: its session
	// directory and run directory live there, outside the worktree.
	runtime := &DockerRuntime{}
	host := runtime.secureHostConfig(&domain.DevMachine{MemoryMB: 4096, CPUMillis: 2000, PidsLimit: 512}, "agent", "machine-network", "workspace-volume", false)
	require.Equal(t, "/workspace", host.Mounts[0].Target)
	require.False(t, host.Mounts[0].ReadOnly)
	require.Contains(t, host.Tmpfs, "/tmp")
	require.Contains(t, host.Tmpfs, "/home/kuayle")
}

func TestVolumeSubpathNeedsDockerAPI145(t *testing.T) {
	require.True(t, apiAtLeast("1.45", volumeSubpathAPI))
	require.True(t, apiAtLeast("1.54", volumeSubpathAPI))
	require.True(t, apiAtLeast("2.0", volumeSubpathAPI))
	require.False(t, apiAtLeast("1.44", volumeSubpathAPI))
	require.False(t, apiAtLeast("1.9", volumeSubpathAPI))
	require.False(t, apiAtLeast("", volumeSubpathAPI))
}
