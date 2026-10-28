package service

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestTaskAttachmentsGetSafeBoundedWorktreePaths(t *testing.T) {
	task, run := uuid.New(), uuid.New()
	assets := []domain.Asset{
		{ID: uuid.New(), StorageKey: "a.png", Filename: "../../Screen Shot 2026.png", ContentType: "image/png", Size: 10},
		{ID: uuid.New(), StorageKey: "b.jpg", Filename: "", ContentType: "image/jpeg", Size: 20},
		{ID: uuid.New(), StorageKey: "c.webp", Filename: "日本.webp", ContentType: "image/webp", Size: domain.AgentTaskAttachmentsMaxBytes},
		{ID: uuid.New(), StorageKey: "d.gif", Filename: "loop.gif", ContentType: "image/gif", Size: 30},
	}
	items := taskAttachments(task, run, assets)
	require.Len(t, items, 3, "an image beyond the byte budget is left out")
	require.Equal(t, ".kuayle/attachments/01-Screen-Shot-2026.png", items[0].Path)
	require.Equal(t, ".kuayle/attachments/02-image.jpg", items[1].Path)
	require.Equal(t, ".kuayle/attachments/03-loop.gif", items[2].Path)
	for i, item := range items {
		require.Equal(t, i, item.Position)
		require.Equal(t, run, item.RunID)
		require.Regexp(t, `^\.kuayle/attachments/[A-Za-z0-9._-]{1,120}$`, item.Path)
	}
	many := make([]domain.Asset, 15)
	for i := range many {
		many[i] = domain.Asset{ID: uuid.New(), StorageKey: "x.png", Filename: "x.png", ContentType: "image/png", Size: 1}
	}
	require.Len(t, taskAttachments(task, run, many), domain.AgentTaskAttachmentsMax)
}

func TestTaskPromptListsAttachedImagesByPath(t *testing.T) {
	require.Equal(t, "Fix it", withAttachments("Fix it", nil))
	items := []domain.AgentTaskAttachment{{Path: ".kuayle/attachments/01-crash.png", ContentType: "image/png", Size: 2048}}
	prompt := withAttachments("Fix it", items)
	require.True(t, strings.HasPrefix(prompt, "Fix it\n\n"))
	require.Contains(t, prompt, ".kuayle/attachments/01-crash.png (image/png, 2 KB)")
	require.Contains(t, prompt, "not part of the repository")
	huge := withAttachments(strings.Repeat("t", 32000), items)
	require.Equal(t, strings.Repeat("t", 32000), huge, "the task is never pushed past the attempt limit")
}
