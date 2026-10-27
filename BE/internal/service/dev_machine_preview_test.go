package service

import (
	"context"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

type previewStoreFake struct {
	*devMachineStoreFake
	preview      *domain.DevMachinePreview
	revokedHosts []string
}

func (f *previewStoreFake) GetPreview(context.Context, uuid.UUID, uuid.UUID) (*domain.DevMachinePreview, error) {
	return f.preview, nil
}
func (f *previewStoreFake) SetPreview(_ context.Context, preview *domain.DevMachinePreview) error {
	f.preview = preview
	return nil
}
func (f *previewStoreFake) DeletePreview(_ context.Context, _, _ uuid.UUID, host string) error {
	f.preview = nil
	f.revokedHosts = append(f.revokedHosts, host)
	return nil
}

func TestPreviewIsOwnerOnlyAvoidsServicePortsAndLaunchesOnItsOwnHost(t *testing.T) {
	workspaceID, userID, machineID := uuid.New(), uuid.New(), uuid.New()
	containerID := "developer"
	ide := domain.DevMachineService{ID: uuid.New(), MachineID: machineID, ServiceKey: "ide", ServiceType: "ide", Status: "running", InternalPort: 8080, ContainerID: &containerID}
	store := &previewStoreFake{devMachineStoreFake: &devMachineStoreFake{
		policy:   testPolicy(workspaceID),
		machine:  &domain.DevMachine{ID: machineID, WorkspaceID: workspaceID, CreatedByUserID: &userID, RoutingKey: "0123456789abcdef0123", Status: domain.DevMachineStatusRunning, DesiredStatus: domain.DevMachineStatusRunning, ExpiresAt: time.Now().Add(time.Hour)},
		services: []domain.DevMachineService{ide},
	}}
	svc := newTestDevMachineService(store)
	ctx := context.Background()
	_, err := svc.LaunchPreview(ctx, workspaceID, machineID, userID)
	require.ErrorIs(t, err, ErrServiceNotAvailable, "no preview configured yet")
	_, err = svc.SetPreview(ctx, workspaceID, machineID, uuid.New(), 5173)
	require.Error(t, err, "only the machine owner manages its preview")
	for _, port := range []int{80, 70000, 8080} {
		_, err = svc.SetPreview(ctx, workspaceID, machineID, userID, port)
		require.ErrorIs(t, err, ErrInvalidMachineInput, port)
	}
	preview, err := svc.SetPreview(ctx, workspaceID, machineID, userID, 5173)
	require.NoError(t, err)
	require.Equal(t, 5173, preview.Port)

	launch, err := svc.LaunchPreview(ctx, workspaceID, machineID, userID)
	require.NoError(t, err)
	require.Equal(t, "ready", launch.Status)
	parsed, err := url.Parse(launch.LaunchURL)
	require.NoError(t, err)
	require.Equal(t, "0123456789abcdef0123-preview."+svc.domain, parsed.Host)
	require.NotEmpty(t, parsed.Query().Get("ticket"))
	require.Empty(t, parsed.Query().Get("folder"))
	ticket := store.createdTicket
	require.Equal(t, ide.ID, ticket.ServiceID, "the ticket names the developer service it is served by")
	require.Equal(t, parsed.Host, ticket.BoundHost, "and is bound to the preview host only")
	require.Equal(t, userID, ticket.UserID)

	require.NoError(t, svc.DeletePreview(ctx, workspaceID, machineID, userID))
	require.Equal(t, []string{parsed.Host}, store.revokedHosts, "deleting revokes preview sessions")
	_, err = svc.LaunchPreview(ctx, workspaceID, machineID, userID)
	require.ErrorIs(t, err, ErrServiceNotAvailable)
}
