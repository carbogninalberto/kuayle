package service

import (
	"context"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

type portStoreFake struct {
	*devMachineStoreFake
	ports   []domain.DevMachinePort
	deleted []uuid.UUID
}

func (f *portStoreFake) ListPorts(_ context.Context, workspaceID, machineID uuid.UUID) ([]domain.DevMachinePort, error) {
	ports := []domain.DevMachinePort{}
	for _, port := range f.ports {
		if port.WorkspaceID == workspaceID && port.MachineID == machineID {
			ports = append(ports, port)
		}
	}
	return ports, nil
}
func (f *portStoreFake) GetPort(_ context.Context, workspaceID, machineID, portID uuid.UUID) (*domain.DevMachinePort, error) {
	for i := range f.ports {
		if f.ports[i].ID == portID && f.ports[i].WorkspaceID == workspaceID && f.ports[i].MachineID == machineID {
			port := f.ports[i]
			return &port, nil
		}
	}
	return nil, nil
}
func (f *portStoreFake) CreatePort(_ context.Context, port *domain.DevMachinePort) error {
	for _, existing := range f.ports {
		if existing.MachineID == port.MachineID && existing.Port == port.Port {
			return repository.ErrPortExists
		}
	}
	port.ID, port.CreatedAt = uuid.New(), time.Now()
	f.ports = append(f.ports, *port)
	return nil
}
func (f *portStoreFake) UpdatePortLabel(_ context.Context, workspaceID, machineID, portID uuid.UUID, label *string) (*domain.DevMachinePort, error) {
	for i := range f.ports {
		if f.ports[i].ID == portID && f.ports[i].WorkspaceID == workspaceID && f.ports[i].MachineID == machineID {
			f.ports[i].Label = label
			port := f.ports[i]
			return &port, nil
		}
	}
	return nil, nil
}
func (f *portStoreFake) DeletePort(ctx context.Context, workspaceID, machineID, portID, _ uuid.UUID) (*domain.DevMachinePort, error) {
	port, _ := f.GetPort(ctx, workspaceID, machineID, portID)
	if port == nil {
		return nil, nil
	}
	f.deleted = append(f.deleted, portID)
	kept := f.ports[:0]
	for _, existing := range f.ports {
		if existing.ID != portID {
			kept = append(kept, existing)
		}
	}
	f.ports = kept
	return port, nil
}

func newPortService(t *testing.T) (*DevMachineService, *portStoreFake, uuid.UUID, uuid.UUID, uuid.UUID, domain.DevMachineService) {
	t.Helper()
	workspaceID, userID, machineID := uuid.New(), uuid.New(), uuid.New()
	containerID := "developer"
	ide := domain.DevMachineService{ID: uuid.New(), MachineID: machineID, ServiceKey: "ide", ServiceType: "ide", Status: "running", InternalPort: 8080, ContainerID: &containerID}
	store := &portStoreFake{devMachineStoreFake: &devMachineStoreFake{
		policy: testPolicy(workspaceID),
		machine: &domain.DevMachine{ID: machineID, WorkspaceID: workspaceID, CreatedByUserID: &userID, RoutingKey: "0123456789abcdef0123",
			Status: domain.DevMachineStatusRunning, DesiredStatus: domain.DevMachineStatusRunning,
			ListeningPorts: pq.Int64Array{5173, 9229}},
		services: []domain.DevMachineService{ide},
	}}
	return newTestDevMachineService(store), store, workspaceID, userID, machineID, ide
}

// Only the machine's creator exposes ports; ports of the machine's own services
// are refused, and each port is launched on its own host.
func TestAppPortsAreOwnerOnlyAvoidServicePortsAndLaunchOnTheirOwnHost(t *testing.T) {
	svc, store, workspaceID, userID, machineID, ide := newPortService(t)
	ctx := context.Background()
	label := "web"
	_, err := svc.CreatePort(ctx, workspaceID, machineID, uuid.New(), 5173, &label)
	require.ErrorIs(t, err, ErrMachineNotFound, "only the machine owner exposes its ports")
	_, err = svc.CreatePort(ctx, workspaceID, machineID, userID, 8080, nil)
	require.ErrorIs(t, err, ErrPortReserved)
	for _, port := range []int{80, 70000} {
		_, err = svc.CreatePort(ctx, workspaceID, machineID, userID, port, nil)
		require.ErrorIs(t, err, ErrInvalidMachineInput, port)
	}
	bad := "<b>"
	_, err = svc.CreatePort(ctx, workspaceID, machineID, userID, 5173, &bad)
	require.ErrorIs(t, err, ErrInvalidMachineInput)

	created, err := svc.CreatePort(ctx, workspaceID, machineID, userID, 5173, &label)
	require.NoError(t, err)
	require.Equal(t, dto.AppPort{ID: created.ID, Port: 5173, Label: &label, Source: "user", Host: "0123456789abcdef0123-5173.machines.example.test", Listening: true, CreatedAt: created.CreatedAt}, *created)
	_, err = svc.CreatePort(ctx, workspaceID, machineID, userID, 5173, nil)
	require.ErrorIs(t, err, ErrPortExists)

	list, err := svc.ListPorts(ctx, workspaceID, machineID, userID, false)
	require.NoError(t, err)
	require.Len(t, list.Ports, 1)
	require.Equal(t, []int{5173, 9229}, list.Listening, "every detected port, exposed or not")
	_, err = svc.ListPorts(ctx, workspaceID, machineID, uuid.New(), false)
	require.ErrorIs(t, err, ErrMachineNotFound)
	admin, err := svc.ListPorts(ctx, workspaceID, machineID, uuid.New(), true)
	require.NoError(t, err, "workspace administrators may read")
	require.Len(t, admin.Ports, 1)
	_, err = svc.ListPorts(ctx, uuid.New(), machineID, uuid.New(), true)
	require.ErrorIs(t, err, ErrMachineNotFound, "but only in their workspace")

	renamed, err := svc.UpdatePort(ctx, workspaceID, machineID, created.ID, userID, nil)
	require.NoError(t, err)
	require.Nil(t, renamed.Label)
	_, err = svc.UpdatePort(ctx, workspaceID, machineID, created.ID, uuid.New(), &label)
	require.ErrorIs(t, err, ErrMachineNotFound)
	_, err = svc.UpdatePort(ctx, workspaceID, machineID, uuid.New(), userID, &label)
	require.ErrorIs(t, err, ErrPortNotFound)

	launch, err := svc.LaunchPort(ctx, workspaceID, machineID, created.ID, userID)
	require.NoError(t, err)
	require.Equal(t, "ready", launch.Status)
	parsed, err := url.Parse(launch.LaunchURL)
	require.NoError(t, err)
	require.Equal(t, "0123456789abcdef0123-5173.machines.example.test", parsed.Host)
	require.NotEmpty(t, parsed.Query().Get("ticket"))
	ticket := store.createdTicket
	require.Equal(t, ide.ID, ticket.ServiceID, "the ticket names the developer service it is served by")
	require.Equal(t, parsed.Host, ticket.BoundHost, "and is bound to the port's host only")
	require.Equal(t, userID, ticket.UserID)
	_, err = svc.LaunchPort(ctx, workspaceID, machineID, created.ID, uuid.New())
	require.ErrorIs(t, err, ErrMachineNotFound)

	require.ErrorIs(t, svc.DeletePort(ctx, workspaceID, machineID, created.ID, uuid.New()), ErrMachineNotFound)
	require.NoError(t, svc.DeletePort(ctx, workspaceID, machineID, created.ID, userID))
	require.Equal(t, []uuid.UUID{created.ID}, store.deleted)
	require.ErrorIs(t, svc.DeletePort(ctx, workspaceID, machineID, created.ID, userID), ErrPortNotFound)
	_, err = svc.LaunchPort(ctx, workspaceID, machineID, created.ID, userID)
	require.ErrorIs(t, err, ErrPortNotFound)
}

func TestAppPortsAreNotLimitedInNumber(t *testing.T) {
	svc, store, workspaceID, userID, machineID, _ := newPortService(t)
	for port := 3000; port < 3050; port++ {
		_, err := svc.CreatePort(context.Background(), workspaceID, machineID, userID, port, nil)
		require.NoError(t, err)
	}
	require.Len(t, store.ports, 50)
}
