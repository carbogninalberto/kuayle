package dto_test

import (
	"encoding/json"
	"testing"

	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/pkg/validate"
	"github.com/stretchr/testify/require"
)

func TestUpdatePreferencesRequestNotificationFields(t *testing.T) {
	var req dto.UpdatePreferencesRequest
	require.NoError(t, json.Unmarshal([]byte(`{"browser_notifications":false,"notification_sound":"bell","notification_events":{"comment":false}}`), &req))
	require.NoError(t, validate.Struct(&req))
	require.False(t, *req.BrowserNotifications)
	require.Equal(t, "bell", *req.NotificationSound)
	require.Equal(t, map[string]bool{"comment": false}, *req.NotificationEvents)

	for _, sound := range []string{"off", "chime", "bell", "pop", "drop", "pulse"} {
		require.NoError(t, validate.Struct(&dto.UpdatePreferencesRequest{NotificationSound: &sound}), sound)
	}
	for _, sound := range []string{"gong", "Chime"} {
		require.Error(t, validate.Struct(&dto.UpdatePreferencesRequest{NotificationSound: &sound}), sound)
	}
	// Absent fields are left unchanged.
	require.NoError(t, validate.Struct(&dto.UpdatePreferencesRequest{}))
}

func TestUpdatePreferencesRequestWorkflowEditorPanel(t *testing.T) {
	var req dto.UpdatePreferencesRequest
	require.NoError(t, json.Unmarshal([]byte(`{"workflow_editor_panel_open":false}`), &req))
	require.NoError(t, validate.Struct(&req))
	require.NotNil(t, req.WorkflowEditorPanelOpen)
	require.False(t, *req.WorkflowEditorPanelOpen)

	// Absent fields are left unchanged.
	var empty dto.UpdatePreferencesRequest
	require.NoError(t, json.Unmarshal([]byte(`{}`), &empty))
	require.Nil(t, empty.WorkflowEditorPanelOpen)
}
