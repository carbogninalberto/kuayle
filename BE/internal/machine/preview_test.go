package machine

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

type previewStoreFake struct {
	*managerStoreFake
	argv     []string
	port     int
	creator  uuid.UUID
	inReview bool
	saved    *domain.DevMachinePreview
}

func (f *previewStoreFake) TaskPreview(context.Context, uuid.UUID) ([]string, int, uuid.UUID, bool, error) {
	return f.argv, f.port, f.creator, f.inReview, nil
}
func (f *previewStoreFake) SetPreview(_ context.Context, preview *domain.DevMachinePreview) error {
	f.saved = preview
	return nil
}

type previewRuntimeFake struct {
	*runtimeFake
	argv []string
	port int
	cwd  string
}

func (f *previewRuntimeFake) StartPreview(_ context.Context, _ *domain.DevMachine, _ []domain.DevMachineService, checkout *domain.DevMachineCheckout, argv []string, port int) error {
	f.argv, f.port, f.cwd = argv, port, checkout.WorkspacePath
	return nil
}

func TestStartPreviewRunsInTheTaskWorktreeAndPointsThePreviewAtIt(t *testing.T) {
	checkoutID, creator := uuid.New(), uuid.New()
	machine := &domain.DevMachine{ID: uuid.New(), WorkspaceID: uuid.New(), Status: domain.DevMachineStatusRunning, ExpiresAt: time.Now().Add(time.Hour)}
	newManager := func(services []domain.DevMachineService, inReview bool) (*Manager, *previewStoreFake, *previewRuntimeFake) {
		store := &previewStoreFake{managerStoreFake: &managerStoreFake{machine: machine, services: services,
			checkout: &domain.DevMachineCheckout{ID: checkoutID, MachineID: machine.ID, WorkspaceID: machine.WorkspaceID, Status: "ready", WorkspacePath: "/workspace/tasks/eng-1"}},
			argv: []string{"npm", "run", "dev"}, port: 5173, creator: creator, inReview: inReview}
		runtime := &previewRuntimeFake{runtimeFake: &runtimeFake{}}
		return &Manager{store: store, runtime: runtime}, store, runtime
	}
	op := &domain.DevMachineOperation{CheckoutID: &checkoutID}

	manager, store, runtime := newManager(nil, true)
	require.NoError(t, manager.startPreview(context.Background(), machine, op))
	require.Equal(t, []string{"npm", "run", "dev"}, runtime.argv)
	require.Equal(t, "/workspace/tasks/eng-1", runtime.cwd)
	require.Equal(t, &domain.DevMachinePreview{MachineID: machine.ID, WorkspaceID: machine.WorkspaceID, Port: 5173, CreatedByUserID: creator}, store.saved)

	manager, store, runtime = newManager([]domain.DevMachineService{{ServiceType: "ide", InternalPort: 5173}}, true)
	err := manager.startPreview(context.Background(), machine, op)
	require.ErrorContains(t, err, "preview_port_reserved")
	require.Nil(t, runtime.argv)
	require.Nil(t, store.saved)

	manager, store, runtime = newManager(nil, false)
	require.NoError(t, manager.startPreview(context.Background(), machine, op), "a task that left review needs no preview")
	require.Nil(t, runtime.argv)
	require.Nil(t, store.saved)
}
