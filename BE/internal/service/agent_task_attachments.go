package service

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

// taskAttachmentSource lists an issue's images and keeps the list fixed per
// attempt, so the prompt and the files copied into the worktree match.
type taskAttachmentSource interface {
	IssueImages(ctx context.Context, task domain.AgentTask, limit int) ([]domain.Asset, error)
	RunAttachments(ctx context.Context, task domain.AgentTask) ([]domain.AgentTaskAttachment, error)
	RecordRunAttachments(ctx context.Context, task domain.AgentTask, items []domain.AgentTaskAttachment) ([]domain.AgentTaskAttachment, error)
}

const taskAttachmentDir = ".kuayle/attachments/"

var unsafeAttachmentName = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// taskAttachments names each image by position under .kuayle/attachments,
// within the per-attempt count and byte budget.
func taskAttachments(task, run uuid.UUID, assets []domain.Asset) []domain.AgentTaskAttachment {
	items := []domain.AgentTaskAttachment{}
	var total int64
	for _, asset := range assets {
		ext, ok := domain.ConversationAttachmentTypes[asset.ContentType]
		if !ok || asset.Size <= 0 || asset.Size > domain.ConversationAttachmentMaxBytes || total+asset.Size > domain.AgentTaskAttachmentsMaxBytes {
			continue
		}
		if len(items) == domain.AgentTaskAttachmentsMax {
			break
		}
		name := filepath.Base(strings.ReplaceAll(asset.Filename, "\\", "/"))
		name = strings.TrimSuffix(name, filepath.Ext(name))
		name = strings.Trim(unsafeAttachmentName.ReplaceAllString(name, "-"), "-.")
		if len(name) > 80 {
			name = strings.Trim(name[:80], "-.")
		}
		if name == "" {
			name = "image"
		}
		id := asset.ID
		total += asset.Size
		items = append(items, domain.AgentTaskAttachment{
			TaskID: task, RunID: run, Position: len(items), AssetID: &id, StorageKey: asset.StorageKey,
			Path: fmt.Sprintf("%s%02d-%s%s", taskAttachmentDir, len(items)+1, name, ext), ContentType: asset.ContentType, Size: asset.Size,
		})
	}
	return items
}

// withAttachments lists the copied images after the task. They are left out
// rather than pushing the prompt past the attempt limit.
func withAttachments(prompt string, items []domain.AgentTaskAttachment) string {
	if len(items) == 0 {
		return prompt
	}
	var b strings.Builder
	b.WriteString("\n\nImages attached to the issue were copied into your working directory (relative to the repository root). Open them by path to see them; they are not part of the repository and are excluded from commits:\n")
	for _, item := range items {
		fmt.Fprintf(&b, "- %s (%s, %d KB)\n", item.Path, item.ContentType, max(1, (item.Size+1023)/1024))
	}
	if len(prompt)+b.Len() > 32000 {
		return prompt
	}
	return prompt + strings.TrimRight(b.String(), "\n")
}

// attemptAttachments returns the attempt's recorded images, recording the
// issue's current images on the first call.
func attemptAttachments(ctx context.Context, source taskAttachmentSource, task domain.AgentTask) ([]domain.AgentTaskAttachment, error) {
	items, err := source.RunAttachments(ctx, task)
	if err != nil || len(items) > 0 {
		return items, err
	}
	assets, err := source.IssueImages(ctx, task, domain.AgentTaskAttachmentsMax*2)
	if err != nil {
		return nil, err
	}
	items = taskAttachments(task.ID, task.RunID, assets)
	if len(items) == 0 {
		return nil, nil
	}
	return source.RecordRunAttachments(ctx, task, items)
}
