package machine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"regexp"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	log "github.com/sirupsen/logrus"
)

// AttachmentFile is one issue image written into a task worktree.
type AttachmentFile struct {
	Path string
	Data []byte
}

// attachmentStager is implemented by runtimes that can write task images.
type attachmentStager interface {
	StageAttachments(ctx context.Context, services []domain.DevMachineService, checkout *domain.DevMachineCheckout, files []AttachmentFile) error
}

type runAttachmentStore interface {
	ListRunAttachments(ctx context.Context, run uuid.UUID) ([]domain.AgentTaskAttachment, error)
}

// attachmentReader reads stored objects; storage.Backend satisfies it.
type attachmentReader interface {
	Get(ctx context.Context, key string) (io.ReadCloser, error)
}

// SetAttachmentStorage lets the manager copy a task's issue images from
// Kuayle's storage into the worktree before the agent starts. Without it no
// images are copied.
func (m *Manager) SetAttachmentStorage(files attachmentReader) {
	m.attachmentFiles = files
}

var attachmentPath = regexp.MustCompile(`^\.kuayle/attachments/[A-Za-z0-9._-]{1,120}$`)
var errStoredAttachmentUnavailable = errors.New("stored attachment is unavailable")

// stageAttachments copies the run's recorded images. Failure never blocks the
// run: the agent is told by path and can report a missing file.
func (m *Manager) stageAttachments(ctx context.Context, machine *domain.DevMachine, run *domain.DevMachineAgentRun, checkout *domain.DevMachineCheckout) {
	store, ok := m.store.(runAttachmentStore)
	stager, staging := m.runtime.(attachmentStager)
	if !ok || !staging || m.attachmentFiles == nil || checkout == nil {
		return
	}
	items, err := store.ListRunAttachments(ctx, run.ID)
	if err != nil || len(items) == 0 {
		return
	}
	files, err := m.readAttachments(ctx, items)
	if err == nil {
		var services []domain.DevMachineService
		if services, err = m.store.ListServices(ctx, machine.WorkspaceID, machine.ID); err == nil {
			err = stager.StageAttachments(ctx, services, checkout, files)
		}
	}
	if err != nil {
		log.WithFields(log.Fields{"machine_id": machine.ID, "agent_run_id": run.ID}).WithError(err).Warn("task images could not be copied")
		_ = m.createEvent(ctx, machine, &run.ID, "agent", "attachments.unavailable", map[string]any{"count": len(items)})
		return
	}
	_ = m.createEvent(ctx, machine, &run.ID, "agent", "attachments.copied", map[string]any{"count": len(files)})
}

func (m *Manager) readAttachments(ctx context.Context, items []domain.AgentTaskAttachment) ([]AttachmentFile, error) {
	files := make([]AttachmentFile, 0, len(items))
	var total int64
	for _, item := range items {
		if len(files) == domain.AgentTaskAttachmentsMax || !attachmentPath.MatchString(item.Path) || total+item.Size > domain.AgentTaskAttachmentsMaxBytes {
			return nil, fmt.Errorf("invalid task attachment")
		}
		data, err := m.readAttachment(ctx, item.StorageKey, item.Size)
		if err != nil {
			return nil, err
		}
		total += item.Size
		files = append(files, AttachmentFile{Path: item.Path, Data: data})
	}
	return files, nil
}

func (m *Manager) readAttachment(ctx context.Context, key string, size int64) ([]byte, error) {
	if size <= 0 || size > domain.ConversationAttachmentMaxBytes {
		return nil, fmt.Errorf("invalid attachment size")
	}
	reader, err := m.attachmentFiles.Get(ctx, key)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("%w: %w", errStoredAttachmentUnavailable, err)
	}
	if err != nil {
		return nil, fmt.Errorf("read attachment: %w", err)
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, size+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) != size {
		return nil, fmt.Errorf("%w: size changed", errStoredAttachmentUnavailable)
	}
	return data, nil
}

// stageAttachmentsScript replaces the worktree's .kuayle/attachments and
// excludes .kuayle/ through the repository's info/exclude (shared by its
// worktrees), never through a tracked .gitignore.
const stageAttachmentsScript = excludeKuayleScript + `rm -rf "$root/.kuayle/attachments"
mkdir -p "$root/.kuayle/attachments"`

const writeAttachmentScript = `set -eu
umask 022
cat > "$1/$2"`

func (r *DockerRuntime) StageAttachments(ctx context.Context, services []domain.DevMachineService, checkout *domain.DevMachineCheckout, files []AttachmentFile) error {
	developerContainer := developerContainerID(services)
	if developerContainer == "" {
		return fmt.Errorf("developer container is unavailable")
	}
	if _, err := r.execOutput(ctx, developerContainer, []string{"/bin/sh", "-c", stageAttachmentsScript, "sh", checkout.WorkspacePath}); err != nil {
		return err
	}
	for _, file := range files {
		if !attachmentPath.MatchString(file.Path) {
			return fmt.Errorf("invalid task attachment path")
		}
		if err := r.execWithInput(ctx, developerContainer, []string{"/bin/sh", "-c", writeAttachmentScript, "sh", checkout.WorkspacePath, file.Path}, string(file.Data)); err != nil {
			return err
		}
	}
	return nil
}
