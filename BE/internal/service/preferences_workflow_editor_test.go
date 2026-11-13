package service

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/stretchr/testify/require"
)

func TestPreferencesWorkflowEditorPanelDefaultsOpenForUserWithoutRow(t *testing.T) {
	svc := NewPreferencesService(&preferencesRepoFake{})

	prefs, err := svc.Get(context.Background(), uuid.New())

	require.NoError(t, err)
	require.True(t, prefs.WorkflowEditorPanelOpen)
}

func TestPreferencesWorkflowEditorPanelUpdate(t *testing.T) {
	repo := &preferencesRepoFake{}
	svc := NewPreferencesService(repo)
	ctx, user := context.Background(), uuid.New()

	closed := false
	prefs, err := svc.Update(ctx, user, dto.UpdatePreferencesRequest{WorkflowEditorPanelOpen: &closed})

	require.NoError(t, err)
	require.False(t, prefs.WorkflowEditorPanelOpen)
	require.False(t, repo.stored.WorkflowEditorPanelOpen)

	// Updating something else leaves the panel preference alone.
	size := "large"
	prefs, err = svc.Update(ctx, user, dto.UpdatePreferencesRequest{FontSize: &size})
	require.NoError(t, err)
	require.False(t, prefs.WorkflowEditorPanelOpen, "an omitted field keeps the stored value")
	require.False(t, repo.stored.WorkflowEditorPanelOpen)
}
