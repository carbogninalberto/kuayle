package machine

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/agent"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// imageStoreFake records the image update calls on top of managerStoreFake.
type imageStoreFake struct {
	*managerStoreFake
	busy            bool
	busyChecks      int
	queued          []*domain.DevMachineOperation
	providerImages  map[uuid.UUID]string
	replacedService []uuid.UUID
}

func (f *imageStoreFake) DockerAllowed(context.Context, uuid.UUID) (bool, error) {
	return true, nil
}

func (f *imageStoreFake) MachineBusyForImageUpdate(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	f.busyChecks++
	return f.busy, nil
}

func (f *imageStoreFake) EnqueueImageUpdate(_ context.Context, operation *domain.DevMachineOperation) (bool, error) {
	f.queued = append(f.queued, operation)
	return true, nil
}

func (f *imageStoreFake) ReplaceServiceImage(_ context.Context, serviceID uuid.UUID, image string) error {
	f.replacedService = append(f.replacedService, serviceID)
	for i := range f.services {
		if f.services[i].ID == serviceID {
			f.services[i].ImageRef = image
			f.services[i].ContainerID = nil
			f.services[i].Status = "pending"
		}
	}
	return nil
}

func (f *imageStoreFake) UpdateProviderImage(_ context.Context, id uuid.UUID, image string) error {
	if f.providerImages == nil {
		f.providerImages = map[uuid.UUID]string{}
	}
	f.providerImages[id] = image
	return nil
}

type imageRuntimeFake struct {
	*runtimeFake
	removed          [][]string
	finishedRemovals int
}

func (f *imageRuntimeFake) RemoveContainers(_ context.Context, _ *domain.DevMachine, ids []string) error {
	f.removed = append(f.removed, ids)
	return nil
}

func (f *imageRuntimeFake) RemoveFinishedRunContainers(context.Context, *domain.DevMachine) error {
	f.finishedRemovals++
	return nil
}

var releaseImages = MachineImages{
	IDE: "registry/ide@sha256:new", Headless: "registry/headless@sha256:new", Browser: "registry/browser@sha256:new",
	Collector: "registry/collector@sha256:new", Egress: "registry/egress@sha256:new", Docker: "registry/docker@sha256:new",
}

func imageService(machineID uuid.UUID, serviceType, key, image, container string) domain.DevMachineService {
	return domain.DevMachineService{ID: uuid.New(), MachineID: machineID, ServiceType: serviceType, ServiceKey: key, ImageRef: image, ContainerID: &container, Status: "running"}
}

func imageFixture(status domain.DevMachineStatus, profile string, services func(uuid.UUID) []domain.DevMachineService) (*imageStoreFake, *imageRuntimeFake, *Manager) {
	machineID, workspaceID := uuid.New(), uuid.New()
	store := &imageStoreFake{managerStoreFake: &managerStoreFake{
		machine: &domain.DevMachine{ID: machineID, WorkspaceID: workspaceID, RoutingKey: "0123456789abcdef0123", Status: status,
			DesiredStatus: domain.DevMachineStatusRunning, Profile: profile, Generation: 4},
		services: services(machineID),
	}}
	runtime := &imageRuntimeFake{runtimeFake: &runtimeFake{}}
	manager := NewManager(store, runtime, agent.NewRegistry(agent.NewClaudeCodeProvider("registry/agent-claude@sha256:new")), nil, make([]byte, 32), make([]byte, 32), "test")
	manager.SetImages(releaseImages)
	return store, runtime, manager
}

func headlessServices(image string) func(uuid.UUID) []domain.DevMachineService {
	return func(machineID uuid.UUID) []domain.DevMachineService {
		return []domain.DevMachineService{
			imageService(machineID, "collector", "collector", "local/collector:v0.2.4", "c-collector"),
			imageService(machineID, "egress", "egress", releaseImages.Egress, "c-egress"),
			imageService(machineID, "docker", "docker", "local/docker:v0.2.4", "c-docker"),
			imageService(machineID, "terminal", "terminal", image, "c-terminal"),
		}
	}
}

func eventTypes(events []domain.DevMachineEvent) []string {
	types := make([]string, 0, len(events))
	for _, event := range events {
		types = append(types, event.EventType)
	}
	return types
}

// A stopped machine runs no processes, so starting it recreates its outdated
// containers from the installed release's images instead of restarting them.
func TestManagerStartMovesStoppedMachineToReleaseImages(t *testing.T) {
	store, runtime, manager := imageFixture(domain.DevMachineStatusStopped, domain.DevMachineProfileHeadless, headlessServices("local/headless:v0.2.4"))

	err := manager.processOperation(context.Background(), &domain.DevMachineOperation{
		MachineID: store.machine.ID, WorkspaceID: store.machine.WorkspaceID, Action: domain.DevMachineOpStart, Generation: 4,
	})

	require.NoError(t, err)
	require.Equal(t, [][]string{{"c-collector", "c-docker", "c-terminal"}}, runtime.removed, "the current egress container is kept")
	require.Equal(t, 1, runtime.spawned, "the removed containers are created again")
	require.Zero(t, runtime.started)
	require.Equal(t, domain.DevMachineStatusRunning, store.machine.Status)
	images := map[string]string{}
	for _, service := range store.services {
		images[service.ServiceKey] = service.ImageRef
	}
	require.Equal(t, map[string]string{"collector": releaseImages.Collector, "egress": releaseImages.Egress, "docker": releaseImages.Docker, "terminal": releaseImages.Headless}, images)
	require.Contains(t, eventTypes(store.events), "machine.images_updated")
	require.Zero(t, runtime.finishedRemovals, "a stopped machine's run containers are cleaned when it is idle and running")
}

func TestManagerStartKeepsMachineOnReleaseImages(t *testing.T) {
	current := func(machineID uuid.UUID) []domain.DevMachineService {
		return []domain.DevMachineService{
			imageService(machineID, "collector", "collector", releaseImages.Collector, "c-collector"),
			imageService(machineID, "terminal", "terminal", releaseImages.Headless, "c-terminal"),
		}
	}
	store, runtime, manager := imageFixture(domain.DevMachineStatusStopped, domain.DevMachineProfileHeadless, current)

	err := manager.processOperation(context.Background(), &domain.DevMachineOperation{
		MachineID: store.machine.ID, WorkspaceID: store.machine.WorkspaceID, Action: domain.DevMachineOpStart, Generation: 4,
	})

	require.NoError(t, err)
	require.Empty(t, runtime.removed)
	require.Equal(t, 1, runtime.started)
	require.Zero(t, runtime.spawned)
	require.NotContains(t, eventTypes(store.events), "machine.images_updated")
}

// A paused machine keeps its frozen processes: resuming it never recreates
// containers.
func TestManagerResumeKeepsPausedMachineImages(t *testing.T) {
	store, runtime, manager := imageFixture(domain.DevMachineStatusPaused, domain.DevMachineProfileHeadless, headlessServices("local/headless:v0.2.4"))

	err := manager.processOperation(context.Background(), &domain.DevMachineOperation{
		MachineID: store.machine.ID, WorkspaceID: store.machine.WorkspaceID, Action: domain.DevMachineOpStart, Generation: 4,
	})

	require.NoError(t, err)
	require.Empty(t, runtime.removed)
	require.Equal(t, 1, runtime.started)
}

func TestManagerUpdateImagesRecreatesIdleRunningMachine(t *testing.T) {
	store, runtime, manager := imageFixture(domain.DevMachineStatusRunning, domain.DevMachineProfileHeadless, headlessServices("local/headless:v0.2.4"))
	operation := &domain.DevMachineOperation{ID: uuid.New(), MachineID: store.machine.ID, WorkspaceID: store.machine.WorkspaceID, Action: domain.DevMachineOpUpdateImages, Generation: 4}

	require.NoError(t, manager.processOperation(context.Background(), operation))

	require.Equal(t, 1, store.busyChecks)
	require.Equal(t, 1, runtime.finishedRemovals, "finished runs' containers no longer pin old images")
	require.Equal(t, [][]string{{"c-collector", "c-docker", "c-terminal"}}, runtime.removed)
	require.Equal(t, 1, runtime.spawned)
	require.Equal(t, domain.DevMachineStatusRunning, store.machine.Status)
	require.Contains(t, eventTypes(store.events), "machine.images_updated")
}

func TestManagerUpdateImagesLeavesBusyMachine(t *testing.T) {
	store, runtime, manager := imageFixture(domain.DevMachineStatusRunning, domain.DevMachineProfileHeadless, headlessServices("local/headless:v0.2.4"))
	store.busy = true
	operation := &domain.DevMachineOperation{ID: uuid.New(), MachineID: store.machine.ID, WorkspaceID: store.machine.WorkspaceID, Action: domain.DevMachineOpUpdateImages, Generation: 4}

	require.NoError(t, manager.processOperation(context.Background(), operation))

	require.Empty(t, runtime.removed)
	require.Zero(t, runtime.finishedRemovals)
	require.Zero(t, runtime.spawned)
}

// The IDE profile's terminal shares the IDE container and image; a machine
// built from a development environment keeps that environment's image.
func TestReleaseServiceImageFollowsProfileAndEnvironment(t *testing.T) {
	_, _, manager := imageFixture(domain.DevMachineStatusRunning, domain.DevMachineProfileHeadless, headlessServices(releaseImages.Headless))
	ide := &domain.DevMachine{ID: uuid.New(), Profile: domain.DevMachineProfileStandard}
	headless := &domain.DevMachine{ID: uuid.New(), Profile: domain.DevMachineProfileHeadless}
	environmentID := uuid.New()
	built := &domain.DevMachine{ID: uuid.New(), Profile: domain.DevMachineProfileStandard, EnvironmentID: &environmentID}

	require.Equal(t, releaseImages.IDE, manager.releaseServiceImage(ide, domain.DevMachineService{ServiceType: "terminal"}))
	require.Equal(t, releaseImages.Headless, manager.releaseServiceImage(headless, domain.DevMachineService{ServiceType: "terminal"}))
	require.Empty(t, manager.releaseServiceImage(built, domain.DevMachineService{ServiceType: "ide"}))
	require.Equal(t, releaseImages.Collector, manager.releaseServiceImage(built, domain.DevMachineService{ServiceType: "collector"}))
	require.Empty(t, manager.releaseServiceImage(ide, domain.DevMachineService{ServiceType: "agent"}), "agent runs choose their image per run")

	manager.SetImages(MachineImages{})
	require.Empty(t, manager.outdatedServices(ide, []domain.DevMachineService{{ServiceType: "ide", ImageRef: "anything"}}), "an unset image changes nothing")
}

func TestManagerQueuesImageUpdateOnlyForOutdatedMachines(t *testing.T) {
	store, _, manager := imageFixture(domain.DevMachineStatusRunning, domain.DevMachineProfileHeadless, headlessServices("local/headless:v0.2.4"))
	now := time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)

	manager.queueImageUpdate(context.Background(), store.machine, store.services, now)
	manager.queueImageUpdate(context.Background(), store.machine, store.services, now.Add(10*time.Minute))
	manager.queueImageUpdate(context.Background(), store.machine, store.services, now.Add(imageUpdateRetry))

	require.Len(t, store.queued, 3)
	require.Equal(t, store.queued[0].IdempotencyKey, store.queued[1].IdempotencyKey, "a queued or completed update is not queued again right away")
	require.NotEqual(t, store.queued[0].IdempotencyKey, store.queued[2].IdempotencyKey, "a machine that was busy is tried again later")

	_, _, current := imageFixture(domain.DevMachineStatusRunning, domain.DevMachineProfileHeadless, func(machineID uuid.UUID) []domain.DevMachineService {
		return []domain.DevMachineService{imageService(machineID, "terminal", "terminal", releaseImages.Headless, "c-terminal")}
	})
	currentStore := current.store.(*imageStoreFake)
	current.queueImageUpdate(context.Background(), currentStore.machine, currentStore.services, now)
	require.Empty(t, currentStore.queued)
}

// New runs of built-in agents use the installed release's image; a run that
// already has a container keeps its image so it can resume; a custom agent
// keeps the image it was configured with.
func TestRunImageUsesReleaseImageForNewRuns(t *testing.T) {
	store, _, manager := imageFixture(domain.DevMachineStatusRunning, domain.DevMachineProfileHeadless, headlessServices(releaseImages.Headless))
	provider := &domain.DevMachineAgentProvider{ID: uuid.New(), ProviderID: "claude-code", ImageRef: "local/agent-claude:v0.2.4"}
	run := &domain.DevMachineAgentRun{ID: uuid.New()}

	require.Equal(t, "registry/agent-claude@sha256:new", manager.runImage(context.Background(), store.machine, run, provider, store.services))
	require.Equal(t, "registry/agent-claude@sha256:new", store.providerImages[provider.ID])

	resumed := &domain.DevMachineAgentRun{ID: uuid.New()}
	services := append(store.services, domain.DevMachineService{AgentRunID: &resumed.ID, ServiceType: "agent", ServiceKey: "agent-" + resumed.ID.String(), ImageRef: "local/agent-claude:v0.2.4"})
	require.Equal(t, "local/agent-claude:v0.2.4", manager.runImage(context.Background(), store.machine, resumed, provider, services))

	custom := &domain.DevMachineAgentProvider{ID: uuid.New(), ProviderID: "claude-code", ImageRef: "custom/agent:1", IsCustom: true}
	require.Equal(t, "custom/agent:1", manager.runImage(context.Background(), store.machine, run, custom, store.services))
}
