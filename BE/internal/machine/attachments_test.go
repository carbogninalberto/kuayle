package machine

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@test.invalid", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@test.invalid")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	return string(out)
}

// The staging script runs inside the developer container; here it runs against
// a real repository and task worktree to prove the images never reach a commit
// and the tracked tree is untouched.
func TestStageAttachmentsScriptExcludesImagesFromTheWorktree(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is unavailable")
	}
	repo := t.TempDir()
	git(t, repo, "init", "-q", "-b", "main")
	require.NoError(t, os.WriteFile(filepath.Join(repo, "README.md"), []byte("hi\n"), 0o644))
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-qm", "init")
	worktree := filepath.Join(t.TempDir(), "task")
	git(t, repo, "worktree", "add", "-q", "-b", "kuayle/task", worktree)
	require.NoError(t, os.MkdirAll(filepath.Join(worktree, ".kuayle/attachments"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(worktree, ".kuayle/attachments/old.png"), []byte("stale"), 0o644))

	for range 2 {
		out, err := exec.Command("/bin/sh", "-c", stageAttachmentsScript, "sh", worktree).CombinedOutput()
		require.NoError(t, err, string(out))
	}
	write := exec.Command("/bin/sh", "-c", writeAttachmentScript, "sh", worktree, ".kuayle/attachments/01-shot.png")
	write.Stdin = bytes.NewReader([]byte("png-bytes"))
	out, err := write.CombinedOutput()
	require.NoError(t, err, string(out))

	data, err := os.ReadFile(filepath.Join(worktree, ".kuayle/attachments/01-shot.png"))
	require.NoError(t, err)
	require.Equal(t, "png-bytes", string(data))
	_, err = os.Stat(filepath.Join(worktree, ".kuayle/attachments/old.png"))
	require.True(t, os.IsNotExist(err), "a previous attempt's images are replaced")
	require.Empty(t, strings.TrimSpace(git(t, worktree, "status", "--porcelain", "--untracked-files=all")))
	git(t, worktree, "add", "-A")
	require.Empty(t, strings.TrimSpace(git(t, worktree, "diff", "--cached", "--name-only")), "images cannot be committed")
	_, err = os.Stat(filepath.Join(worktree, ".gitignore"))
	require.True(t, os.IsNotExist(err), "the repository's .gitignore is never written")
	exclude, err := os.ReadFile(filepath.Join(repo, ".git/info/exclude"))
	require.NoError(t, err)
	require.Equal(t, 1, strings.Count(string(exclude), "/.kuayle/"), "the exclusion is added once")
}

type stagingStore struct {
	ManagerStore
	items  []domain.AgentTaskAttachment
	events []string
}

func (s *stagingStore) ListRunAttachments(context.Context, uuid.UUID) ([]domain.AgentTaskAttachment, error) {
	return s.items, nil
}
func (s *stagingStore) ListServices(context.Context, uuid.UUID, uuid.UUID) ([]domain.DevMachineService, error) {
	return nil, nil
}
func (s *stagingStore) CreateEvent(_ context.Context, event *domain.DevMachineEvent) error {
	s.events = append(s.events, event.EventType)
	return nil
}

type stagingRuntime struct {
	Runtime
	files []AttachmentFile
}

func (r *stagingRuntime) StageAttachments(_ context.Context, _ []domain.DevMachineService, _ *domain.DevMachineCheckout, files []AttachmentFile) error {
	r.files = files
	return nil
}

type stagingFiles map[string][]byte

func (f stagingFiles) Get(_ context.Context, key string) (io.ReadCloser, error) {
	data, ok := f[key]
	if !ok {
		return nil, os.ErrNotExist
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func TestManagerCopiesRecordedTaskImagesWithinBounds(t *testing.T) {
	machine := &domain.DevMachine{ID: uuid.New(), WorkspaceID: uuid.New()}
	run := &domain.DevMachineAgentRun{ID: uuid.New()}
	checkout := &domain.DevMachineCheckout{WorkspacePath: "/workspace/tasks/eng-1-task"}
	store := &stagingStore{items: []domain.AgentTaskAttachment{{Path: ".kuayle/attachments/01-a.png", StorageKey: "a.png", Size: 3}}}
	runtime := &stagingRuntime{}
	manager := &Manager{store: store, runtime: runtime}

	manager.stageAttachments(context.Background(), machine, run, checkout)
	require.Nil(t, runtime.files, "nothing is copied without attachment storage")

	manager.SetAttachmentStorage(stagingFiles{"a.png": []byte("abc")})
	manager.stageAttachments(context.Background(), machine, run, checkout)
	require.Equal(t, []AttachmentFile{{Path: ".kuayle/attachments/01-a.png", Data: []byte("abc")}}, runtime.files)
	require.Equal(t, []string{"attachments.copied"}, store.events)

	for name, item := range map[string]domain.AgentTaskAttachment{
		"escaping path": {Path: ".kuayle/attachments/../../.git/config", StorageKey: "a.png", Size: 3},
		"changed size":  {Path: ".kuayle/attachments/01-a.png", StorageKey: "a.png", Size: 2},
		"oversized":     {Path: ".kuayle/attachments/01-a.png", StorageKey: "a.png", Size: domain.ConversationAttachmentMaxBytes + 1},
		"missing":       {Path: ".kuayle/attachments/01-a.png", StorageKey: "gone.png", Size: 3},
	} {
		runtime.files, store.events = nil, nil
		store.items = []domain.AgentTaskAttachment{item}
		manager.stageAttachments(context.Background(), machine, run, checkout)
		require.Nil(t, runtime.files, name)
		require.Equal(t, []string{"attachments.unavailable"}, store.events, name)
	}
}
