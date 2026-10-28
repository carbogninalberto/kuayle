package machine

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

func outboxShells(t *testing.T) []string {
	shells := []string{"/bin/sh"}
	if _, err := os.Stat("/bin/dash"); err == nil {
		// The developer container's /bin/sh is dash.
		shells = append(shells, "/bin/dash")
	}
	return shells
}

// The take script reads only regular .json files of each task worktree's
// .kuayle/outbox, bounded, and removes what it took and what it refused.
func TestOutboxTakeScriptReadsOnlyBoundedRegularFiles(t *testing.T) {
	for _, shell := range outboxShells(t) {
		t.Run(filepath.Base(shell), func(t *testing.T) {
			root := t.TempDir()
			outbox := filepath.Join(root, "eng-1", ".kuayle", "outbox")
			require.NoError(t, os.MkdirAll(outbox, 0o700))
			write := func(name, body string) {
				require.NoError(t, os.WriteFile(filepath.Join(outbox, name), []byte(body), 0o600))
			}
			write("01-hello.json", `{"to":"ENG-2","body":"hello"}`)
			write("02-big.json", strings.Repeat("x", domain.AgentOutboxFileMaxBytes+100))
			write("notes.txt", "not a message")
			write("bad name.json", `{}`)
			write(".draft.json", `{"to":"ENG-2","body":"still writing"}`)
			secret := filepath.Join(t.TempDir(), "secret.json")
			require.NoError(t, os.WriteFile(secret, []byte(`{"token":"s3cret"}`), 0o600))
			require.NoError(t, os.Symlink(secret, filepath.Join(outbox, "03-link.json")))
			// A worktree whose .kuayle is a symlink is not read.
			linked := filepath.Join(root, "eng-3")
			require.NoError(t, os.MkdirAll(linked, 0o700))
			elsewhere := t.TempDir()
			require.NoError(t, os.MkdirAll(filepath.Join(elsewhere, "outbox"), 0o700))
			require.NoError(t, os.WriteFile(filepath.Join(elsewhere, "outbox", "x.json"), []byte(`{"to":"ENG-1","body":"x"}`), 0o600))
			require.NoError(t, os.Symlink(elsewhere, filepath.Join(linked, ".kuayle")))
			many := filepath.Join(root, "eng-4", ".kuayle", "outbox")
			require.NoError(t, os.MkdirAll(many, 0o700))
			for i := range 12 {
				require.NoError(t, os.WriteFile(filepath.Join(many, string(rune('a'+i))+".json"), []byte(`{"to":"ENG-1","body":"x"}`), 0o600))
			}

			output, err := exec.Command(shell, "-c", outboxTakeScript, "kuayle-outbox", root).CombinedOutput()
			require.NoError(t, err, string(output))
			files := parseOutboxTake(output, "/workspace/tasks")
			byName := map[string]domain.AgentOutboxFile{}
			for _, file := range files {
				byName[strings.TrimPrefix(file.Worktree, "/workspace/tasks/")+"/"+file.Name] = file
			}
			require.Len(t, files, 2+outboxFilesPerWorktree)
			require.Equal(t, `{"to":"ENG-2","body":"hello"}`, string(byName["eng-1/01-hello.json"].Data))
			require.False(t, byName["eng-1/01-hello.json"].Oversize)
			require.True(t, byName["eng-1/02-big.json"].Oversize)
			require.Len(t, byName["eng-1/02-big.json"].Data, domain.AgentOutboxFileMaxBytes)
			require.NotContains(t, string(output), "czNjcmV0", "a symlinked file is never read")

			left, err := os.ReadDir(outbox)
			require.NoError(t, err)
			names := []string{}
			for _, entry := range left {
				names = append(names, entry.Name())
			}
			require.Equal(t, []string{".draft.json"}, names, "taken and refused entries are removed; hidden drafts stay")
			require.FileExists(t, secret, "a symlink is removed, never its target")
			require.FileExists(t, filepath.Join(elsewhere, "outbox", "x.json"))
			rest, err := os.ReadDir(many)
			require.NoError(t, err)
			require.Len(t, rest, 12-outboxFilesPerWorktree, "files over the per-scan limit wait for the next scan")
		})
	}
}

func TestOutboxPrepareScriptExcludesKuayleFilesFromCommits(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	for _, shell := range outboxShells(t) {
		t.Run(filepath.Base(shell), func(t *testing.T) {
			repo := t.TempDir()
			git(t, repo, "init", "-q")
			worktree := repo
			require.NoError(t, os.Symlink(t.TempDir(), filepath.Join(worktree, ".kuayle")))
			output, err := exec.Command(shell, "-c", prepareOutboxScript, "kuayle-outbox", worktree).CombinedOutput()
			require.NoError(t, err, string(output))
			info, err := os.Lstat(filepath.Join(worktree, ".kuayle"))
			require.NoError(t, err)
			require.True(t, info.IsDir(), "a symlinked .kuayle is replaced by a directory")
			require.DirExists(t, filepath.Join(worktree, ".kuayle", "outbox"))
			require.NoError(t, os.WriteFile(filepath.Join(worktree, ".kuayle", "outbox", "1.json"), []byte(`{}`), 0o600))
			git(t, worktree, "add", "-A")
			require.Empty(t, strings.TrimSpace(git(t, worktree, "diff", "--cached", "--name-only")), "outbox files cannot be committed")
			output, err = exec.Command(shell, "-c", prepareOutboxScript, "kuayle-outbox", worktree).CombinedOutput()
			require.NoError(t, err, string(output))
			exclude, err := os.ReadFile(filepath.Join(repo, ".git", "info", "exclude"))
			require.NoError(t, err)
			require.Equal(t, 1, strings.Count(string(exclude), "/.kuayle/"))
		})
	}
}

type outboxRuntimeFake struct {
	*portRuntimeFake
	files    []domain.AgentOutboxFile
	prepared []string
}

func (f *outboxRuntimeFake) TakeOutbox(context.Context, []domain.DevMachineService) ([]domain.AgentOutboxFile, error) {
	files := f.files
	f.files = nil
	return files, nil
}

func (f *outboxRuntimeFake) PrepareOutbox(_ context.Context, _ []domain.DevMachineService, checkout *domain.DevMachineCheckout) error {
	f.prepared = append(f.prepared, checkout.WorkspacePath)
	return nil
}

type outboxStoreFake struct {
	*managerStoreFake
	machines []uuid.UUID
	recorded []domain.AgentOutboxFile
	events   []string
}

func (f *outboxStoreFake) RecordAgentOutbox(_ context.Context, machine uuid.UUID, files []domain.AgentOutboxFile) (int, error) {
	f.machines = append(f.machines, machine)
	f.recorded = append(f.recorded, files...)
	return len(files), nil
}

func (f *outboxStoreFake) CreateEvent(_ context.Context, event *domain.DevMachineEvent) error {
	f.events = append(f.events, event.EventType)
	return nil
}

// The port scan also collects agent messages while attempts run.
func TestPortScanCollectsAgentOutboxes(t *testing.T) {
	manager, store, ports := newPortManager()
	runtime := &outboxRuntimeFake{portRuntimeFake: ports, files: []domain.AgentOutboxFile{{Worktree: "/workspace/tasks/eng-1", Name: "1.json", Data: []byte(`{}`)}}}
	outboxes := &outboxStoreFake{managerStoreFake: store}
	manager.runtime, manager.store = runtime, outboxes
	require.NoError(t, manager.syncMachinePorts(context.Background(), store.machine, store.services))
	require.Equal(t, []uuid.UUID{store.machine.ID}, outboxes.machines)
	require.Len(t, outboxes.recorded, 1)
	require.Contains(t, outboxes.events, "agent_messages.recorded")
	require.NoError(t, manager.syncMachinePorts(context.Background(), store.machine, store.services))
	require.Len(t, outboxes.machines, 1, "an empty outbox records nothing")
}
